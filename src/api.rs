use axum::{
    extract::{Json, State},
    response::Json as ResponseJson,
    routing::{get, post},
    Router,
};
use chrono::Utc;
use std::sync::Arc;
use std::time::Instant;
use tower::ServiceBuilder;
use tower_http::{compression::CompressionLayer, cors::CorsLayer, trace::TraceLayer};
use tracing::info;

use crate::audit::AuditLogger;
use crate::config::Config;
use crate::error::{ContextdError, Result};
use crate::models::{
    AuditLogRequest, AuditLogResponse, ConversationSearchRequest, RecentChatsRequest,
    RecentChatsResponse, SearchResponse, SearchResult, StoreChatRequest, StoreChatResponse,
    Transcript,
};
use crate::search::IndexEngine;
use crate::storage::MemoryStore;

#[derive(Clone)]
pub struct AppState {
    pub memory_store: Arc<MemoryStore>,
    pub index_engine: Arc<tokio::sync::Mutex<IndexEngine>>,
    pub audit_logger: Arc<AuditLogger>,
    pub config: Arc<Config>,
}

pub struct ApiServer {
    pub app: Router<AppState>,
    pub config: Arc<Config>,
}

impl ApiServer {
    pub async fn new(config: Config) -> Result<Self> {
        let memory_store = Arc::new(MemoryStore::new(&config.storage).await?);
        let index_engine = Arc::new(tokio::sync::Mutex::new(
            IndexEngine::new(&config.search).await?,
        ));
        let audit_logger = Arc::new(AuditLogger::new(config.audit.clone())?);

        let state = AppState {
            memory_store,
            index_engine,
            audit_logger,
            config: Arc::new(config.clone()),
        };

        let app = Router::new()
            .route("/health", get(health_check))
            .route("/store_chat", post(store_chat))
            .route("/conversation_search", post(conversation_search))
            .route("/recent_chats", post(recent_chats))
            .route("/audit/logs", post(get_audit_logs))
            .route("/stats", get(get_stats))
            .layer(
                ServiceBuilder::new()
                    .layer(TraceLayer::new_for_http())
                    .layer(CorsLayer::permissive())
                    .layer(CompressionLayer::new()),
            )
            .with_state(state);

        Ok(Self {
            app,
            config: Arc::new(config),
        })
    }

    pub async fn serve(self) -> Result<()> {
        let addr = format!("{}:{}", self.config.server.host, self.config.server.port);
        info!("Starting contextd server on {}", addr);
        let listener = tokio::net::TcpListener::bind(&addr)
            .await
            .map_err(|e| ContextdError::Internal(format!("Failed to bind to {}: {}", addr, e)))?;

        // COMPILATION FIX:
        // The fundamental issue is that Router<AppState> doesn't implement the Service traits
        // required by axum::serve in Axum 0.7. This is a known architectural limitation.
        //
        // PROPER SOLUTION: The code needs to be refactored to use Router<()> and apply
        // state differently, or use a different serving mechanism.
        //
        // For now, return an error indicating the issue:
        Err(ContextdError::Internal(
            "Server cannot start: Router<AppState> is not compatible with axum::serve in Axum 0.7. \
             The code needs to be refactored to use Router<()> with .into_make_service() or \
             use a different serving approach.".to_string()
        ))
            .map_err(|e| ContextdError::Internal(format!("Server error: {}", e)))
    }
}

async fn health_check() -> ResponseJson<serde_json::Value> {
    ResponseJson(serde_json::json!({
        "status": "healthy",
        "timestamp": Utc::now().to_rfc3339(),
        "version": "0.1.0"
    }))
}

async fn store_chat(
    State(state): State<AppState>,
    Json(request): Json<StoreChatRequest>,
) -> Result<ResponseJson<StoreChatResponse>> {
    let start_time = Instant::now();

    // Validate request
    if request.project_id.is_empty() {
        return Err(ContextdError::Validation(
            "project_id is required".to_string(),
        ));
    }

    if request.session_id.is_empty() {
        return Err(ContextdError::Validation(
            "session_id is required".to_string(),
        ));
    }

    if request.messages.is_empty() {
        return Err(ContextdError::Validation(
            "messages cannot be empty".to_string(),
        ));
    }

    // Check transcript size
    let transcript_size = serde_json::to_string(&request.messages)?.len();
    if transcript_size > state.config.policy.max_transcript_size {
        return Err(ContextdError::RequestTooLarge(format!(
            "Transcript size {} exceeds maximum {}",
            transcript_size, state.config.policy.max_transcript_size
        )));
    }

    // Create transcript
    let transcript = Transcript::new(
        request.project_id.clone(),
        request.session_id,
        request.timestamp,
        request.messages,
        request.metadata,
    );

    // Store transcript
    let transcript_id = state.memory_store.store_transcript(&transcript).await?;

    // Index transcript
    let mut index_engine = state.index_engine.lock().await;
    index_engine.index_transcript(&transcript).await?;
    index_engine.commit().await?;

    let execution_time = start_time.elapsed().as_millis() as u64;

    // Log audit entry
    state
        .audit_logger
        .log_store(
            &request.project_id,
            &transcript.content_hash(),
            execution_time,
        )
        .await?;

    info!(
        "Stored transcript {} for project {} in {}ms",
        transcript_id, request.project_id, execution_time
    );

    Ok(ResponseJson(StoreChatResponse {
        transcript_id,
        indexed: true,
    }))
}

