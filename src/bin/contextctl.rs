use anyhow::Result;
use chrono::{DateTime, Utc};
use clap::{Arg, Command, Subcommand};
use reqwest::Client;
use serde_json::{json, Value};
use std::collections::HashMap;

#[tokio::main]
async fn main() -> Result<()> {
    let matches = Command::new("contextctl")
        .version("0.1.0")
        .about("CLI client for contextd memory daemon")
        .arg(
            Arg::new("url")
                .long("url")
                .short('u')
                .value_name("URL")
                .help("contextd server URL")
                .default_value("http://127.0.0.1:8080"),
        )
        .subcommand(
            Command::new("store")
                .about("Store a chat transcript")
                .arg(
                    Arg::new("project")
                        .long("project")
                        .short('p')
                        .value_name("PROJECT_ID")
                        .help("Project ID")
                        .required(true),
                )
                .arg(
                    Arg::new("session")
                        .long("session")
                        .short('s')
                        .value_name("SESSION_ID")
                        .help("Session ID")
                        .required(true),
                )
                .arg(
                    Arg::new("user-message")
                        .long("user")
                        .value_name("MESSAGE")
                        .help("User message")
                        .required(true),
                )
                .arg(
                    Arg::new("assistant-message")
                        .long("assistant")
                        .value_name("MESSAGE")
                        .help("Assistant message")
                        .required(true),
                ),
        )
        .subcommand(
            Command::new("search")
                .about("Search conversation history")
                .arg(
                    Arg::new("project")
                        .long("project")
                        .short('p')
                        .value_name("PROJECT_ID")
                        .help("Project ID")
                        .required(true),
                )
                .arg(
                    Arg::new("query")
                        .long("query")
                        .short('q')
                        .value_name("QUERY")
                        .help("Search query")
                        .required(true),
                )
                .arg(
                    Arg::new("limit")
                        .long("limit")
                        .short('l')
                        .value_name("COUNT")
                        .help("Maximum number of results")
                        .default_value("10"),
                )
                .arg(
                    Arg::new("session")
                        .long("session")
                        .short('s')
                        .value_name("SESSION_ID")
                        .help("Filter by session ID"),
                ),
        )
        .subcommand(
            Command::new("recent")
                .about("Get recent chat history")
                .arg(
                    Arg::new("project")
                        .long("project")
                        .short('p')
                        .value_name("PROJECT_ID")
                        .help("Project ID")
                        .required(true),
                )
                .arg(
                    Arg::new("limit")
                        .long("limit")
                        .short('l')
                        .value_name("COUNT")
                        .help("Maximum number of results")
                        .default_value("20"),
                )
                .arg(
                    Arg::new("session")
                        .long("session")
                        .short('s')
                        .value_name("SESSION_ID")
                        .help("Filter by session ID"),
                ),
        )
        .subcommand(
            Command::new("audit")
                .about("View audit logs")
                .arg(
                    Arg::new("project")
                        .long("project")
                        .short('p')
                        .value_name("PROJECT_ID")
                        .help("Project ID")
                        .required(true),
                )
                .arg(
                    Arg::new("limit")
                        .long("limit")
                        .short('l')
                        .value_name("COUNT")
                        .help("Maximum number of entries")
                        .default_value("50"),
                ),
        )
        .subcommand(Command::new("health").about("Check server health"))
        .subcommand(Command::new("stats").about("Show server statistics"))
        .subcommand_required(true)
        .get_matches();

    let base_url = matches.get_one::<String>("url").unwrap();
    let client = Client::new();

    match matches.subcommand() {
        Some(("store", sub_matches)) => {
            let project_id = sub_matches.get_one::<String>("project").unwrap();
            let session_id = sub_matches.get_one::<String>("session").unwrap();
            let user_message = sub_matches.get_one::<String>("user-message").unwrap();
            let assistant_message = sub_matches.get_one::<String>("assistant-message").unwrap();

            let request = json!({
                "project_id": project_id,
                "session_id": session_id,
                "timestamp": Utc::now().to_rfc3339(),
                "messages": [
                    {"role": "user", "content": user_message},
                    {"role": "assistant", "content": assistant_message}
                ],
                "metadata": {}
            });

            let response = client
                .post(&format!("{}/store_chat", base_url))
                .json(&request)
                .send()
                .await?;

            if response.status().is_success() {
                let result: Value = response.json().await?;
                println!("Stored transcript: {}", result["transcript_id"]);
            } else {
                let error: Value = response.json().await?;
                eprintln!("Error: {}", error);
            }
        }

        Some(("search", sub_matches)) => {
            let project_id = sub_matches.get_one::<String>("project").unwrap();
            let query = sub_matches.get_one::<String>("query").unwrap();
            let limit: usize = sub_matches.get_one::<String>("limit").unwrap().parse()?;
            let session_id = sub_matches.get_one::<String>("session");

            let request = json!({
                "project_id": project_id,
                "query": query,
                "max_results": limit,
                "session_id": session_id
            });

            let response = client
                .post(&format!("{}/conversation_search", base_url))
                .json(&request)
                .send()
                .await?;

            if response.status().is_success() {
                let result: Value = response.json().await?;
                print_search_results(&result)?;
            } else {
                let error: Value = response.json().await?;
                eprintln!("Error: {}", error);
            }
        }

        Some(("recent", sub_matches)) => {
            let project_id = sub_matches.get_one::<String>("project").unwrap();
            let limit: usize = sub_matches.get_one::<String>("limit").unwrap().parse()?;
            let session_id = sub_matches.get_one::<String>("session");

            let request = json!({
                "project_id": project_id,
                "limit": limit,
                "session_id": session_id
            });

            let response = client
                .post(&format!("{}/recent_chats", base_url))
                .json(&request)
                .send()
                .await?;

            if response.status().is_success() {
                let result: Value = response.json().await?;
                print_recent_chats(&result)?;
            } else {
                let error: Value = response.json().await?;
                eprintln!("Error: {}", error);
            }
        }

        Some(("audit", sub_matches)) => {
            let project_id = sub_matches.get_one::<String>("project").unwrap();
            let limit: usize = sub_matches.get_one::<String>("limit").unwrap().parse()?;

            let request = json!({
                "project_id": project_id,
                "limit": limit
            });

            let response = client
                .post(&format!("{}/audit/logs", base_url))
                .json(&request)
                .send()
                .await?;

            if response.status().is_success() {
                let result: Value = response.json().await?;
                print_audit_logs(&result)?;
            } else {
                let error: Value = response.json().await?;
                eprintln!("Error: {}", error);
            }
        }

        Some(("health", _)) => {
            let response = client.get(&format!("{}/health", base_url)).send().await?;

            if response.status().is_success() {
                let result: Value = response.json().await?;
                println!("Status: {}", result["status"]);
                println!(
                    "Version: {}",
                    result.get("version").unwrap_or(&json!("unknown"))
                );
                println!("Timestamp: {}", result["timestamp"]);
            } else {
                eprintln!("Health check failed: {}", response.status());
            }
        }

        Some(("stats", _)) => {
            let response = client.get(&format!("{}/stats", base_url)).send().await?;

            if response.status().is_success() {
                let result: Value = response.json().await?;
                println!("Server Statistics:");
                println!(
                    "  Version: {}",
                    result.get("version").unwrap_or(&json!("unknown"))
                );

                if let Some(index) = result.get("index") {
                    println!("  Index:");
                    println!(
                        "    Total documents: {}",
                        index.get("total_documents").unwrap_or(&json!(0))
                    );
                    println!(
                        "    Index size: {} bytes",
                        index.get("index_size_bytes").unwrap_or(&json!(0))
                    );
                }
            } else {
                let error: Value = response.json().await?;
                eprintln!("Error: {}", error);
            }
        }

        _ => unreachable!(),
    }

    Ok(())
}

