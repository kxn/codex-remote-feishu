use std::io;
use std::process::Stdio;

use anyhow::{Context, Result};
use tokio::io::{AsyncBufRead, AsyncBufReadExt, AsyncRead, AsyncWrite, AsyncWriteExt, BufReader};
use tokio::process::Command;
use tracing::{debug, error, info};

/// Forward lines from an async reader to an async writer, byte-for-byte.
/// Each line is delimited by `\n`. The newline is included in the forwarded bytes.
/// Returns when the reader reaches EOF.
pub async fn forward_lines(
    mut reader: impl AsyncBufRead + Unpin,
    mut writer: impl AsyncWrite + Unpin,
) -> io::Result<()> {
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
    pub args: Vec<String>,
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
    info!(binary = %config.binary_path, "Spawning child process");

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

    // Forward wrapper stdin → child stdin
    let stdin_handle = tokio::spawn(async move {
        let result = forward_lines(BufReader::new(wrapper_stdin), child_stdin).await;
        if let Err(ref e) = result {
            // BrokenPipe is expected when child exits before stdin is closed
            if e.kind() != io::ErrorKind::BrokenPipe {
                error!(error = %e, "stdin forwarding error");
            }
        }
        debug!("stdin forwarding finished");
        result
    });

    // Forward child stdout → wrapper stdout
    let stdout_handle = tokio::spawn(async move {
        let result = forward_lines(BufReader::new(child_stdout), wrapper_stdout).await;
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

    // Abort forwarding tasks (they'll stop naturally when pipes close,
    // but we abort to ensure cleanup within 5s)
    stdin_handle.abort();
    // Wait briefly for stdout/stderr to drain
    let _ = tokio::time::timeout(std::time::Duration::from_secs(3), async {
        let _ = stdout_handle.await;
        let _ = stderr_handle.await;
    })
    .await;

    #[cfg(unix)]
    signal_handle.abort();

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

#[cfg(test)]
mod tests {
    use super::*;
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
}
