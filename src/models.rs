use chrono::{DateTime, Utc};
use serde::{Deserialize, Serialize};
use std::collections::HashMap;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ChatMessage {
    pub role: String,
    pub content: String,
    #[serde(default)]
    pub metadata: HashMap<String, serde_json::Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StoreChatRequest {
    pub project_id: String,
    pub session_id: String,
    pub timestamp: DateTime<Utc>,
    pub messages: Vec<ChatMessage>,
    #[serde(default)]
    pub metadata: HashMap<String, serde_json::Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ConversationSearchRequest {
    pub project_id: String,
    pub query: String,
    #[serde(default = "default_max_results")]
    pub max_results: usize,
    pub time_range: Option<TimeRange>,
    pub session_id: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RecentChatsRequest {
    pub project_id: String,
    #[serde(default = "default_limit")]
    pub limit: usize,
    pub session_id: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct TimeRange {
    pub start: DateTime<Utc>,
    pub end: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SearchResult {
    pub transcript_id: Uuid,
    pub session_id: String,
    pub timestamp: DateTime<Utc>,
    pub messages: Vec<ChatMessage>,
    pub score: f32,
    pub snippet: String,
    #[serde(default)]
    pub metadata: HashMap<String, serde_json::Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SearchResponse {
    pub results: Vec<SearchResult>,
    pub total_count: usize,
    pub query_time_ms: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct RecentChatsResponse {
    pub chats: Vec<SearchResult>,
    pub total_count: usize,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StoreChatResponse {
    pub transcript_id: Uuid,
    pub indexed: bool,
}

#[derive(Debug, Clone)]
pub struct Transcript {
    pub id: Uuid,
    pub project_id: String,
    pub session_id: String,
    pub timestamp: DateTime<Utc>,
    pub messages: Vec<ChatMessage>,
    pub metadata: HashMap<String, serde_json::Value>,
    pub created_at: DateTime<Utc>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuditLogEntry {
    pub id: Uuid,
    pub timestamp: DateTime<Utc>,
    pub project_id: String,
    pub operation: String, // "search", "recent", "store"
    pub query: Option<String>,
    pub result_count: usize,
    pub result_hashes: Vec<String>,
    pub execution_time_ms: u64,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuditLogRequest {
    pub project_id: String,
    #[serde(default = "default_limit")]
    pub limit: usize,
    pub start_time: Option<DateTime<Utc>>,
    pub end_time: Option<DateTime<Utc>>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct AuditLogResponse {
    pub entries: Vec<AuditLogEntry>,
    pub total_count: usize,
}

// Default value functions
fn default_max_results() -> usize {
    10
}

fn default_limit() -> usize {
    20
}

impl Transcript {
    pub fn new(
        project_id: String,
        session_id: String,
        timestamp: DateTime<Utc>,
        messages: Vec<ChatMessage>,
        metadata: HashMap<String, serde_json::Value>,
    ) -> Self {
        Self {
            id: Uuid::new_v4(),
            project_id,
            session_id,
            timestamp,
            messages,
            metadata,
            created_at: Utc::now(),
        }
    }

    pub fn content_for_search(&self) -> String {
        self.messages
            .iter()
            .map(|msg| format!("{}: {}", msg.role, msg.content))
            .collect::<Vec<_>>()
            .join("\n")
    }

    pub fn snippet(&self, max_length: usize) -> String {
        let content = self.content_for_search();
        if content.len() <= max_length {
            content
        } else {
            format!("{}...", &content[..max_length])
        }
    }

    pub fn content_hash(&self) -> String {
        use std::collections::hash_map::DefaultHasher;
        use std::hash::{Hash, Hasher};

        let mut hasher = DefaultHasher::new();
        self.content_for_search().hash(&mut hasher);
        format!("{:x}", hasher.finish())
    }
}
