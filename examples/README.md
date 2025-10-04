# contextd Examples

This directory contains comprehensive examples demonstrating how to use contextd in various scenarios.

## Running Examples

First, start the contextd daemon:

```bash
# In the root directory
make serve
# or: cargo run -- serve
```

Then run the examples:

```bash
# Quick way - run all examples
make demo

# Or run individual examples
make run-basic-example      # Basic integration demo
make run-llm-example        # LLM memory integration demo  
make run-batch-example      # Batch operations demo

# Manual execution
cd examples
cargo run --bin basic_integration
cargo run --bin llm_memory_demo
cargo run --bin batch_operations
```

## Examples Overview

### 1. Basic Integration (`basic_integration.rs`)

Demonstrates core contextd functionality:

- **Health checks** - Verify server status
- **Storing conversations** - Save chat transcripts with metadata
- **Searching conversations** - Full-text search across stored chats
- **Recent chats** - Retrieve chronologically ordered conversations
- **Audit logging** - View transparency logs of all operations
- **Project isolation** - Verify data separation between projects

**Key APIs covered:**
```rust
// Store a conversation
client.store_chat(project_id, session_id, messages, metadata).await?;

// Search for relevant context  
client.search_conversations(project_id, query, max_results, session_filter).await?;

// Get recent conversations
client.get_recent_chats(project_id, limit, session_filter).await?;

// View audit trail
client.get_audit_logs(project_id, limit).await?;
```

### 2. LLM Memory Integration (`llm_memory_demo.rs`)

Shows how to integrate contextd as a memory backend for LLM systems:

- **Explicit memory retrieval** - Search for relevant context before generating responses
- **Memory-aware responses** - Use retrieved context to inform LLM outputs
- **Conversation storage** - Automatically store all interactions for future retrieval
- **Audit transparency** - Track exactly what memory was accessed for each response

**Integration pattern:**
```rust
// 1. Search for relevant context
let context = client.search_conversations(project_id, user_query, 3, None).await?;

// 2. Generate response using context (your LLM here)
let response = llm.generate_with_context(user_query, context);

// 3. Store the interaction for future retrieval
client.store_chat(project_id, session_id, messages, metadata).await?;
```

### 3. Batch Operations (`batch_operations.rs`)

Demonstrates efficient batch processing and error handling:

- **Concurrent operations** - Process multiple requests in parallel with rate limiting
- **Error handling** - Robust error recovery and retry logic
- **Performance testing** - Generate synthetic data for load testing
- **Audit analysis** - Analyze performance metrics from audit logs
- **Edge case handling** - Test various error conditions and limits

**Performance patterns:**
```rust
// Concurrent operations with semaphore limiting
let semaphore = tokio::sync::Semaphore::new(10);
for conversation in conversations {
    let permit = semaphore.acquire().await?;
    let task = tokio::spawn(async move {
        let _permit = permit; // Hold permit until completion
        client.store_chat(project_id, session_id, messages, None).await
    });
    tasks.push(task);
}
```

## API Examples by Use Case

### Health and Status Checking

```rust
// Check if contextd is running
match client.health_check().await {
    Ok(health) => println!("Server healthy: {}", health["status"]),
    Err(e) => println!("Server unavailable: {}", e),
}
```

### Storing Conversations

```rust
let messages = vec![
    ChatMessage {
        role: "user".to_string(),
        content: "How do I learn Rust?".to_string(),
        metadata: HashMap::new(),
    },
    ChatMessage {
        role: "assistant".to_string(),
        content: "Start with the Rust Book...".to_string(),
        metadata: HashMap::new(),
    },
];

let transcript_id = client.store_chat(
    "my-project",
    "session-123", 
    messages,
    Some(metadata)
).await?;
```

### Searching for Context

```rust
// Basic search
let results = client.search_conversations(
    "my-project",
    "Rust programming",
    10,
    None  // No session filter
).await?;

// Session-specific search
let results = client.search_conversations(
    "my-project", 
    "error handling",
    5,
    Some("session-123")  // Filter to specific session
).await?;
```

### Retrieving Recent Conversations

```rust
// Get recent across all sessions
let recent = client.get_recent_chats("my-project", 20, None).await?;

// Get recent from specific session
let recent = client.get_recent_chats("my-project", 10, Some("session-123")).await?;
```

### Audit and Transparency

```rust
// View all operations
let logs = client.get_audit_logs("my-project", 50).await?;

// Analyze operations
for log in logs {
    println!("Operation: {} took {}ms", 
             log["operation"], log["execution_time_ms"]);
}
```

## Error Handling Patterns

### Graceful Degradation

```rust
// Continue operation even if memory search fails
let context = match client.search_conversations(project_id, query, 3, None).await {
    Ok(results) => results,
    Err(e) => {
        tracing::warn!("Memory search failed: {}", e);
        SearchResponse { results: vec![], total_count: 0, query_time_ms: 0 }
    }
};
```

### Retry Logic

```rust
// Retry storage operations with exponential backoff
let mut retries = 0;
loop {
    match client.store_chat(project_id, session_id, messages.clone(), None).await {
        Ok(transcript_id) => break Ok(transcript_id),
        Err(e) if retries < 3 => {
            retries += 1;
            tokio::time::sleep(Duration::from_millis(100 * 2_u64.pow(retries))).await;
        }
        Err(e) => break Err(e),
    }
}
```

## Performance Considerations

### Concurrent Operations

```rust
// Limit concurrency to avoid overwhelming the server
let semaphore = tokio::sync::Semaphore::new(10);
let mut tasks = Vec::new();

for item in work_items {
    let permit = semaphore.acquire().await?;
    let client = client.clone();
    
    let task = tokio::spawn(async move {
        let _permit = permit;
        // Your operation here
        client.some_operation(item).await
    });
    
    tasks.push(task);
}

// Wait for all operations to complete
let results = futures::future::join_all(tasks).await;
```

### Batch Processing

```rust
// Process in batches to manage memory usage
for chunk in conversations.chunks(100) {
    for conversation in chunk {
        client.store_chat(project_id, session_id, messages, None).await?;
    }
    
    // Brief pause between batches
    tokio::time::sleep(Duration::from_millis(10)).await;
}
```

## Testing Your Integration

Each example includes comprehensive error testing. To test your own integration:

1. **Start with health checks** - Ensure connectivity
2. **Test error conditions** - Invalid inputs, network issues
3. **Verify audit logs** - Check that operations are properly logged
4. **Load testing** - Use batch operations to test under load
5. **Monitor performance** - Check execution times in audit logs

## Next Steps

After running these examples:

1. **Adapt to your use case** - Modify the client patterns for your specific needs
2. **Integrate with your LLM** - Replace the simulated LLM responses with actual model calls
3. **Configure for production** - See `contextd.toml` for production settings
4. **Monitor in production** - Use audit logs and health checks for operational visibility

For more information, see:
- [Architecture Documentation](../docs/ARCHITECTURE.md)
- [Configuration Guide](../contextd.toml)
- [API Reference](../docs/API.md)
- [Deployment Guide](../docker-compose.yml)
