use std::fmt;
use std::path::{Path, PathBuf};

use clap::Parser;
use tokio_tungstenite::tungstenite::http::Uri;

/// Wrapper CLI and environment configuration.
#[derive(Parser, Debug)]
#[command(version, about = "Codex Relay Wrapper - stdio proxy for the codex CLI")]
struct Cli {
    /// Path to the real codex binary
    #[arg(long, env = "CODEX_REAL_BINARY", default_value = "codex")]
    codex_binary: String,

    /// Relay server websocket URL
    #[arg(long, env = "RELAY_SERVER_URL")]
    relay_url: Option<String>,

    /// Human-friendly session display name
    #[arg(long)]
    name: Option<String>,

    /// Additional arguments to pass to the codex binary
    #[arg(trailing_var_arg = true)]
    args: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub struct WrapperConfig {
    pub binary_path: String,
    pub relay_url: Option<String>,
    pub session_name: String,
    pub args: Vec<String>,
}

#[derive(Debug, Clone, PartialEq, Eq)]
pub enum ConfigWarning {
    MissingRelayUrl,
    InvalidRelayUrl,
}

impl fmt::Display for ConfigWarning {
    fn fmt(&self, f: &mut fmt::Formatter<'_>) -> fmt::Result {
        match self {
            Self::MissingRelayUrl => {
                write!(
                    f,
                    "relay server URL is not configured; starting in degraded mode without relay connectivity"
                )
            }
            Self::InvalidRelayUrl => {
                write!(
                    f,
                    "relay server URL is invalid; starting in degraded mode without relay connectivity"
                )
            }
        }
    }
}

pub fn parse_wrapper_config() -> (WrapperConfig, Vec<ConfigWarning>) {
    let cli = Cli::parse();
    let cwd = std::env::current_dir().unwrap_or_else(|_| PathBuf::from("."));
    resolve_wrapper_config(cli, &cwd)
}

fn resolve_wrapper_config(cli: Cli, cwd: &Path) -> (WrapperConfig, Vec<ConfigWarning>) {
    let (relay_url, relay_warnings) = resolve_relay_url(cli.relay_url);

    let config = WrapperConfig {
        binary_path: cli.codex_binary,
        relay_url,
        session_name: cli.name.unwrap_or_else(|| default_session_name(cwd)),
        args: cli.args,
    };

    (config, relay_warnings)
}

fn resolve_relay_url(relay_url: Option<String>) -> (Option<String>, Vec<ConfigWarning>) {
    match relay_url {
        Some(value) if is_valid_relay_url(&value) => (Some(value), Vec::new()),
        Some(_) => (None, vec![ConfigWarning::InvalidRelayUrl]),
        None => (None, vec![ConfigWarning::MissingRelayUrl]),
    }
}

fn is_valid_relay_url(value: &str) -> bool {
    value.parse::<Uri>().ok().is_some_and(|uri| {
        matches!(uri.scheme_str(), Some("ws" | "wss")) && uri.authority().is_some()
    })
}

fn default_session_name(cwd: &Path) -> String {
    let name = cwd
        .file_name()
        .filter(|value| !value.is_empty())
        .or_else(|| {
            cwd.components().next_back().and_then(|component| {
                let value = component.as_os_str();
                if value.is_empty() {
                    None
                } else {
                    Some(value)
                }
            })
        })
        .unwrap_or(cwd.as_os_str());

    let session_name = name.to_string_lossy().trim().to_string();
    if session_name.is_empty() {
        "session".to_string()
    } else {
        session_name
    }
}

#[cfg(test)]
mod tests {
    use std::path::Path;

    use super::{resolve_wrapper_config, Cli, ConfigWarning};

    #[test]
    fn defaults_codex_binary_to_codex() {
        let cli = Cli {
            codex_binary: "codex".to_string(),
            relay_url: None,
            name: None,
            args: Vec::new(),
        };

        let (config, warnings) = resolve_wrapper_config(cli, Path::new("/tmp/example-project"));

        assert_eq!(config.binary_path, "codex");
        assert_eq!(config.session_name, "example-project");
        assert_eq!(warnings, vec![ConfigWarning::MissingRelayUrl]);
    }

    #[test]
    fn defaults_session_name_to_cwd_basename() {
        let cli = Cli {
            codex_binary: "/usr/bin/codex".to_string(),
            relay_url: Some("ws://localhost:9500".to_string()),
            name: None,
            args: Vec::new(),
        };

        let (config, warnings) = resolve_wrapper_config(cli, Path::new("/workspaces/demo-app"));

        assert_eq!(config.session_name, "demo-app");
        assert!(warnings.is_empty());
    }

    #[test]
    fn uses_explicit_session_name_when_provided() {
        let cli = Cli {
            codex_binary: "/usr/bin/codex".to_string(),
            relay_url: Some("ws://localhost:9500".to_string()),
            name: Some("custom-name".to_string()),
            args: vec!["--json".to_string()],
        };

        let (config, warnings) = resolve_wrapper_config(cli, Path::new("/workspaces/demo-app"));

        assert_eq!(config.session_name, "custom-name");
        assert_eq!(config.args, vec!["--json"]);
        assert!(warnings.is_empty());
    }

    #[test]
    fn keeps_valid_relay_url() {
        let cli = Cli {
            codex_binary: "codex".to_string(),
            relay_url: Some("wss://relay.example.test/session".to_string()),
            name: None,
            args: Vec::new(),
        };

        let (config, warnings) = resolve_wrapper_config(cli, Path::new("/tmp/project"));

        assert_eq!(
            config.relay_url.as_deref(),
            Some("wss://relay.example.test/session")
        );
        assert!(warnings.is_empty());
    }

    #[test]
    fn warns_when_relay_url_is_missing() {
        let cli = Cli {
            codex_binary: "codex".to_string(),
            relay_url: None,
            name: None,
            args: Vec::new(),
        };

        let (config, warnings) = resolve_wrapper_config(cli, Path::new("/tmp/project"));

        assert_eq!(config.relay_url, None);
        assert_eq!(warnings, vec![ConfigWarning::MissingRelayUrl]);
    }

    #[test]
    fn warns_when_relay_url_is_invalid() {
        let cli = Cli {
            codex_binary: "codex".to_string(),
            relay_url: Some("not-a-websocket-url".to_string()),
            name: None,
            args: Vec::new(),
        };

        let (config, warnings) = resolve_wrapper_config(cli, Path::new("/tmp/project"));

        assert_eq!(config.relay_url, None);
        assert_eq!(warnings, vec![ConfigWarning::InvalidRelayUrl]);
    }
}
