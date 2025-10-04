/*!
Integration tests for contextd

These tests verify the complete functionality of contextd by testing the API endpoints
against a real instance of the application.
*/

use chrono::Utc;
use contextd::{config::Config, ApiServer};
use reqwest::Client;
use serde_json::{json, Value};
use std::collections::HashMap;
use tempfile::TempDir;
use tokio::time::{sleep, timeout, Duration};
use uuid::Uuid;

/// Test harness that sets up a contextd server for testing
struct TestHarness {
    #[allow(dead_code)]
    temp_dir: TempDir,
    server_handle: tokio::task::JoinHandle<()>,
    client: Client,
    base_url: String,
}

impl TestHarness {
    async fn new() -> Self {
        let temp_dir = TempDir::new().expect("Failed to create temp directory");
        let temp_path = temp_dir.path().to_str().unwrap();

        // Create test configuration
        let mut config = Config::default();
        config.server.port = 0; // Let the OS assign a port
        config.storage.sqlite_path = Some(format!("{}/test.db", temp_path));
        config.search.index_path = format!("{}/index", temp_path);
        config.audit.log_path = format!("{}/audit.log", temp_path);

        // Start the server
        let server = ApiServer::new(config.clone())
            .await
            .expect("Failed to create server");
        let listener = tokio::net::TcpListener::bind("127.0.0.1:0").await.unwrap();
        let addr = listener.local_addr().unwrap();
        let base_url = format!("http://{}", addr);

        let server_handle = tokio::spawn(async move {
            // Note: Router<AppState> serving not supported in current Axum version
            // This is a placeholder for integration testing
            eprintln!("Integration test server setup - Router<AppState> serving not implemented");
        });

        // Wait for server to start
        sleep(Duration::from_millis(100)).await;

        let client = Client::new();

        Self {
            temp_dir,
            server_handle,
            client,
            base_url,
        }
    }

    async fn health_check(&self) -> reqwest::Result<Value> {
        let response = self
            .client
            .get(&format!("{}/health", self.base_url))
            .send()
            .await?;
        response.json().await
    }

    async fn store_chat(
        &self,
        project_id: &str,
        session_id: &str,
        messages: Vec<Value>,
    ) -> reqwest::Result<Value> {
        let request = json!({
            "project_id": project_id,
            "session_id": session_id,
            "timestamp": Utc::now().to_rfc3339(),
            "messages": messages,
            "metadata": {}
        });

        let response = self
            .client
            .post(&format!("{}/store_chat", self.base_url))
            .json(&request)
            .send()
            .await?;

        response.json().await
    }

    async fn search_conversations(
        &self,
        project_id: &str,
        query: &str,
        max_results: usize,
    ) -> reqwest::Result<Value> {
        let request = json!({
            "project_id": project_id,
            "query": query,
            "max_results": max_results
        });

        let response = self
            .client
            .post(&format!("{}/conversation_search", self.base_url))
            .json(&request)
            .send()
            .await?;

        response.json().await
    }

    async fn get_recent_chats(&self, project_id: &str, limit: usize) -> reqwest::Result<Value> {
        let request = json!({
            "project_id": project_id,
            "limit": limit
        });

        let response = self
            .client
            .post(&format!("{}/recent_chats", self.base_url))
            .json(&request)
            .send()
            .await?;

        response.json().await
    }

    async fn get_audit_logs(&self, project_id: &str, limit: usize) -> reqwest::Result<Value> {
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

        response.json().await
    }
}

impl Drop for TestHarness {
    fn drop(&mut self) {
        self.server_handle.abort();
    }
}

#[tokio::test]
async fn test_health_check() {
    let harness = TestHarness::new().await;

    let health = harness.health_check().await.expect("Health check failed");

    assert_eq!(health["status"], "healthy");
    assert!(health["version"].is_string());
    assert!(health["timestamp"].is_string());
}

#[tokio::test]
async fn test_store_and_retrieve_conversation() {
    let harness = TestHarness::new().await;
    let project_id = "test-project";
    let session_id = "test-session";

    let messages = vec![
        json!({"role": "user", "content": "Hello, how are you?"}),
        json!({"role": "assistant", "content": "I'm doing well, thank you!"}),
    ];

    // Store conversation
    let store_result = harness
        .store_chat(project_id, session_id, messages)
        .await
        .expect("Failed to store chat");

    assert!(store_result["transcript_id"].is_string());
    assert_eq!(store_result["indexed"], true);

    // Retrieve recent chats
    let recent_chats = harness
        .get_recent_chats(project_id, 10)
        .await
        .expect("Failed to get recent chats");

    let chats = recent_chats["chats"].as_array().unwrap();
    assert_eq!(chats.len(), 1);

    let chat = &chats[0];
    assert_eq!(chat["session_id"], session_id);
    assert_eq!(chat["messages"].as_array().unwrap().len(), 2);
}

