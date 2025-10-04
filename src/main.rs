mod api;
mod audit;
mod config;
mod error;
mod models;
mod search;
mod storage;

use anyhow::Result;
use clap::{Arg, Command};
use tracing::{error, info};
use tracing_subscriber::{layer::SubscriberExt, util::SubscriberInitExt};

use crate::api::ApiServer;
use crate::config::Config;

#[tokio::main]
async fn main() -> Result<()> {
    // Initialize tracing
    tracing_subscriber::registry()
        .with(
            tracing_subscriber::EnvFilter::try_from_default_env()
                .unwrap_or_else(|_| "contextd=debug,tower_http=debug".into()),
        )
        .with(tracing_subscriber::fmt::layer())
        .init();

    let matches = Command::new("contextd")
        .version("0.1.0")
        .about("A transparent, privacy-first memory daemon for LLMs")
        .subcommand(
            Command::new("serve")
                .about("Start the contextd daemon")
                .arg(
                    Arg::new("config")
                        .short('c')
                        .long("config")
                        .value_name("FILE")
                        .help("Configuration file path")
                        .default_value("contextd.toml"),
                ),
        )
        .subcommand_required(true)
        .get_matches();

    match matches.subcommand() {
        Some(("serve", sub_matches)) => {
            let config_path = sub_matches.get_one::<String>("config").unwrap();

            info!("Loading configuration from {}", config_path);
            let config = Config::load(config_path)?;

            info!(
                "Starting contextd daemon on {}:{}",
                config.server.host, config.server.port
            );

            let server = ApiServer::new(config).await?;
            if let Err(e) = server.serve().await {
                error!("Server error: {}", e);
                std::process::exit(1);
            }
        }
        _ => unreachable!(),
    }

    Ok(())
}
