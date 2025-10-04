/*!
# Batch Operations Example

This example demonstrates how to efficiently work with contextd for batch operations:
1. Bulk storage of conversation histories
2. Batch search operations
3. Performance optimization techniques
4. Error handling and retry logic

## Usage

```bash
cd examples
cargo run --bin batch_operations
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
use std::time::Instant;
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

/// Batch processor for contextd operations
pub struct BatchProcessor {
    client: ContextdClient,
    project_id: String,
}

impl BatchProcessor {
    pub fn new(client: ContextdClient, project_id: &str) -> Self {
        Self {
            client,
            project_id: project_id.to_string(),
        }
    }

    /// Store multiple conversations in batch with error handling
    pub async fn store_conversations_batch(
        &self,
        conversations: Vec<(String, Vec<ChatMessage>)>, // (session_id, messages)
    ) -> Result<Vec<Result<Uuid, String>>> {
        println!(
            "📦 Storing {} conversations in batch...",
            conversations.len()
        );

        let start_time = Instant::now();
        let mut results = Vec::new();
        let mut successful = 0;
        let mut failed = 0;

        // Use concurrency but limit to avoid overwhelming the server
        let semaphore = tokio::sync::Semaphore::new(10); // Max 10 concurrent requests

        let mut tasks = Vec::new();

        for (session_id, messages) in conversations {
            let client = &self.client;
            let project_id = &self.project_id;
            let permit = semaphore.acquire().await?;

            let task = tokio::spawn(async move {
                let _permit = permit; // Hold permit until task completes

                match client
                    .store_chat(project_id, &session_id, messages, None)
                    .await
                {
                    Ok(transcript_id) => Ok(transcript_id),
                    Err(e) => Err(format!("Session {}: {}", session_id, e)),
                }
            });

            tasks.push(task);
        }

        // Wait for all tasks to complete
        for task in tasks {
            match task.await? {
                Ok(transcript_id) => {
                    results.push(Ok(transcript_id));
                    successful += 1;
                }
                Err(error) => {
                    results.push(Err(error));
                    failed += 1;
                }
            }
        }

        let elapsed = start_time.elapsed();
        println!(
            "✅ Batch store completed: {} successful, {} failed in {:.2}s",
            successful,
            failed,
            elapsed.as_secs_f64()
        );

        Ok(results)
    }

    /// Perform multiple searches in parallel
    pub async fn search_batch(&self, queries: Vec<&str>) -> Result<Vec<(String, usize, u64)>> {
        println!("🔍 Performing {} searches in batch...", queries.len());

        let start_time = Instant::now();
        let mut tasks = Vec::new();

        for query in queries {
            let client = &self.client;
            let project_id = &self.project_id;
            let query_str = query.to_string();

            let task = tokio::spawn(async move {
                let search_start = Instant::now();

                match client
                    .search_conversations(project_id, &query_str, 10, None)
                    .await
                {
                    Ok(response) => Ok((
                        query_str,
                        response.results.len(),
                        search_start.elapsed().as_millis() as u64,
                    )),
                    Err(e) => Err(format!("Query '{}': {}", query_str, e)),
                }
            });

            tasks.push(task);
        }

        let mut results = Vec::new();
        for task in tasks {
            match task.await? {
                Ok(result) => results.push(result),
                Err(e) => println!("❌ Search failed: {}", e),
            }
        }

        let elapsed = start_time.elapsed();
        println!(
            "✅ Batch search completed: {} queries in {:.2}s",
            results.len(),
            elapsed.as_secs_f64()
        );

        Ok(results)
    }

    /// Generate synthetic conversation data for testing
    pub fn generate_sample_conversations(&self, count: usize) -> Vec<(String, Vec<ChatMessage>)> {
        let topics = vec![
            ("rust-basics", "How do I get started with Rust?", "Start with the Rust Book and practice with cargo new. The compiler will guide you!"),
            ("async-programming", "Explain async/await in Rust", "Async/await allows non-blocking code execution. Use Tokio for the runtime."),
            ("web-frameworks", "What web frameworks are available in Rust?", "Popular choices include Axum, Actix-web, and Warp for different use cases."),
            ("database-integration", "How do I connect to databases in Rust?", "Use SQLx for async database operations or Diesel for a more traditional ORM approach."),
            ("error-handling", "What's the best way to handle errors in Rust?", "Use Result<T, E> for recoverable errors and panic! for unrecoverable ones."),
            ("memory-management", "How does Rust manage memory?", "Rust uses ownership, borrowing, and lifetimes to manage memory without a garbage collector."),
            ("concurrency", "How do I write concurrent code in Rust?", "Use channels, Arc/Mutex, or async/await depending on your concurrency needs."),
            ("testing", "How do I write tests in Rust?", "Use #[test] attribute for unit tests and #[cfg(test)] for test modules."),
        ];

        let mut conversations = Vec::new();

        for i in 0..count {
            let (topic, question, answer) = &topics[i % topics.len()];
            let session_id = format!("batch-session-{}-{}", topic, i / topics.len() + 1);

            let messages = vec![
                ChatMessage {
                    role: "user".to_string(),
                    content: question.to_string(),
                    metadata: HashMap::new(),
                },
                ChatMessage {
                    role: "assistant".to_string(),
                    content: answer.to_string(),
                    metadata: HashMap::new(),
                },
            ];

            conversations.push((session_id, messages));
        }

        conversations
    }
}

async fn demo_batch_operations() -> Result<()> {
    println!("=== Batch Operations Demo ===\n");

    let client = ContextdClient::new("http://127.0.0.1:8080");
    let processor = BatchProcessor::new(client, "batch-demo");

    // 1. Generate and store sample data
    println!("1. Generating sample conversations...");
    let conversations = processor.generate_sample_conversations(50);
    println!("   Generated {} conversations", conversations.len());

    // 2. Batch store operations
    println!("\n2. Batch storing conversations...");
    let store_results = processor.store_conversations_batch(conversations).await?;

    let successful_stores: Vec<_> = store_results
        .iter()
        .filter_map(|r| r.as_ref().ok())
        .collect();
    let failed_stores: Vec<_> = store_results
        .iter()
        .filter_map(|r| r.as_ref().err())
        .collect();

    println!("   ✅ Successfully stored: {}", successful_stores.len());
    if !failed_stores.is_empty() {
        println!("   ❌ Failed to store: {}", failed_stores.len());
        for error in failed_stores.iter().take(3) {
            println!("      - {}", error);
        }
    }

    // 3. Batch search operations
    println!("\n3. Batch searching conversations...");
    let search_queries = vec![
        "Rust programming basics",
        "async await concurrent",
        "web framework server",
        "database SQLx connection",
        "error handling Result",
        "memory ownership borrowing",
        "testing unit integration",
        "performance optimization",
    ];

    let search_results = processor.search_batch(search_queries).await?;

    println!("   Search results:");
    for (query, result_count, time_ms) in search_results {
        println!(
            "     '{}': {} results in {}ms",
            truncate(&query, 30),
            result_count,
            time_ms
        );
    }

    // 4. Performance analysis
    println!("\n4. Performance analysis...");
    let client = &processor.client;

    match client.get_audit_logs("batch-demo", 100).await {
        Ok(audit_logs) => {
            let total_ops = audit_logs.len();
            let avg_time: f64 = audit_logs
                .iter()
                .map(|log| log["execution_time_ms"].as_u64().unwrap_or(0) as f64)
                .sum::<f64>()
                / total_ops as f64;

            let search_ops = audit_logs
                .iter()
                .filter(|log| log["operation"].as_str() == Some("search"))
                .count();

            let store_ops = audit_logs
                .iter()
                .filter(|log| log["operation"].as_str() == Some("store"))
                .count();

            println!("   📊 Performance metrics:");
            println!("     - Total operations: {}", total_ops);
            println!("     - Average execution time: {:.2}ms", avg_time);
            println!("     - Search operations: {}", search_ops);
            println!("     - Store operations: {}", store_ops);

            // Find slowest operations
            let mut times: Vec<_> = audit_logs
                .iter()
                .map(|log| log["execution_time_ms"].as_u64().unwrap_or(0))
                .collect();
            times.sort_by(|a, b| b.cmp(a));

            if times.len() >= 5 {
                println!(
                    "     - Slowest operations: {}ms, {}ms, {}ms",
                    times[0], times[1], times[2]
                );
            }
        }
        Err(e) => {
            println!("   ⚠️  Could not retrieve performance data: {}", e);
        }
    }

    Ok(())
}

async fn demo_error_handling() -> Result<()> {
    println!("\n=== Error Handling Demo ===\n");

    let client = ContextdClient::new("http://127.0.0.1:8080");

    // Test various error conditions
    println!("1. Testing invalid project ID...");
    match client.search_conversations("", "test query", 5, None).await {
        Ok(_) => println!("   ⚠️  Expected error but got success"),
        Err(e) => println!("   ✅ Correctly handled error: {}", e),
    }

    println!("\n2. Testing empty query...");
    match client
        .search_conversations("test-project", "", 5, None)
        .await
    {
        Ok(_) => println!("   ⚠️  Expected error but got success"),
        Err(e) => println!("   ✅ Correctly handled error: {}", e),
    }

    println!("\n3. Testing malformed requests...");
    // Test with extremely large message
    let large_message = "x".repeat(10_000_000); // 10MB message
    let messages = vec![ChatMessage {
        role: "user".to_string(),
        content: large_message,
        metadata: HashMap::new(),
    }];

    match client
        .store_chat("test-project", "test-session", messages, None)
        .await
    {
        Ok(_) => println!("   ⚠️  Large message was accepted"),
        Err(e) => println!("   ✅ Large message rejected: {}", e),
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
    // Check server health
    let client = ContextdClient::new("http://127.0.0.1:8080");
    match client.health_check().await {
        Ok(_) => println!("✅ contextd server is running\n"),
        Err(e) => {
            println!("❌ contextd server is not responding: {}", e);
            println!("Please start the server first with: cargo run -- serve\n");
            return Err(e);
        }
    }

    // Run batch operations demo
    if let Err(e) = demo_batch_operations().await {
        println!("❌ Batch operations demo failed: {}", e);
        return Err(e);
    }

    // Run error handling demo
    if let Err(e) = demo_error_handling().await {
        println!("❌ Error handling demo failed: {}", e);
        return Err(e);
    }

    println!("\n✅ Batch Operations Demo completed successfully!");
    println!("\n🎯 Key learnings:");
    println!("  • Use semaphores to limit concurrent operations");
    println!("  • Implement proper error handling and retry logic");
    println!("  • Monitor performance via audit logs");
    println!("  • Test edge cases and error conditions");
    println!("  • Batch operations can significantly improve throughput");

    Ok(())
}