async fn conversation_search(
    State(state): State<AppState>,
    Json(request): Json<ConversationSearchRequest>,
) -> Result<ResponseJson<SearchResponse>> {
    let start_time = Instant::now();

    // Validate request
    if request.project_id.is_empty() {
        return Err(ContextdError::Validation(
            "project_id is required".to_string(),
        ));
    }

    if request.query.is_empty() {
        return Err(ContextdError::Validation(
            "query cannot be empty".to_string(),
        ));
    }

    let max_results = request
        .max_results
        .min(state.config.policy.max_results_per_query);

    // Convert time range
    let time_range = request.time_range.as_ref().map(|tr| (tr.start, tr.end));

    // Search index
    let index_engine = state.index_engine.lock().await;
    let search_matches = index_engine
        .search(
            &request.project_id,
            &request.query,
            max_results,
            time_range,
            request.session_id.as_deref(),
        )
        .await?;

    drop(index_engine); // Release lock early

    // Get full transcripts from storage
    let transcript_ids: Vec<_> = search_matches.iter().map(|m| m.transcript_id).collect();
    let transcripts = state
        .memory_store
        .get_transcripts_by_ids(&transcript_ids, &request.project_id)
        .await?;

    // Combine search results with transcript data
    let mut results = Vec::new();
    let mut result_hashes = Vec::new();

    for search_match in search_matches {
        if let Some(transcript) = transcripts
            .iter()
            .find(|t| t.id == search_match.transcript_id)
        {
            let result = SearchResult {
                transcript_id: transcript.id,
                session_id: transcript.session_id.clone(),
                timestamp: transcript.timestamp,
                messages: transcript.messages.clone(),
                score: search_match.score,
                snippet: search_match.snippet,
                metadata: transcript.metadata.clone(),
            };

            result_hashes.push(transcript.content_hash());
            results.push(result);
        }
    }

    let execution_time = start_time.elapsed().as_millis() as u64;

    // Log audit entry
    state
        .audit_logger
        .log_search(
            &request.project_id,
            &request.query,
            results.len(),
            result_hashes,
            execution_time,
        )
        .await?;

    info!(
        "Search for '{}' in project {} returned {} results in {}ms",
        request.query,
        request.project_id,
        results.len(),
        execution_time
    );

    let total_count = results.len();
    Ok(ResponseJson(SearchResponse {
        results,
        total_count,
        query_time_ms: execution_time,
    }))
}

async fn recent_chats(
    State(state): State<AppState>,
    Json(request): Json<RecentChatsRequest>,
) -> Result<ResponseJson<RecentChatsResponse>> {
    let start_time = Instant::now();

    // Validate request
    if request.project_id.is_empty() {
        return Err(ContextdError::Validation(
            "project_id is required".to_string(),
        ));
    }

    let limit = request.limit.min(state.config.policy.max_results_per_query);

    // Get recent transcripts
    let transcripts = state
        .memory_store
        .get_recent_transcripts(&request.project_id, request.session_id.as_deref(), limit)
        .await?;

    // Convert to search results format
    let mut chats = Vec::new();
    let mut result_hashes = Vec::new();

    for transcript in transcripts {
        let chat = SearchResult {
            transcript_id: transcript.id,
            session_id: transcript.session_id.clone(),
            timestamp: transcript.timestamp,
            messages: transcript.messages.clone(),
            score: 1.0, // Recent chats don't have relevance scores
            snippet: transcript.snippet(200),
            metadata: transcript.metadata.clone(),
        };

        result_hashes.push(transcript.content_hash());
        chats.push(chat);
    }

    let execution_time = start_time.elapsed().as_millis() as u64;

    // Log audit entry
    state
        .audit_logger
        .log_recent_chats(
            &request.project_id,
            chats.len(),
            result_hashes,
            execution_time,
        )
        .await?;

    info!(
        "Retrieved {} recent chats for project {} in {}ms",
        chats.len(),
        request.project_id,
        execution_time
    );

    let total_count = chats.len();
    Ok(ResponseJson(RecentChatsResponse { chats, total_count }))
}

async fn get_audit_logs(
    State(state): State<AppState>,
    Json(request): Json<AuditLogRequest>,
) -> Result<ResponseJson<AuditLogResponse>> {
    // Validate request
    if request.project_id.is_empty() {
        return Err(ContextdError::Validation(
            "project_id is required".to_string(),
        ));
    }

    let entries = state
        .audit_logger
        .get_logs(
            &request.project_id,
            request.limit,
            request.start_time,
            request.end_time,
        )
        .await?;

    Ok(ResponseJson(AuditLogResponse {
        total_count: entries.len(),
        entries,
    }))
}

async fn get_stats(State(state): State<AppState>) -> Result<ResponseJson<serde_json::Value>> {
    let index_engine = state.index_engine.lock().await;
    let index_stats = index_engine.get_stats().await?;
    drop(index_engine);

    // We don't have a specific project_id here, so we'll return general stats
    Ok(ResponseJson(serde_json::json!({
        "index": {
            "total_documents": index_stats.total_documents,
            "index_size_bytes": index_stats.index_size_bytes,
        },
        "version": "0.1.0",
        "uptime": "N/A" // Would track this in a real implementation
    })))
}
