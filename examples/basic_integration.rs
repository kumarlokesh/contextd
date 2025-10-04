/*!
# Basic Integration Example

This example demonstrates the core functionality of contextd:
1. Storing conversation transcripts
2. Searching historical context
3. Retrieving recent chats
4. Auditing memory access

Run this after starting contextd with `cargo run -- serve`

## Usage

```bash
cd examples
cargo run --bin basic_integration
```
*/

use anyhow::Result;
use chrono::{DateTime, Utc};
use reqwest::Client;
use serde::{Deserialize, Serialize};
use serde_json::{json, Value};
use std::collections::HashMap;
use uuid::Uuid;

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ChatMessage {
    pub role: String,
    pub content: String,
    #[serde(default)]
    pub metadata: HashMap<String, Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct StoreChatRequest {
    pub project_id: String,
    pub session_id: String,
    pub timestamp: DateTime<Utc>,
    pub messages: Vec<ChatMessage>,
    #[serde(default)]
    pub metadata: HashMap<String, Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct ConversationSearchRequest {
    pub project_id: String,
    pub query: String,
    pub max_results: usize,
    pub session_id: Option<String>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SearchResult {
    pub transcript_id: Uuid,
    pub session_id: String,
    pub timestamp: DateTime<Utc>,
    pub messages: Vec<ChatMessage>,
    pub score: f32,
    pub snippet: String,
    pub metadata: HashMap<String, Value>,
}

#[derive(Debug, Clone, Serialize, Deserialize)]
pub struct SearchResponse {
    pub results: Vec<SearchResult>,
    pub total_count: usize,
    pub query_time_ms: u64,
}

/// Rust client for the contextd API
pub struct ContextdClient {
    client: Client,
    base_url: String,
}

impl ContextdClient {
    pub fn new(base_url: &str) -> Self {
        Self {
            client: Client::new(),
            base_url: base_url.trim_end_matches('/').to_string(),
        }
    }

    pub async fn health_check(&self) -> Result<Value> {
        let response = self
            .client
            .get(&format!("{}/health", self.base_url))
            .send()
            .await?;

        if response.status().is_success() {
            Ok(response.json().await?)
        } else {
            anyhow::bail!("Health check failed: {}", response.status());
        }
    }

    pub async fn store_chat(
        &self,
        project_id: &str,
        session_id: &str,
        messages: Vec<ChatMessage>,
        metadata: Option<HashMap<String, Value>>,
    ) -> Result<Uuid> {
        let request = StoreChatRequest {
            project_id: project_id.to_string(),
            session_id: session_id.to_string(),
            timestamp: Utc::now(),
            messages,
            metadata: metadata.unwrap_or_default(),
        };

        let response = self
            .client
            .post(&format!("{}/store_chat", self.base_url))
            .json(&request)
            .send()
            .await?;

        if response.status().is_success() {
            let result: Value = response.json().await?;
            let transcript_id = result["transcript_id"]
                .as_str()
                .ok_or_else(|| anyhow::anyhow!("Missing transcript_id in response"))?;
            Ok(Uuid::parse_str(transcript_id)?)
        } else {
            let error: Value = response.json().await?;
            anyhow::bail!("Store chat failed: {}", error);
        }
    }

    pub async fn search_conversations(
        &self,
        project_id: &str,
        query: &str,
        max_results: usize,
        session_id: Option<&str>,
    ) -> Result<SearchResponse> {
        let request = ConversationSearchRequest {
            project_id: project_id.to_string(),
            query: query.to_string(),
            max_results,
            session_id: session_id.map(|s| s.to_string()),
        };

        let response = self
            .client
            .post(&format!("{}/conversation_search", self.base_url))
            .json(&request)
            .send()
            .await?;

        if response.status().is_success() {
            Ok(response.json().await?)
        } else {
            let error: Value = response.json().await?;
            anyhow::bail!("Search failed: {}", error);
        }
    }

    pub async fn get_recent_chats(
        &self,
        project_id: &str,
        limit: usize,
        session_id: Option<&str>,
    ) -> Result<Vec<SearchResult>> {
        let request = json!({
            "project_id": project_id,
            "limit": limit,
            "session_id": session_id
        });

        let response = self
            .client
            .post(&format!("{}/recent_chats", self.base_url))
            .json(&request)
            .send()
            .await?;

        if response.status().is_success() {
            let result: Value = response.json().await?;
            Ok(serde_json::from_value(result["chats"].clone())?)
        } else {
            let error: Value = response.json().await?;
            anyhow::bail!("Recent chats failed: {}", error);
        }
    }

    pub async fn get_audit_logs(&self, project_id: &str, limit: usize) -> Result<Vec<Value>> {
        let request = json!({
            "project_id": project_id,
            "limit": limit
        });

        let response = self
            .client
            .post(&format!("{}/audit/logs", self.base_url))
            .json(&request)
            .send()
            .await?;

        if response.status().is_success() {
            let result: Value = response.json().await?;
            Ok(serde_json::from_value(result["entries"].clone())?)
        } else {
            let error: Value = response.json().await?;
            anyhow::bail!("Audit logs failed: {}", error);
        }
    }
}

async fn demo_basic_usage() -> Result<()> {
    println!("=== contextd Basic Integration Demo ===\n");

    // Initialize client
    let client = ContextdClient::new("http://127.0.0.1:8080");
    let project_id = "demo-rust-project";

    println!("1. Testing health check...");
    match client.health_check().await {
        Ok(health) => {
            println!("✅ contextd server is healthy");
            println!("   Status: {}", health["status"]);
            println!(
                "   Version: {}\n",
                health.get("version").unwrap_or(&json!("unknown"))
            );
        }
        Err(e) => {
            println!("❌ contextd server is not responding: {}", e);
            return Err(e);
        }
    }

    println!("2. Storing sample conversations...");

    // Sample conversations to store
    let conversations = vec![
        (
            "session-1",
            vec![
                ChatMessage {
                    role: "user".to_string(),
                    content: "How do I learn Rust programming?".to_string(),
                    metadata: HashMap::new(),
                },
                ChatMessage {
                    role: "assistant".to_string(),
                    content: "Start with the Rust Book, practice with cargo, and build small projects. The compiler is your friend!".to_string(),
                    metadata: HashMap::new(),
                },
            ],
        ),
        (
            "session-1",
            vec![
                ChatMessage {
                    role: "user".to_string(),
                    content: "What are the best Rust crates for web development?".to_string(),
                    metadata: HashMap::new(),
                },
                ChatMessage {
                    role: "assistant".to_string(),
                    content: "Popular web crates include Axum and Actix-web for servers, Serde for JSON, and SQLx for databases.".to_string(),
                    metadata: HashMap::new(),
                },
            ],
        ),
        (
            "session-2",
            vec![
                ChatMessage {
                    role: "user".to_string(),
                    content: "How do I handle async programming in Rust?".to_string(),
                    metadata: HashMap::new(),
                },
                ChatMessage {
                    role: "assistant".to_string(),
                    content: "Use async/await with Tokio runtime. Start with simple async functions and work up to complex patterns.".to_string(),
                    metadata: HashMap::new(),
                },
            ],
        ),
    ];

    let mut transcript_ids = Vec::new();
    for (session_id, messages) in conversations {
        match client
            .store_chat(project_id, session_id, messages, None)
            .await
        {
            Ok(transcript_id) => {
                transcript_ids.push(transcript_id);
                println!("✅ Stored conversation: {}", transcript_id);
            }
            Err(e) => {
                println!("❌ Failed to store conversation: {}", e);
            }
        }
    }

    println!("\n3. Searching conversations...");

    // Search for Rust-related content
    match client
        .search_conversations(project_id, "Rust web development", 5, None)
        .await
    {
        Ok(search_response) => {
            println!(
                "Found {} results for 'Rust web development' in {}ms:",
                search_response.results.len(),
                search_response.query_time_ms
            );

            for (i, result) in search_response.results.iter().enumerate() {
                println!(
                    "  {}. Score: {:.3}, Session: {}",
                    i + 1,
                    result.score,
                    result.session_id
                );
                println!("     Snippet: {}...", truncate(&result.snippet, 80));
            }
        }
        Err(e) => {
            println!("❌ Search failed: {}", e);
        }
    }

    println!("\n4. Getting recent chats...");

    match client.get_recent_chats(project_id, 5, None).await {
        Ok(recent_chats) => {
            println!("Recent {} conversations:", recent_chats.len());
            for (i, chat) in recent_chats.iter().enumerate() {
                println!(
                    "  {}. {} - Session: {} ({} messages)",
                    i + 1,
                    chat.timestamp.format("%Y-%m-%d %H:%M:%S UTC"),
                    chat.session_id,
                    chat.messages.len()
                );
            }
        }
        Err(e) => {
            println!("❌ Failed to get recent chats: {}", e);
        }
    }

    println!("\n5. Checking audit logs...");

    match client.get_audit_logs(project_id, 10).await {
        Ok(audit_logs) => {
            println!("Last {} operations:", audit_logs.len());
            for log in audit_logs {
                let operation = log["operation"].as_str().unwrap_or("unknown");
                let result_count = log["result_count"].as_u64().unwrap_or(0);
                let exec_time = log["execution_time_ms"].as_u64().unwrap_or(0);

                println!(
                    "  {}: {} results in {}ms",
                    operation, result_count, exec_time
                );

                if let Some(query) = log["query"].as_str() {
                    println!("    Query: {}", truncate(query, 60));
                }
            }
        }
        Err(e) => {
            println!("❌ Failed to get audit logs: {}", e);
        }
    }

    Ok(())
}

fn truncate(s: &str, max_len: usize) -> String {
    if s.len() <= max_len {
        s.to_string()
    } else {
        format!("{}...", &s[..max_len])
    }
}

#[tokio::main]
async fn main() -> Result<()> {
    match demo_basic_usage().await {
        Ok(_) => {
            println!("\n✅ Demo completed successfully!");
            println!("\nKey takeaways:");
            println!("- All memory access is explicit via API calls");
            println!("- Every retrieval is logged for transparency");
            println!("- Context is only injected when specifically requested");
            println!("- Full audit trail available for compliance");
            println!("- Pure Rust implementation for performance and safety");
            Ok(())
        }
        Err(e) => {
            println!("❌ Demo failed: {}", e);
            println!("\nTroubleshooting:");
            println!("- Make sure contextd is running: cargo run -- serve");
            println!("- Check that port 8080 is available");
            println!("- Verify network connectivity to localhost");
            Err(e)
        }
    }
}