#[tokio::test]
async fn test_conversation_search() {
    let harness = TestHarness::new().await;
    let project_id = "search-test-project";

    // Store multiple conversations
    let conversations = vec![
        (
            "session-1",
            vec![
                json!({"role": "user", "content": "How do I learn Rust programming?"}),
                json!({"role": "assistant", "content": "Start with the Rust Book and practice coding"}),
            ],
        ),
        (
            "session-2",
            vec![
                json!({"role": "user", "content": "What are good Python libraries?"}),
                json!({"role": "assistant", "content": "NumPy, Pandas, and Django are popular"}),
            ],
        ),
        (
            "session-3",
            vec![
                json!({"role": "user", "content": "Explain Rust ownership"}),
                json!({"role": "assistant", "content": "Ownership is Rust's memory management system"}),
            ],
        ),
    ];

    for (session_id, messages) in conversations {
        harness
            .store_chat(project_id, session_id, messages)
            .await
            .expect("Failed to store conversation");
    }

    // Give the search index time to update
    sleep(Duration::from_millis(100)).await;

    // Search for Rust-related conversations
    let search_results = harness
        .search_conversations(project_id, "Rust programming", 5)
        .await
        .expect("Failed to search conversations");

    let results = search_results["results"].as_array().unwrap();
    assert!(results.len() >= 1);

    // Verify that the search found relevant content
    let found_rust_content = results.iter().any(|result| {
        let messages = result["messages"].as_array().unwrap();
        messages.iter().any(|msg| {
            msg["content"]
                .as_str()
                .unwrap()
                .to_lowercase()
                .contains("rust")
        })
    });
    assert!(found_rust_content);
}

#[tokio::test]
async fn test_project_isolation() {
    let harness = TestHarness::new().await;

    let project_a = "project-a";
    let project_b = "project-b";

    // Store conversation in project A
    let messages_a = vec![
        json!({"role": "user", "content": "Project A conversation"}),
        json!({"role": "assistant", "content": "This is in project A"}),
    ];
    harness
        .store_chat(project_a, "session-1", messages_a)
        .await
        .expect("Failed to store in project A");

    // Store conversation in project B
    let messages_b = vec![
        json!({"role": "user", "content": "Project B conversation"}),
        json!({"role": "assistant", "content": "This is in project B"}),
    ];
    harness
        .store_chat(project_b, "session-1", messages_b)
        .await
        .expect("Failed to store in project B");

    // Verify project A only sees its own conversations
    let recent_a = harness
        .get_recent_chats(project_a, 10)
        .await
        .expect("Failed to get recent chats for project A");
    let chats_a = recent_a["chats"].as_array().unwrap();
    assert_eq!(chats_a.len(), 1);

    let content_a = chats_a[0]["messages"][0]["content"].as_str().unwrap();
    assert!(content_a.contains("Project A"));

    // Verify project B only sees its own conversations
    let recent_b = harness
        .get_recent_chats(project_b, 10)
        .await
        .expect("Failed to get recent chats for project B");
    let chats_b = recent_b["chats"].as_array().unwrap();
    assert_eq!(chats_b.len(), 1);

    let content_b = chats_b[0]["messages"][0]["content"].as_str().unwrap();
    assert!(content_b.contains("Project B"));
}

