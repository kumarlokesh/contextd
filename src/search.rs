use chrono::{DateTime, Utc};
use std::path::Path;
use tantivy::collector::TopDocs;
use tantivy::query::QueryParser;
use tantivy::schema::{Field, Schema, Value, STORED, TEXT};
use tantivy::{doc, Index, IndexReader, IndexWriter, ReloadPolicy, TantivyDocument, Term};
use uuid::Uuid;

use crate::config::SearchConfig;
use crate::error::{ContextdError, Result};
use crate::models::Transcript;

pub struct IndexEngine {
    index: Index,
    reader: IndexReader,
    writer: IndexWriter,
    schema: TantivySchema,
}

struct TantivySchema {
    id: Field,
    project_id: Field,
    session_id: Field,
    timestamp: Field,
    content: Field,
    messages_json: Field,
    metadata_json: Field,
}

#[derive(Debug, Clone)]
pub struct SearchMatch {
    pub transcript_id: Uuid,
    pub score: f32,
    pub snippet: String,
}

impl IndexEngine {
    pub async fn new(config: &SearchConfig) -> Result<Self> {
        // Create index directory if it doesn't exist
        std::fs::create_dir_all(&config.index_path)?;

        let mut schema_builder = Schema::builder();

        let schema = TantivySchema {
            id: schema_builder.add_text_field("id", STORED),
            project_id: schema_builder.add_text_field("project_id", TEXT),
            session_id: schema_builder.add_text_field("session_id", TEXT | STORED),
            timestamp: schema_builder.add_text_field("timestamp", STORED),
            content: schema_builder.add_text_field("content", TEXT),
            messages_json: schema_builder.add_text_field("messages_json", STORED),
            metadata_json: schema_builder.add_text_field("metadata_json", STORED),
        };

        let tantivy_schema = schema_builder.build();

        let index_path = Path::new(&config.index_path);
        let index = if index_path.join("meta.json").exists() {
            Index::open_in_dir(index_path)
                .map_err(|e| ContextdError::Search(format!("Failed to open index: {}", e)))?
        } else {
            Index::create_in_dir(index_path, tantivy_schema.clone())
                .map_err(|e| ContextdError::Search(format!("Failed to create index: {}", e)))?
        };

        let reader = index
            .reader_builder()
            .reload_policy(ReloadPolicy::OnCommitWithDelay)
            .try_into()
            .map_err(|e| ContextdError::Search(format!("Failed to create reader: {}", e)))?;

        let writer = index
            .writer(50_000_000) // 50MB heap
            .map_err(|e| ContextdError::Search(format!("Failed to create writer: {}", e)))?;

        Ok(Self {
            index,
            reader,
            writer,
            schema,
        })
    }

    pub async fn index_transcript(&mut self, transcript: &Transcript) -> Result<()> {
        let content = transcript.content_for_search();
        let messages_json = serde_json::to_string(&transcript.messages)?;
        let metadata_json = serde_json::to_string(&transcript.metadata)?;

        let doc = doc!(
            self.schema.id => transcript.id.to_string(),
            self.schema.project_id => transcript.project_id.clone(),
            self.schema.session_id => transcript.session_id.clone(),
            self.schema.timestamp => transcript.timestamp.to_rfc3339(),
            self.schema.content => content,
            self.schema.messages_json => messages_json,
            self.schema.metadata_json => metadata_json,
        );

        self.writer
            .add_document(doc)
            .map_err(|e| ContextdError::Search(format!("Failed to add document: {}", e)))?;

        Ok(())
    }

    pub async fn commit(&mut self) -> Result<()> {
        self.writer
            .commit()
            .map_err(|e| ContextdError::Search(format!("Failed to commit: {}", e)))?;
        Ok(())
    }

