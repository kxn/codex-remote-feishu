use std::io;
use std::process::Stdio;
use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::{SystemTime, UNIX_EPOCH};

use anyhow::{Context, Result};
use serde_json::Value;
use tokio::io::{AsyncBufRead, AsyncBufReadExt, AsyncRead, AsyncWrite, AsyncWriteExt, BufReader};
use tokio::process::Command;
use tracing::{debug, error, info};

use crate::classifier::{MessageClassification, MessageClassifier};
use crate::ws_client::{self, OutboundRelayMessage, RelayRegistration};

/// Forward lines from an async reader to an async writer, byte-for-byte.
/// Each line is delimited by `\n`. The newline is included in the forwarded bytes.
/// Returns when the reader reaches EOF.
#[cfg(test)]
pub async fn forward_lines(
    mut reader: impl AsyncBufRead + Unpin,
    mut writer: impl AsyncWrite + Unpin,
) -> io::Result<()> {
    forward_lines_with_observer(&mut reader, &mut writer, |_| {}).await
}

async fn forward_lines_with_observer<R, W, F>(
    reader: &mut R,
    writer: &mut W,
    mut observer: F,
) -> io::Result<()>
where
    R: AsyncBufRead + Unpin,
    W: AsyncWrite + Unpin,
    F: FnMut(&[u8]),
{
    let mut buf = Vec::with_capacity(4096);
    loop {
        buf.clear();
        let n = reader.read_until(b'\n', &mut buf).await?;
        if n == 0 {
            // EOF
            break;
        }
        writer.write_all(&buf).await?;
        writer.flush().await?;
        observer(&buf);
    }
    Ok(())
}

/// Forward raw bytes from reader to writer (used for stderr passthrough).
pub async fn forward_bytes(
    mut reader: impl AsyncRead + Unpin,
    mut writer: impl AsyncWrite + Unpin,
) -> io::Result<u64> {
    tokio::io::copy(&mut reader, &mut writer).await
}

/// Configuration for the stdio proxy.
pub struct ProxyConfig {
    pub binary_path: String,
    pub relay_url: Option<String>,
    pub session_name: String,
    pub args: Vec<String>,
}

#[derive(Debug)]
pub enum ChildInputCommand {
    Write(Vec<u8>),
    Close,
}

/// Spawn the child process and run the stdio proxy.
///
/// This function:
/// 1. Spawns the child binary with piped stdin/stdout/stderr
/// 2. Forwards wrapper stdin → child stdin (line by line)
/// 3. Forwards child stdout → wrapper stdout (line by line)
/// 4. Forwards child stderr → wrapper stderr (raw bytes)
/// 5. Forwards SIGINT/SIGTERM to the child
/// 6. Returns the child's exit code
pub async fn run_proxy(config: &ProxyConfig) -> Result<i32> {
    run_proxy_with_io(
        config,
        tokio::io::stdin(),
        tokio::io::stdout(),
        tokio::io::stderr(),
    )
    .await
}