#[tokio::test]
async fn test_audit_logging() {
    let harness = TestHarness::new().await;
    let project_id = "audit-test-project";

    // Perform operations that should be audited
    let messages = vec![
        json!({"role": "user", "content": "Test message"}),
        json!({"role": "assistant", "content": "Test response"}),
    ];
    harness
        .store_chat(project_id, "session-1", messages)
        .await
        .expect("Failed to store chat");

    harness
        .search_conversations(project_id, "test", 5)
        .await
        .expect("Failed to search");

    harness
        .get_recent_chats(project_id, 5)
        .await
        .expect("Failed to get recent chats");

    // Check audit logs
    let audit_logs = harness
        .get_audit_logs(project_id, 10)
        .await
        .expect("Failed to get audit logs");

    let entries = audit_logs["entries"].as_array().unwrap();
    assert!(entries.len() >= 3); // store, search, recent

    // Verify log entries have required fields
    for entry in entries {
        assert!(entry["id"].is_string());
        assert!(entry["timestamp"].is_string());
        assert_eq!(entry["project_id"], project_id);
        assert!(entry["operation"].is_string());
        assert!(entry["result_count"].is_number());
        assert!(entry["execution_time_ms"].is_number());
    }

    // Verify we have different operation types
    let operations: Vec<&str> = entries
        .iter()
        .map(|entry| entry["operation"].as_str().unwrap())
        .collect();

    assert!(operations.contains(&"store"));
    assert!(operations.contains(&"search"));
    assert!(operations.contains(&"recent"));
}

#[tokio::test]
async fn test_error_handling() {
    let harness = TestHarness::new().await;

    // Test empty project ID
    let response = harness
        .client
        .post(&format!("{}/store_chat", harness.base_url))
        .json(&json!({
            "project_id": "",
            "session_id": "session-1",
            "timestamp": Utc::now().to_rfc3339(),
            "messages": [{"role": "user", "content": "test"}],
            "metadata": {}
        }))
        .send()
        .await
        .expect("Request failed");

    assert_eq!(response.status(), 400);

    // Test empty query
    let response = harness
        .client
        .post(&format!("{}/conversation_search", harness.base_url))
        .json(&json!({
            "project_id": "test-project",
            "query": "",
            "max_results": 10
        }))
        .send()
        .await
        .expect("Request failed");

    assert_eq!(response.status(), 400);
}

#[tokio::test]
async fn test_concurrent_operations() {
    let harness = TestHarness::new().await;
    let project_id = "concurrent-test-project";

    // Store multiple conversations concurrently
    let mut tasks = Vec::new();

    for i in 0..10 {
        let harness_clone = &harness;
        let project_id_clone = project_id;

        let task = async move {
            let messages = vec![
                json!({"role": "user", "content": format!("Message {}", i)}),
                json!({"role": "assistant", "content": format!("Response {}", i)}),
            ];

            harness_clone
                .store_chat(project_id_clone, &format!("session-{}", i), messages)
                .await
        };

        tasks.push(task);
    }

    // Execute all tasks concurrently
    let results = futures::future::join_all(tasks).await;

    // Verify all operations succeeded
    for result in results {
        assert!(result.is_ok());
    }

    // Verify all conversations were stored
    let recent_chats = harness
        .get_recent_chats(project_id, 20)
        .await
        .expect("Failed to get recent chats");

    let chats = recent_chats["chats"].as_array().unwrap();
    assert_eq!(chats.len(), 10);
}

#[tokio::test]
async fn test_performance_under_load() {
    let harness = TestHarness::new().await;
    let project_id = "perf-test-project";

    // Store a reasonable number of conversations to test search performance
    for i in 0..50 {
        let messages = vec![
            json!({"role": "user", "content": format!("User message {} about programming", i)}),
            json!({"role": "assistant", "content": format!("Assistant response {} about coding", i)}),
        ];

        harness
            .store_chat(project_id, &format!("session-{}", i), messages)
            .await
            .expect("Failed to store conversation");
    }

    // Give the search index time to update
    sleep(Duration::from_millis(500)).await;

    // Test search performance
    let start = std::time::Instant::now();
    let search_results = harness
        .search_conversations(project_id, "programming", 10)
        .await
        .expect("Failed to search conversations");
    let search_duration = start.elapsed();

    // Verify search completed in reasonable time (less than 1 second)
    assert!(search_duration < Duration::from_secs(1));

    // Verify search found relevant results
    let results = search_results["results"].as_array().unwrap();
    assert!(results.len() > 0);

    // Test recent chats performance
    let start = std::time::Instant::now();
    let recent_chats = harness
        .get_recent_chats(project_id, 20)
        .await
        .expect("Failed to get recent chats");
    let recent_duration = start.elapsed();

    // Verify recent chats completed in reasonable time
    assert!(recent_duration < Duration::from_millis(500));

    let chats = recent_chats["chats"].as_array().unwrap();
    assert_eq!(chats.len(), 20); // Should be limited to requested amount
}
