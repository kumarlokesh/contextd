/*!
# LLM Memory Integration Demo

This example demonstrates how to integrate contextd as a memory backend for an LLM system.
It shows explicit memory retrieval patterns that give transparency and control over context injection.

## Usage

```bash
cd examples
cargo run --bin llm_memory_demo
```
*/

use anyhow::Result;
use contextd::{
    ChatMessage, ConversationSearchRequest, RecentChatsRequest, RecentChatsResponse,
    SearchResponse, StoreChatRequest,
};
use reqwest::Client;
use serde_json::json;
use std::collections::HashMap;
use uuid::Uuid;

/// Simple HTTP client for contextd API
pub struct ContextdClient {
    client: Client,
    base_url: String,
}

impl ContextdClient {
    pub fn new(base_url: &str) -> Self {
        Self {
            client: Client::new(),
            base_url: base_url.to_string(),
        }
    }

    pub async fn store_chat(&self, request: StoreChatRequest) -> Result<()> {
        let response = self
            .client
            .post(&format!("{}/api/store", self.base_url))
            .json(&request)
            .send()
            .await?;

        if !response.status().is_success() {
            anyhow::bail!("Failed to store chat: {}", response.status());
        }
        Ok(())
    }

    pub async fn search_conversations(
        &self,
        request: ConversationSearchRequest,
    ) -> Result<SearchResponse> {
        let response = self
            .client
            .post(&format!("{}/api/search", self.base_url))
            .json(&request)
            .send()
            .await?;

        if !response.status().is_success() {
            anyhow::bail!("Failed to search conversations: {}", response.status());
        }

        Ok(response.json().await?)
    }

    pub async fn get_recent_chats(
        &self,
        request: RecentChatsRequest,
    ) -> Result<RecentChatsResponse> {
        let response = self
            .client
            .post(&format!("{}/api/recent", self.base_url))
            .json(&request)
            .send()
            .await?;

        if !response.status().is_success() {
            anyhow::bail!("Failed to get recent chats: {}", response.status());
        }

        Ok(response.json().await?)
    }

    pub async fn get_audit_logs(
        &self,
        project_id: &str,
        limit: usize,
    ) -> Result<Vec<serde_json::Value>> {
        let request = json!({
            "project_id": project_id,
            "limit": limit
        });

        let response = self
            .client
            .post(&format!("{}/api/audit", self.base_url))
            .json(&request)
            .send()
            .await?;

        if !response.status().is_success() {
            anyhow::bail!("Failed to get audit logs: {}", response.status());
        }

        let audit_response: serde_json::Value = response.json().await?;
        Ok(audit_response["logs"].as_array().unwrap_or(&vec![]).clone())
    }

    pub async fn health_check(&self) -> Result<()> {
        let response = self
            .client
            .get(&format!("{}/health", self.base_url))
            .send()
            .await?;

        if !response.status().is_success() {
            anyhow::bail!("Health check failed: {}", response.status());
        }

        Ok(())
    }
}

/// Example LLM wrapper that uses contextd for memory
pub struct LLMWithMemory {
    project_id: String,
    session_id: String,
    contextd: ContextdClient,
}

impl LLMWithMemory {
    pub fn new(project_id: &str, contextd_client: ContextdClient) -> Self {
        Self {
            project_id: project_id.to_string(),
            session_id: Uuid::new_v4().to_string(),
            contextd: contextd_client,
        }
    }