/// Run the stdio proxy with custom IO streams (for testing).
pub async fn run_proxy_with_io<R, W, E>(
    config: &ProxyConfig,
    wrapper_stdin: R,
    wrapper_stdout: W,
    wrapper_stderr: E,
) -> Result<i32>
where
    R: AsyncRead + Unpin + Send + 'static,
    W: AsyncWrite + Unpin + Send + 'static,
    E: AsyncWrite + Unpin + Send + 'static,
{
    info!(
        binary = %config.binary_path,
        session_name = %config.session_name,
        relay_configured = config.relay_url.is_some(),
        "Spawning child process"
    );

    let mut child = Command::new(&config.binary_path)
        .args(&config.args)
        .stdin(Stdio::piped())
        .stdout(Stdio::piped())
        .stderr(Stdio::piped())
        .kill_on_drop(false)
        .spawn()
        .with_context(|| format!("Failed to spawn child process: {}", config.binary_path))?;

    let child_stdin = child
        .stdin
        .take()
        .context("Failed to open child stdin")?;
    let child_stdout = child
        .stdout
        .take()
        .context("Failed to open child stdout")?;
    let child_stderr = child
        .stderr
        .take()
        .context("Failed to open child stderr")?;

    let child_pid = child.id();

    debug!(pid = ?child_pid, "Child process spawned");

    let attached = Arc::new(AtomicBool::new(false));
    let (child_input_tx, mut child_input_rx) = tokio::sync::mpsc::unbounded_channel();

    let relay_runtime = config.relay_url.clone().map(|relay_url| {
        let registration = RelayRegistration {
            session_id: generate_session_id(),
            display_name: config.session_name.clone(),
            workspace_path: current_workspace_path(),
            wrapper_version: env!("CARGO_PKG_VERSION").to_string(),
            pid: std::process::id(),
        };

        let (relay_tx, relay_handle) = ws_client::spawn_relay_client(
            relay_url,
            registration,
            Arc::clone(&attached),
            child_input_tx.clone(),
        );

        (relay_tx, relay_handle)
    });

    let relay_tx = relay_runtime.as_ref().map(|(tx, _)| tx.clone());

    let child_writer_handle = tokio::spawn(async move {
        let mut child_stdin = child_stdin;
        while let Some(command) = child_input_rx.recv().await {
            match command {
                ChildInputCommand::Write(bytes) => {
                    child_stdin.write_all(&bytes).await?;
                    child_stdin.flush().await?;
                }
                ChildInputCommand::Close => {
                    child_stdin.shutdown().await?;
                    break;
                }
            }
        }
        debug!("child stdin writer finished");
        Ok::<(), io::Error>(())
    });

    // Forward wrapper stdin → child stdin
    let stdin_attached = Arc::clone(&attached);
    let stdin_relay_tx = relay_tx.clone();
    let stdin_child_input_tx = child_input_tx.clone();
    let stdin_handle = tokio::spawn(async move {
        let mut reader = BufReader::new(wrapper_stdin);
        let mut buf = Vec::with_capacity(4096);

        loop {
            buf.clear();
            let n = reader.read_until(b'\n', &mut buf).await?;
            if n == 0 {
                let _ = stdin_child_input_tx.send(ChildInputCommand::Close);
                break;
            }

            if stdin_attached.load(Ordering::SeqCst) && is_local_turn_start(&buf) {
                stdin_attached.store(false, Ordering::SeqCst);
                if let Some(relay_tx) = stdin_relay_tx.as_ref() {
                    let _ = relay_tx.send(OutboundRelayMessage::AutoDetach {
                        reason: "local-input",
                    });
                }
            }

            stdin_child_input_tx
                .send(ChildInputCommand::Write(buf.clone()))
                .map_err(|_| io::Error::new(io::ErrorKind::BrokenPipe, "child stdin channel closed"))?;
        }

        debug!("stdin forwarding finished");
        Ok::<(), io::Error>(())
    });

    // Forward child stdout → wrapper stdout
    let stdout_handle = tokio::spawn(async move {
        let mut classifier = MessageClassifier::default();
        let mut child_stdout = BufReader::new(child_stdout);
        let mut wrapper_stdout = wrapper_stdout;
        let attached = attached;
        let relay_tx = relay_tx;
        let result =
            forward_lines_with_observer(&mut child_stdout, &mut wrapper_stdout, |line| {
                let classified = classifier.classify(line);
                debug!(
                    classification = ?classified.classification,
                    method = ?classified.method,
                    thread_id = ?classified.thread_id,
                    turn_id = ?classified.turn_id,
                    "classified codex stdout line"
                );

                if attached.load(Ordering::SeqCst) {
                    if let Some(relay_message) = build_relay_message(&classified, line) {
                        if let Some(relay_tx) = relay_tx.as_ref() {
                            let _ = relay_tx.send(relay_message);
                        }
                    }
                }
            })
            .await;
        if let Err(ref e) = result {
            error!(error = %e, "stdout forwarding error");
        }
        debug!("stdout forwarding finished");
        result
    });

    // Forward child stderr → wrapper stderr
    let stderr_handle = tokio::spawn(async move {
        let result = forward_bytes(child_stderr, wrapper_stderr).await;
        if let Err(ref e) = result {
            error!(error = %e, "stderr forwarding error");
        }
        debug!("stderr forwarding finished");
        result
    });

    // Set up signal forwarding (Unix only)
    #[cfg(unix)]
    let signal_handle = {
        let pid = child_pid;
        tokio::spawn(async move {
            if let Some(pid) = pid {
                if let Err(e) = forward_signals(pid).await {
                    error!(error = %e, "signal forwarding error");
                }
            }
        })
    };

    // Wait for child to exit
    let status = child.wait().await.context("Failed to wait for child")?;

    info!(status = ?status, "Child process exited");

    // Stop tasks that can still accept new child input, then wait for the
    // existing stdout/stderr forwarding tasks to drain until EOF.
    stdin_handle.abort();
    let _ = stdin_handle.await;

    if let Some((relay_tx, relay_handle)) = relay_runtime {
        drop(relay_tx);
        relay_handle.abort();
        let _ = relay_handle.await;
    }
    drop(child_input_tx);

    let (_child_writer_result, _stdout_result, _stderr_result) =
        tokio::join!(child_writer_handle, stdout_handle, stderr_handle);

    #[cfg(unix)]
    {
        signal_handle.abort();
        let _ = signal_handle.await;
    }

    // Determine exit code
    let exit_code = get_exit_code(&status);
    debug!(exit_code, "Wrapper exiting");

    Ok(exit_code)
}

