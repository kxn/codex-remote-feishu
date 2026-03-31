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

    tracing_subscriber::fmt()
        .with_env_filter(env_filter)
        .with_writer(std::io::stderr)
        .init();

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
