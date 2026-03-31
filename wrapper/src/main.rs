mod proxy;

use anyhow::Result;
use clap::Parser;
use tracing_subscriber::EnvFilter;

/// Codex Relay Wrapper - stdio proxy for the codex CLI
#[derive(Parser, Debug)]
#[command(version, about)]
struct Cli {
    /// Path to the real codex binary
    #[arg(long, env = "CODEX_REAL_BINARY", default_value = "codex")]
    codex_binary: String,

    /// Additional arguments to pass to the codex binary
    #[arg(trailing_var_arg = true)]
    args: Vec<String>,
}

#[tokio::main]
async fn main() -> Result<()> {
    tracing_subscriber::fmt()
        .with_env_filter(EnvFilter::from_default_env())
        .with_writer(std::io::stderr)
        .init();

    let cli = Cli::parse();

    let config = proxy::ProxyConfig {
        binary_path: cli.codex_binary,
        args: cli.args,
    };

    let exit_code = proxy::run_proxy(&config).await?;

    std::process::exit(exit_code);
}