/// Extract exit code from process status.
/// On Unix, if the process was killed by a signal, returns 128 + signal number.
fn get_exit_code(status: &std::process::ExitStatus) -> i32 {
    status.code().unwrap_or_else(|| {
        #[cfg(unix)]
        {
            use std::os::unix::process::ExitStatusExt;
            status.signal().map(|s| 128 + s).unwrap_or(1)
        }
        #[cfg(not(unix))]
        {
            1
        }
    })
}

/// Forward SIGINT and SIGTERM to the child process.
#[cfg(unix)]
async fn forward_signals(child_pid: u32) -> Result<()> {
    use tokio::signal::unix::{signal, SignalKind};

    let mut sigint = signal(SignalKind::interrupt()).context("Failed to register SIGINT handler")?;
    let mut sigterm =
        signal(SignalKind::terminate()).context("Failed to register SIGTERM handler")?;

    loop {
        tokio::select! {
            _ = sigint.recv() => {
                info!("Received SIGINT, forwarding to child");
                send_signal(child_pid, libc::SIGINT);
            }
            _ = sigterm.recv() => {
                info!("Received SIGTERM, forwarding to child");
                send_signal(child_pid, libc::SIGTERM);
                break;
            }
        }
    }

    Ok(())
}

/// Send a signal to a process by PID.
#[cfg(unix)]
fn send_signal(pid: u32, signal: i32) {
    unsafe {
        libc::kill(pid as i32, signal);
    }
}

fn build_relay_message(
    classified: &crate::classifier::ClassifiedMessage,
    line: &[u8],
) -> Option<OutboundRelayMessage> {
    let classification = match classified.classification {
        MessageClassification::AgentMessage => "agentMessage",
        MessageClassification::ServerRequest => "serverRequest",
        _ => return None,
    };

    Some(OutboundRelayMessage::Classified {
        classification,
        method: classified.method.clone(),
        thread_id: classified.thread_id.clone(),
        turn_id: classified.turn_id.clone(),
        raw: String::from_utf8_lossy(line).to_string(),
        payload: serde_json::from_slice::<Value>(line).ok(),
    })
}

