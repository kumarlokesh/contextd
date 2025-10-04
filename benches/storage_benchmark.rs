use contextd::config::StorageConfig;
use contextd::models::{ChatMessage, Transcript};
use contextd::storage::MemoryStore;
use criterion::{black_box, criterion_group, criterion_main, Criterion};
use std::collections::HashMap;
use tempfile::TempDir;
use tokio::runtime::Runtime;
fn create_test_transcript(id: usize) -> Transcript {
    let messages = vec![
        ChatMessage {
            role: "user".to_string(),
            content: format!("Test user message {}", id),
            metadata: HashMap::new(),
        },
        ChatMessage {
            role: "assistant".to_string(),
            content: format!("Test assistant response {}", id),
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

async fn setup_memory_store() -> (MemoryStore, TempDir) {
    let temp_dir = TempDir::new().expect("Failed to create temp directory");
    let temp_path = temp_dir.path().to_str().unwrap();

    let config = StorageConfig {
        storage_type: "sqlite".to_string(),
        sqlite_path: Some(format!("{}/bench.db", temp_path)),
        postgres_url: None,
        compression: true,
    };

    let store = MemoryStore::new(&config)
        .await
        .expect("Failed to create memory store");
    (store, temp_dir)
}

fn bench_store_transcript(c: &mut Criterion) {
    let rt = Runtime::new().unwrap();

    c.bench_function("store_transcript", |b| {
        b.iter_custom(|iters| {
            rt.block_on(async move {
                let start = std::time::Instant::now();
                for _i in 0..iters {
                    let (store, _temp_dir) = setup_memory_store().await;
                    let transcript = create_test_transcript(1);
                    black_box(store.store_transcript(&transcript).await.unwrap());
                }
                start.elapsed()
            })
        });
    });
}

fn bench_store_multiple_transcripts(c: &mut Criterion) {
    let rt = Runtime::new().unwrap();

    c.bench_function("store_100_transcripts", |b| {
        b.iter_custom(|iters| {
            rt.block_on(async move {
                let start = std::time::Instant::now();
                for _i in 0..iters {
                    let (store, _temp_dir) = setup_memory_store().await;
                    for j in 0..100 {
                        let transcript = create_test_transcript(j);
                        store.store_transcript(&transcript).await.unwrap();
                    }
                }
                start.elapsed()
            })
        });
    });
}

fn bench_get_transcript(c: &mut Criterion) {
    let rt = Runtime::new().unwrap();

    c.bench_function("get_transcript", |b| {
        b.iter_custom(|iters| {
            rt.block_on(async move {
                let start = std::time::Instant::now();
                for _i in 0..iters {
                    // Setup store and data for each iteration
                    let (store, _temp_dir) = setup_memory_store().await;
                    let transcript = create_test_transcript(1);
                    let transcript_id = store.store_transcript(&transcript).await.unwrap();

                    black_box(
                        store
                            .get_transcript(transcript_id, "benchmark-project")
                            .await
                            .unwrap(),
                    );
                }
                start.elapsed()
            })
        });
    });
}

fn bench_get_recent_transcripts(c: &mut Criterion) {
    let rt = Runtime::new().unwrap();

    c.bench_function("get_recent_100_transcripts", |b| {
        b.iter_custom(|iters| {
            rt.block_on(async move {
                let start = std::time::Instant::now();
                for _i in 0..iters {
                    // Setup store and data for each iteration
                    let (store, _temp_dir) = setup_memory_store().await;

                    // Store 100 transcripts for each iteration
                    for j in 0..100 {
                        let transcript = create_test_transcript(j);
                        store.store_transcript(&transcript).await.unwrap();
                    }

                    black_box(
                        store
                            .get_recent_transcripts("benchmark-project", None, 100)
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
    bench_store_transcript,
    bench_store_multiple_transcripts,
    bench_get_transcript,
    bench_get_recent_transcripts
);
criterion_main!(benches);
