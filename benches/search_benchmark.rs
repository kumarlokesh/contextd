use contextd::config::SearchConfig;
use contextd::models::{ChatMessage, Transcript};
use contextd::search::IndexEngine;
use criterion::{black_box, criterion_group, criterion_main, Criterion};
use std::collections::HashMap;
use tempfile::TempDir;
use tokio::runtime::Runtime;

fn create_test_transcript(id: usize, content_variety: &str) -> Transcript {
    let messages = vec![
        ChatMessage {
            role: "user".to_string(),
            content: format!("User asking about {} topic number {}", content_variety, id),
            metadata: HashMap::new(),
        },
        ChatMessage {
            role: "assistant".to_string(),
            content: format!(
                "Assistant explaining {} concepts for question {}",
                content_variety, id
            ),
            metadata: HashMap::new(),
        },
    ];

    Transcript::new(
        "benchmark-project".to_string(),
        format!("session-{}", id),
        chrono::Utc::now(),
        messages,
        HashMap::new(),
    )
}

async fn setup_search_engine() -> (IndexEngine, TempDir) {
    let temp_dir = TempDir::new().expect("Failed to create temp directory");
    let temp_path = temp_dir.path().to_str().unwrap();

    let config = SearchConfig {
        engine: "tantivy".to_string(),
        index_path: format!("{}/index", temp_path),
        vector_search: false,
        vector_dimension: 384,
    };

    let engine = IndexEngine::new(&config)
        .await
        .expect("Failed to create search engine");
    (engine, temp_dir)
}

fn bench_index_transcript(c: &mut Criterion) {
    let rt = Runtime::new().unwrap();

    c.bench_function("index_transcript", |b| {
        b.iter_custom(|iters| {
            rt.block_on(async move {
                let start = std::time::Instant::now();
                for _i in 0..iters {
                    let (mut engine, _temp_dir) = setup_search_engine().await;
                    let transcript = create_test_transcript(1, "programming");

                    black_box(engine.index_transcript(&transcript).await.unwrap());
                    engine.commit().await.unwrap();
                }
                start.elapsed()
            })
        });
    });
}

fn bench_index_multiple_transcripts(c: &mut Criterion) {
    let rt = Runtime::new().unwrap();

    c.bench_function("index_1000_transcripts", |b| {
        b.iter_custom(|iters| {
            rt.block_on(async move {
                let start = std::time::Instant::now();
                for _i in 0..iters {
                    let (mut engine, _temp_dir) = setup_search_engine().await;

                    // Index test documents
                    let topics = ["programming", "mathematics", "science"];
                    for j in 0..100 {
                        let topic = topics[j % topics.len()];
                        let transcript = create_test_transcript(j, topic);
                        black_box(engine.index_transcript(&transcript).await.unwrap());
                    }
                    black_box(engine.commit().await.unwrap());
                }
                start.elapsed()
            })
        });
    });
}

fn bench_search_small_index(c: &mut Criterion) {
    let rt = Runtime::new().unwrap();

    c.bench_function("search_small_index_100_docs", |b| {
        b.iter_custom(|iters| {
            rt.block_on(async move {
                // Create fresh engine for each benchmark run
                let (mut engine, _temp_dir) = setup_search_engine().await;

                let topics = [
                    "programming",
                    "mathematics",
                    "science",
                    "history",
                    "literature",
                ];

                for i in 0..100 {
                    let topic = topics[i % topics.len()];
                    let transcript = create_test_transcript(i, topic);
                    engine.index_transcript(&transcript).await.unwrap();
                }

                engine.commit().await.unwrap();

                let start = std::time::Instant::now();
                for _i in 0..iters {
                    black_box(
                        engine
                            .search("benchmark-project", "programming", 10, None, None)
                            .await
                            .unwrap(),
                    );
                }
                start.elapsed()
            })
        });
    });
}

fn bench_search_large_index(c: &mut Criterion) {
    let rt = Runtime::new().unwrap();

    c.bench_function("search_large_index_10k_docs", |b| {
        b.iter_custom(|iters| {
            rt.block_on(async move {
                // Create fresh engine for each benchmark run
                let (mut engine, _temp_dir) = setup_search_engine().await;

                let topics = [
                    "programming",
                    "mathematics",
                    "science",
                    "history",
                    "literature",
                    "physics",
                    "chemistry",
                    "biology",
                    "psychology",
                    "philosophy",
                ];

                for i in 0..1000 {
                    // Use fewer docs for faster benchmarking
                    let topic = topics[i % topics.len()];
                    let transcript = create_test_transcript(i, topic);
                    engine.index_transcript(&transcript).await.unwrap();
                }

                engine.commit().await.unwrap();

                let start = std::time::Instant::now();
                for _i in 0..iters {
                    black_box(
                        engine
                            .search("benchmark-project", "programming", 10, None, None)
                            .await
                            .unwrap(),
                    );
                }
                start.elapsed()
            })
        });
    });
}

fn bench_search_complex_query(c: &mut Criterion) {
    let rt = Runtime::new().unwrap();

    c.bench_function("search_complex_query", |b| {
        b.iter_custom(|iters| {
            rt.block_on(async move {
                // Create fresh engine for each benchmark run
                let (mut engine, _temp_dir) = setup_search_engine().await;

                let complex_topics = [
                    "advanced rust programming techniques",
                    "mathematical optimization algorithms",
                    "quantum physics and mechanics",
                    "machine learning neural networks",
                    "distributed systems architecture",
                ];

                for i in 0..100 {
                    // Use fewer docs for faster benchmarking
                    let topic = complex_topics[i % complex_topics.len()];
                    let transcript = create_test_transcript(i, topic);
                    engine.index_transcript(&transcript).await.unwrap();
                }

                engine.commit().await.unwrap();

                let start = std::time::Instant::now();
                for _i in 0..iters {
                    // Complex multi-term query
                    black_box(
                        engine
                            .search(
                                "benchmark-project",
                                "advanced programming optimization neural networks",
                                20,
                                None,
                                None,
                            )
                            .await
                            .unwrap(),
                    );
                }
                start.elapsed()
            })
        });
    });
}

criterion_group!(
    benches,
    bench_index_transcript,
    bench_index_multiple_transcripts,
    bench_search_small_index,
    bench_search_large_index,
    bench_search_complex_query
);
criterion_main!(benches);