fn current_workspace_path() -> String {
    std::env::current_dir()
        .unwrap_or_else(|_| std::path::PathBuf::from("."))
        .to_string_lossy()
        .to_string()
}

fn generate_session_id() -> String {
    let timestamp = SystemTime::now()
        .duration_since(UNIX_EPOCH)
        .unwrap_or_default()
        .as_millis();
    format!("wrapper-{}-{timestamp}", std::process::id())
}

fn is_local_turn_start(line: &[u8]) -> bool {
    serde_json::from_slice::<Value>(line)
        .ok()
        .and_then(|value| value.get("method").and_then(Value::as_str).map(str::to_owned))
        .is_some_and(|method| method == "turn/start")
}

#[cfg(test)]
mod tests {
    use super::*;
    use crate::classifier::MessageClassification;
    use tokio::io::{duplex, AsyncReadExt};

    #[tokio::test]
    async fn test_forward_single_line() {
        let (mut input_writer, input_reader) = duplex(4096);
        let (output_writer, mut output_reader) = duplex(4096);

        let handle = tokio::spawn(forward_lines(BufReader::new(input_reader), output_writer));

        input_writer
            .write_all(b"{\"msg\":\"hello\"}\n")
            .await
            .unwrap();
        drop(input_writer); // EOF

        let mut buf = Vec::new();
        output_reader.read_to_end(&mut buf).await.unwrap();
        assert_eq!(buf, b"{\"msg\":\"hello\"}\n");

        handle.await.unwrap().unwrap();
    }

    #[tokio::test]
    async fn test_forward_multiple_lines_preserves_order() {
        let (mut input_writer, input_reader) = duplex(4096);
        let (output_writer, mut output_reader) = duplex(4096);

        let handle = tokio::spawn(forward_lines(BufReader::new(input_reader), output_writer));

        let messages: Vec<String> = (0..100)
            .map(|i| format!("{{\"seq\":{}}}\n", i))
            .collect();

        for msg in &messages {
            input_writer.write_all(msg.as_bytes()).await.unwrap();
        }
        drop(input_writer);

        let mut buf = Vec::new();
        output_reader.read_to_end(&mut buf).await.unwrap();

        let expected: String = messages.concat();
        assert_eq!(buf, expected.as_bytes());

        handle.await.unwrap().unwrap();
    }

    #[tokio::test]
    async fn test_forward_preserves_bytes_exactly() {
        let (mut input_writer, input_reader) = duplex(4096);
        let (output_writer, mut output_reader) = duplex(4096);

        let handle = tokio::spawn(forward_lines(BufReader::new(input_reader), output_writer));

        // Include special characters, unicode, extra whitespace
        let line = b"{\"text\":\"hello \\\"world\\\" \\u00e9\\t\\r\"}\n";
        input_writer.write_all(line).await.unwrap();
        drop(input_writer);

        let mut buf = Vec::new();
        output_reader.read_to_end(&mut buf).await.unwrap();
        assert_eq!(buf, line);

        handle.await.unwrap().unwrap();
    }

    #[tokio::test]
    async fn test_forward_empty_lines() {
        let (mut input_writer, input_reader) = duplex(4096);
        let (output_writer, mut output_reader) = duplex(4096);

        let handle = tokio::spawn(forward_lines(BufReader::new(input_reader), output_writer));

        input_writer.write_all(b"\n\n\n").await.unwrap();
        drop(input_writer);

        let mut buf = Vec::new();
        output_reader.read_to_end(&mut buf).await.unwrap();
        assert_eq!(buf, b"\n\n\n");

        handle.await.unwrap().unwrap();
    }

    #[tokio::test]
    async fn test_forward_eof_returns_ok() {
        let (_input_writer, input_reader) = duplex(4096);
        let (output_writer, _output_reader) = duplex(4096);

        // Drop the writer immediately → EOF
        drop(_input_writer);

        let result = forward_lines(BufReader::new(input_reader), output_writer).await;
        assert!(result.is_ok());
    }