fn print_search_results(result: &Value) -> Result<()> {
    let results = result["results"].as_array().unwrap();
    let query_time = result["query_time_ms"].as_u64().unwrap_or(0);

    println!("Found {} results in {}ms:", results.len(), query_time);
    println!();

    for (i, result) in results.iter().enumerate() {
        println!("Result {}:", i + 1);
        println!("  ID: {}", result["transcript_id"]);
        println!("  Session: {}", result["session_id"]);
        println!("  Timestamp: {}", result["timestamp"]);
        println!("  Score: {:.3}", result["score"].as_f64().unwrap_or(0.0));

        if let Some(messages) = result["messages"].as_array() {
            println!("  Messages:");
            for msg in messages {
                let role = msg["role"].as_str().unwrap_or("unknown");
                let content = msg["content"].as_str().unwrap_or("");
                println!("    {}: {}", role, truncate_string(content, 100));
            }
        }

        println!();
    }

    Ok(())
}

fn print_recent_chats(result: &Value) -> Result<()> {
    let chats = result["chats"].as_array().unwrap();

    println!("Recent {} chats:", chats.len());
    println!();

    for (i, chat) in chats.iter().enumerate() {
        println!("Chat {}:", i + 1);
        println!("  ID: {}", chat["transcript_id"]);
        println!("  Session: {}", chat["session_id"]);
        println!("  Timestamp: {}", chat["timestamp"]);

        if let Some(messages) = chat["messages"].as_array() {
            println!("  Messages:");
            for msg in messages {
                let role = msg["role"].as_str().unwrap_or("unknown");
                let content = msg["content"].as_str().unwrap_or("");
                println!("    {}: {}", role, truncate_string(content, 100));
            }
        }

        println!();
    }

    Ok(())
}

fn print_audit_logs(result: &Value) -> Result<()> {
    let entries = result["entries"].as_array().unwrap();

    println!("Audit log entries ({}):", entries.len());
    println!();

    for entry in entries {
        println!("Entry ID: {}", entry["id"]);
        println!("  Timestamp: {}", entry["timestamp"]);
        println!("  Operation: {}", entry["operation"]);

        if let Some(query) = entry["query"].as_str() {
            println!("  Query: {}", truncate_string(query, 80));
        }

        println!("  Result count: {}", entry["result_count"]);
        println!("  Execution time: {}ms", entry["execution_time_ms"]);
        println!();
    }

    Ok(())
}

fn truncate_string(s: &str, max_len: usize) -> String {
    if s.len() <= max_len {
        s.to_string()
    } else {
        format!("{}...", &s[..max_len])
    }
}