    pub async fn search(
        &self,
        project_id: &str,
        query: &str,
        max_results: usize,
        time_range: Option<(DateTime<Utc>, DateTime<Utc>)>,
        session_id: Option<&str>,
    ) -> Result<Vec<SearchMatch>> {
        let searcher = self.reader.searcher();

        // Build query
        let mut query_parts = vec![format!("project_id:{}", project_id)];

        if !query.is_empty() {
            query_parts.push(format!("content:({})", query));
        }

        if let Some(session) = session_id {
            query_parts.push(format!("session_id:{}", session));
        }

        let query_string = query_parts.join(" AND ");

        let query_parser = QueryParser::for_index(
            &self.index,
            vec![
                self.schema.content,
                self.schema.project_id,
                self.schema.session_id,
            ],
        );

        let parsed_query = query_parser
            .parse_query(&query_string)
            .map_err(|e| ContextdError::Search(format!("Failed to parse query: {}", e)))?;

        let top_docs = searcher
            .search(&parsed_query, &TopDocs::with_limit(max_results))
            .map_err(|e| ContextdError::Search(format!("Search failed: {}", e)))?;

        let mut matches = Vec::new();

        for (score, doc_address) in top_docs {
            let retrieved_doc: TantivyDocument = searcher.doc(doc_address).map_err(|e| {
                ContextdError::Search(format!("Failed to retrieve document: {}", e))
            })?;

            let id_text = retrieved_doc
                .get_first(self.schema.id)
                .and_then(|v| v.as_str())
                .ok_or_else(|| ContextdError::Search("Missing ID field".to_string()))?;

            let transcript_id = Uuid::parse_str(id_text)
                .map_err(|e| ContextdError::Search(format!("Invalid UUID: {}", e)))?;

            let timestamp_text = retrieved_doc
                .get_first(self.schema.timestamp)
                .and_then(|v| v.as_str())
                .ok_or_else(|| ContextdError::Search("Missing timestamp field".to_string()))?;

            let timestamp = DateTime::parse_from_rfc3339(timestamp_text)
                .map_err(|e| ContextdError::Search(format!("Invalid timestamp: {}", e)))?
                .with_timezone(&Utc);

            // Apply time range filter if specified
            if let Some((start, end)) = time_range {
                if timestamp < start || timestamp > end {
                    continue;
                }
            }

            let messages_json = retrieved_doc
                .get_first(self.schema.messages_json)
                .and_then(|v| v.as_str())
                .ok_or_else(|| ContextdError::Search("Missing messages field".to_string()))?;

            // Create snippet from the content
            let snippet = self.create_snippet(messages_json, query, 200);

            matches.push(SearchMatch {
                transcript_id,
                score,
                snippet,
            });
        }

        Ok(matches)
    }

    pub async fn delete_transcript(&mut self, transcript_id: Uuid, project_id: &str) -> Result<()> {
        let term = Term::from_field_text(self.schema.id, &transcript_id.to_string());
        let _project_term = Term::from_field_text(self.schema.project_id, project_id);

        // For now, we'll use a simple approach - delete by ID
        // In production, we'd want to verify project_id as well
        let _deleted_count = self.writer.delete_term(term);

        Ok(())
    }

    fn create_snippet(&self, messages_json: &str, query: &str, max_length: usize) -> String {
        // Simple snippet creation - in production, we'd want more sophisticated highlighting
        if messages_json.len() <= max_length {
            messages_json.to_string()
        } else {
            // Try to find the query term and show context around it
            let lowercase_content = messages_json.to_lowercase();
            let lowercase_query = query.to_lowercase();

            if let Some(pos) = lowercase_content.find(&lowercase_query) {
                let start = pos.saturating_sub(max_length / 4);
                let end = (pos + lowercase_query.len() + max_length / 4).min(messages_json.len());

                let mut snippet = messages_json[start..end].to_string();
                if start > 0 {
                    snippet = format!("...{}", snippet);
                }
                if end < messages_json.len() {
                    snippet = format!("{}...", snippet);
                }
                snippet
            } else {
                format!(
                    "{}...",
                    &messages_json[..max_length.min(messages_json.len())]
                )
            }
        }
    }

    pub async fn get_stats(&self) -> Result<IndexStats> {
        let searcher = self.reader.searcher();
        let num_docs = searcher.num_docs() as usize;

        Ok(IndexStats {
            total_documents: num_docs,
            index_size_bytes: 0, // We'd need to walk the directory to get actual size
        })
    }
}

#[derive(Debug, Clone)]
pub struct IndexStats {
    pub total_documents: usize,
    pub index_size_bytes: u64,
}