    #[tokio::test]
    async fn test_forward_large_message() {
        let (mut input_writer, input_reader) = duplex(65536);
        let (output_writer, mut output_reader) = duplex(65536);

        let handle = tokio::spawn(forward_lines(BufReader::new(input_reader), output_writer));

        // Create a large JSONL message (~50KB)
        let large_value = "x".repeat(50_000);
        let line = format!("{{\"data\":\"{}\"}}\n", large_value);
        input_writer.write_all(line.as_bytes()).await.unwrap();
        drop(input_writer);

        let mut buf = Vec::new();
        output_reader.read_to_end(&mut buf).await.unwrap();
        assert_eq!(buf, line.as_bytes());

        handle.await.unwrap().unwrap();
    }

    #[tokio::test]
    async fn test_forward_bytes_passthrough() {
        let (mut input_writer, input_reader) = duplex(4096);
        let (output_writer, mut output_reader) = duplex(4096);

        let handle = tokio::spawn(forward_bytes(input_reader, output_writer));

        let data = b"some stderr output\nwith multiple lines\n";
        input_writer.write_all(data).await.unwrap();
        drop(input_writer);

        let mut buf = Vec::new();
        output_reader.read_to_end(&mut buf).await.unwrap();
        assert_eq!(buf, data);

        let bytes_copied = handle.await.unwrap().unwrap();
        assert_eq!(bytes_copied, data.len() as u64);
    }

    #[tokio::test]
    async fn test_forward_line_without_trailing_newline() {
        // If the last line doesn't end with \n, it should still be forwarded
        let (mut input_writer, input_reader) = duplex(4096);
        let (output_writer, mut output_reader) = duplex(4096);

        let handle = tokio::spawn(forward_lines(BufReader::new(input_reader), output_writer));

        input_writer
            .write_all(b"{\"msg\":1}\n{\"msg\":2}")
            .await
            .unwrap();
        drop(input_writer);

        let mut buf = Vec::new();
        output_reader.read_to_end(&mut buf).await.unwrap();
        assert_eq!(buf, b"{\"msg\":1}\n{\"msg\":2}");

        handle.await.unwrap().unwrap();
    }

    #[tokio::test]
    async fn test_forward_lines_with_classifier_observer_preserves_bytes_and_tracks_context() {
        let (mut input_writer, input_reader) = duplex(4096);
        let (output_writer, mut output_reader) = duplex(4096);

        let input = concat!(
            "{\"method\":\"thread/started\",\"params\":{\"threadId\":\"thread-1\"}}\n",
            "{\"method\":\"turn/started\",\"params\":{\"turnId\":\"turn-1\",\"threadId\":\"thread-1\"}}\n",
            "{malformed json}\n"
        );

        let mut reader = BufReader::new(input_reader);
        let mut writer = output_writer;
        let mut classifier = MessageClassifier::default();
        let mut observed = Vec::new();

        let handle = tokio::spawn(async move {
            forward_lines_with_observer(&mut reader, &mut writer, |line| {
                observed.push(classifier.classify(line));
            })
            .await
            .map(|_| observed)
        });

        input_writer.write_all(input.as_bytes()).await.unwrap();
        drop(input_writer);

        let mut forwarded = Vec::new();
        output_reader.read_to_end(&mut forwarded).await.unwrap();
        assert_eq!(forwarded, input.as_bytes());

        let observed = handle.await.unwrap().unwrap();
        assert_eq!(observed.len(), 3);
        assert_eq!(
            observed[0].classification,
            MessageClassification::ThreadLifecycle
        );
        assert_eq!(
            observed[1].classification,
            MessageClassification::TurnLifecycle
        );
        assert_eq!(observed[2].classification, MessageClassification::Unknown);
        assert_eq!(observed[2].thread_id.as_deref(), Some("thread-1"));
        assert_eq!(observed[2].turn_id.as_deref(), Some("turn-1"));
    }
}