    pub async fn chat(&self, user_message: &str, use_memory: bool) -> Result<String> {
        println!("\n🧠 Processing: {}", user_message);

        let mut context = String::new();

        if use_memory {
            print!("📚 Searching memory for relevant context... ");

            // Search for relevant context
            let search_request = ConversationSearchRequest {
                project_id: self.project_id.clone(),
                query: user_message.to_string(),
                max_results: 3,
                time_range: None,
                session_id: None,
            };

            match self.contextd.search_conversations(search_request).await {
                Ok(search_response) => {
                    println!("found {} results", search_response.results.len());

                    if !search_response.results.is_empty() {
                        let mut context_parts = Vec::new();

                        for ctx in &search_response.results {
                            for msg in &ctx.messages {
                                context_parts.push(format!("{}: {}", msg.role, msg.content));
                            }
                        }

                        context = format!(
                            "Previous relevant context (score: {:.3}):\n{}\n\n",
                            search_response.results[0].score,
                            context_parts.join("\n")
                        );
                    }
                }
                Err(e) => {
                    println!("⚠️  Memory search failed: {}", e);
                }
            }
        }

        // Simulate LLM response (in real usage, this would call your LLM)
        let assistant_response = self.simulate_llm_response(user_message, &context);

        // Store the conversation
        let messages = vec![
            ChatMessage {
                role: "user".to_string(),
                content: user_message.to_string(),
                metadata: HashMap::new(),
            },
            ChatMessage {
                role: "assistant".to_string(),
                content: assistant_response.clone(),
                metadata: HashMap::new(),
            },
        ];

        let mut metadata = HashMap::new();
        metadata.insert("used_memory".to_string(), json!(use_memory));
        metadata.insert("context_length".to_string(), json!(context.len()));

        let store_request = StoreChatRequest {
            project_id: self.project_id.clone(),
            session_id: self.session_id.clone(),
            timestamp: chrono::Utc::now(),
            messages,
            metadata,
        };

        match self.contextd.store_chat(store_request).await {
            Ok(_) => {
                println!("💾 Stored conversation successfully");
            }
            Err(e) => {
                println!("⚠️  Failed to store conversation: {}", e);
            }
        }

        Ok(assistant_response)
    }

    fn simulate_llm_response(&self, user_message: &str, context: &str) -> String {
        let user_lower = user_message.to_lowercase();

        // Enhanced responses based on context
        if !context.is_empty() && user_lower.contains("remind") || user_lower.contains("previous") {
            return format!(
                "Based on our previous conversations:\n\n{}\n\nTo answer your current question: {}",
                &context[..context.len().min(300)],
                self.generate_base_response(&user_lower)
            );
        }

        self.generate_base_response(&user_lower)
    }

    fn generate_base_response(&self, user_lower: &str) -> String {
        if user_lower.contains("rust") {
            "Rust is an excellent systems programming language! Here's what makes it special:\n\n```rust\nfn main() {\n    println!(\"Memory safety without garbage collection!\");\n}\n```\n\nKey features: ownership system, zero-cost abstractions, and fearless concurrency.".to_string()
        } else if user_lower.contains("async") {
            "Async programming in Rust is powerful with the async/await syntax:\n\n```rust\nasync fn fetch_data() -> Result<String, Error> {\n    let response = reqwest::get(\"https://api.example.com\").await?;\n    Ok(response.text().await?)\n}\n```\n\nUse Tokio for the runtime and embrace the Future trait!".to_string()
        } else if user_lower.contains("web") || user_lower.contains("server") {
            "For web development in Rust, consider these excellent crates:\n\n- **Axum**: Modern, ergonomic web framework\n- **Actix-web**: High-performance actor-based framework\n- **Warp**: Composable web server framework\n- **Rocket**: Type-safe web framework\n\nPair with SQLx for databases and Serde for JSON handling!".to_string()
        } else if user_lower.contains("database") {
            "Rust has great database options:\n\n- **SQLx**: Compile-time checked SQL with async support\n- **Diesel**: Type-safe ORM with excellent performance\n- **SeaORM**: Modern async ORM\n- **redis-rs**: For Redis integration\n\nChoose based on your needs: SQLx for flexibility, Diesel for type safety!".to_string()
        } else if user_lower.contains("memory") || user_lower.contains("context") {
            format!(
                "Great question about memory and context! With contextd, you get:\n\n✅ Explicit retrieval - no silent context injection\n✅ Full audit trail - see exactly what memory was accessed\n✅ Local-first - your data stays on your machine\n✅ Hybrid search - full-text + semantic search\n\nYour question was: {}", 
                user_lower
            )
        } else {
            format!(
                "I understand you're asking about: {}\n\nI'd be happy to help! Could you provide more specific details about what you'd like to know?",
                user_lower
            )
        }
    }
}

