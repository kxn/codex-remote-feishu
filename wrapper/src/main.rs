use std::fs::OpenOptions;
use std::io::Write;
use std::path::PathBuf;

mod classifier;
mod config;
mod proxy;
mod ws_client;

use anyhow::Result;
use tracing::warn;
use tracing_subscriber::EnvFilter;

#[tokio::main]
async fn main() -> Result<()> {
    let env_filter =
        EnvFilter::try_from_default_env().unwrap_or_else(|_| EnvFilter::new("warn"));

    initialize_tracing(env_filter);

    let (wrapper_config, warnings) = config::parse_wrapper_config();
    for warning in warnings {
        warn!(warning = %warning, "wrapper configuration warning");
    }

    let config = proxy::ProxyConfig {
        binary_path: wrapper_config.binary_path,
        relay_url: wrapper_config.relay_url,
        session_name: wrapper_config.session_name,
        args: wrapper_config.args,
    };

    let exit_code = proxy::run_proxy(&config).await?;

    std::process::exit(exit_code);
}

fn initialize_tracing(env_filter: EnvFilter) {
    let log_path = tracing_log_path();
    let file_logging_enabled = OpenOptions::new()
        .create(true)
        .write(true)
        .truncate(true)
        .open(&log_path)
        .is_ok();

    tracing_subscriber::fmt()
        .with_env_filter(env_filter)
        .with_ansi(false)
        .with_writer(move || {
            if file_logging_enabled {
                match OpenOptions::new().create(true).append(true).open(&log_path) {
                    Ok(file) => Box::new(file) as Box<dyn Write + Send>,
                    Err(_) => Box::new(std::io::sink()) as Box<dyn Write + Send>,
                }
            } else {
                Box::new(std::io::sink()) as Box<dyn Write + Send>
            }
        })
        .init();
}

fn tracing_log_path() -> PathBuf {
    std::env::temp_dir().join(format!("codex-relay-wrapper-{}.log", std::process::id()))
}