async fn demo_llm_integration() -> Result<()> {
    println!("=== LLM Memory Integration Demo ===\n");

    let client = ContextdClient::new("http://127.0.0.1:8080");
    let llm = LLMWithMemory::new("llm-rust-demo", client);

    // Simulate a conversation with memory
    println!("🤖 Starting conversation with LLM (with contextd memory)");
    println!("    Project: llm-rust-demo");
    println!("    Session: {}\n", llm.session_id);

    let conversations = vec![
        ("Tell me about Rust programming", true),
        ("How do I handle async programming in Rust?", true),
        ("What are good web frameworks for Rust?", true),
        (
            "Can you remind me what we discussed about Rust earlier?",
            true,
        ), // This should use memory
        ("Tell me about database options in Rust", true),
        ("What did we talk about regarding async programming?", true), // Memory retrieval
    ];

    for (user_msg, use_memory) in conversations {
        println!("👤 User: {}", user_msg);

        match llm.chat(user_msg, use_memory).await {
            Ok(response) => {
                println!("🤖 Assistant: {}\n", response);

                // Brief pause for readability
                tokio::time::sleep(tokio::time::Duration::from_millis(500)).await;
            }
            Err(e) => {
                println!("❌ Chat failed: {}\n", e);
            }
        }
    }

    Ok(())
}

async fn demo_memory_audit() -> Result<()> {
    println!("=== Memory Access Audit ===\n");

    let client = ContextdClient::new("http://127.0.0.1:8080");

    // Show audit trail for transparency
    match client.get_audit_logs("llm-rust-demo", 20).await {
        Ok(audit_logs) => {
            println!("📊 Operations performed: {}", audit_logs.len());

            let search_count = audit_logs
                .iter()
                .filter(|log| log["operation"].as_str() == Some("search"))
                .count();

            let store_count = audit_logs
                .iter()
                .filter(|log| log["operation"].as_str() == Some("store"))
                .count();

            println!("  - 🔍 Searches: {}", search_count);
            println!("  - 💾 Stores: {}", store_count);
            println!("  - 📝 Total memory accesses: {}\n", audit_logs.len());

            println!("Recent operations:");
            for (i, log) in audit_logs.iter().take(10).enumerate() {
                let operation = log["operation"].as_str().unwrap_or("unknown");
                let result_count = log["result_count"].as_u64().unwrap_or(0);
                let exec_time = log["execution_time_ms"].as_u64().unwrap_or(0);
                let timestamp = log["timestamp"].as_str().unwrap_or("unknown");

                println!(
                    "  {}. {} - {} results in {}ms ({})",
                    i + 1,
                    operation,
                    result_count,
                    exec_time,
                    timestamp
                );

                if let Some(query) = log["query"].as_str() {
                    println!("     Query: {}", truncate(query, 80));
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
    // Check server health first
    let client = ContextdClient::new("http://127.0.0.1:8080");
    match client.health_check().await {
        Ok(_) => println!("✅ contextd server is running\n"),
        Err(e) => {
            println!("❌ contextd server is not responding: {}", e);
            println!("Please start the server first with: cargo run -- serve\n");
            return Err(e);
        }
    }

    // Run the demo
    if let Err(e) = demo_llm_integration().await {
        println!("❌ LLM integration demo failed: {}", e);
        return Err(e);
    }

    if let Err(e) = demo_memory_audit().await {
        println!("❌ Memory audit demo failed: {}", e);
        return Err(e);
    }

    println!("\n✅ LLM Memory Demo completed successfully!");
    println!("\n🎯 Key insights:");
    println!("  • Memory retrieval is explicit and transparent");
    println!("  • Context injection only happens when requested");
    println!("  • Full audit trail shows exactly what was accessed");
    println!("  • Rust provides type safety and performance");
    println!("  • Local-first architecture protects privacy");

    Ok(())
}
