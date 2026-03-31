use std::path::PathBuf;
use std::time::Instant;

use tokio::io::{AsyncBufReadExt, AsyncReadExt, AsyncWriteExt, BufReader};
use tokio::process::Command;

/// Helper to get the absolute path to a test fixture.
fn fixture_path(name: &str) -> String {
    let mut path = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    path.push("tests");
    path.push("fixtures");
    path.push(name);
    path.to_string_lossy().to_string()
}

// ---- Helper module to access proxy internals ----
// We test via the binary or re-export. For unit-level forwarding tests,
// see the #[cfg(test)] module in proxy.rs. Here we test the full proxy
// by spawning the wrapper binary or using run_proxy_with_io.

/// Spawn the wrapper binary as a child process for integration testing.
async fn spawn_wrapper(codex_binary: &str) -> tokio::process::Child {
    let wrapper_bin = env!("CARGO_BIN_EXE_codex-relay-wrapper");
    Command::new(wrapper_bin)
        .arg("--codex-binary")
        .arg(codex_binary)
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .kill_on_drop(true)
        .spawn()
        .expect("Failed to spawn wrapper binary")
}

/// Spawn the wrapper binary with extra env vars.
async fn spawn_wrapper_with_env(
    codex_binary: &str,
    env_vars: Vec<(&str, &str)>,
) -> tokio::process::Child {
    let wrapper_bin = env!("CARGO_BIN_EXE_codex-relay-wrapper");
    let mut cmd = Command::new(wrapper_bin);
    cmd.arg("--codex-binary")
        .arg(codex_binary)
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .kill_on_drop(true);
    for (k, v) in env_vars {
        cmd.env(k, v);
    }
    cmd.spawn().expect("Failed to spawn wrapper binary")
}

// ============================================================
// VAL-WRAP-001: Bidirectional stdio message passthrough
// ============================================================

#[tokio::test]
async fn test_stdin_to_child_passthrough() {
    let mock = fixture_path("mock_codex_echo.sh");
    let mut wrapper = spawn_wrapper(&mock).await;

    let mut stdin = wrapper.stdin.take().unwrap();
    let stdout = wrapper.stdout.take().unwrap();

    let msg = b"{\"type\":\"turn/start\",\"prompt\":\"hello\"}\n";
    stdin.write_all(msg).await.unwrap();
    drop(stdin); // Close stdin → EOF

    let mut reader = BufReader::new(stdout);
    let mut line = String::new();
    reader.read_line(&mut line).await.unwrap();

    assert_eq!(line.as_bytes(), msg, "Message must pass through byte-for-byte");

    let status = wrapper.wait().await.unwrap();
    assert!(status.success());
}

#[tokio::test]
async fn test_multiple_messages_passthrough() {
    let mock = fixture_path("mock_codex_echo.sh");
    let mut wrapper = spawn_wrapper(&mock).await;

    let mut stdin = wrapper.stdin.take().unwrap();
    let stdout = wrapper.stdout.take().unwrap();

    let messages: Vec<String> = (0..10)
        .map(|i| format!("{{\"seq\":{},\"data\":\"test message {}\"}}\n", i, i))
        .collect();

    for msg in &messages {
        stdin.write_all(msg.as_bytes()).await.unwrap();
    }
    drop(stdin);

    let mut reader = BufReader::new(stdout);
    let mut received = Vec::new();
    let mut line = String::new();
    loop {
        line.clear();
        let n = reader.read_line(&mut line).await.unwrap();
        if n == 0 {
            break;
        }
        received.push(line.clone());
    }

    assert_eq!(received.len(), messages.len(), "All messages must be received");
    for (sent, recv) in messages.iter().zip(received.iter()) {
        assert_eq!(sent, recv, "Messages must be byte-for-byte identical");
    }

    let status = wrapper.wait().await.unwrap();
    assert!(status.success());
}

// ============================================================
// VAL-WRAP-002: Message ordering preserved
// ============================================================

#[tokio::test]
async fn test_message_ordering_preserved() {
    let mock = fixture_path("mock_codex_echo.sh");
    let mut wrapper = spawn_wrapper(&mock).await;

    let mut stdin = wrapper.stdin.take().unwrap();
    let stdout = wrapper.stdout.take().unwrap();

    // Send 50 messages rapidly
    let count = 50;
    for i in 0..count {
        let msg = format!("{{\"seq\":{}}}\n", i);
        stdin.write_all(msg.as_bytes()).await.unwrap();
    }
    drop(stdin);

    let mut reader = BufReader::new(stdout);
    let mut received_seqs = Vec::new();
    let mut line = String::new();
    loop {
        line.clear();
        let n = reader.read_line(&mut line).await.unwrap();
        if n == 0 {
            break;
        }
        // Parse the seq number
        if let Ok(v) = serde_json::from_str::<serde_json::Value>(&line) {
            if let Some(seq) = v.get("seq").and_then(|s| s.as_i64()) {
                received_seqs.push(seq);
            }
        }
    }

    assert_eq!(received_seqs.len(), count, "All messages received");
    for i in 0..count {
        assert_eq!(
            received_seqs[i], i as i64,
            "Message ordering must be strictly monotonic"
        );
    }

    let status = wrapper.wait().await.unwrap();
    assert!(status.success());
}

// ============================================================
// VAL-WRAP-003: Child process spawned with correct binary path
// ============================================================

#[tokio::test]
async fn test_child_spawned_with_correct_binary() {
    let sentinel = format!("/tmp/mock_codex_sentinel_{}", std::process::id());
    let mock = fixture_path("mock_codex_echo.sh");

    let mut wrapper = spawn_wrapper_with_env(&mock, vec![("MOCK_SENTINEL_FILE", &sentinel)]).await;

    // Close stdin to let the mock exit
    drop(wrapper.stdin.take());

    let status = wrapper.wait().await.unwrap();
    assert!(status.success());

    // Verify sentinel file was created
    assert!(
        std::path::Path::new(&sentinel).exists(),
        "Sentinel file must exist, proving the correct binary was spawned"
    );

    // Cleanup
    let _ = std::fs::remove_file(&sentinel);
}

// ============================================================
// VAL-WRAP-004: Exit code propagation
// ============================================================

#[tokio::test]
async fn test_exit_code_zero() {
    let mock = fixture_path("mock_codex_exit.sh");
    let mut wrapper = spawn_wrapper_with_env(&mock, vec![("MOCK_EXIT_CODE", "0")]).await;
    drop(wrapper.stdin.take());

    let status = wrapper.wait().await.unwrap();
    assert_eq!(status.code(), Some(0));
}

#[tokio::test]
async fn test_exit_code_nonzero() {
    let mock = fixture_path("mock_codex_exit.sh");
    let mut wrapper = spawn_wrapper_with_env(&mock, vec![("MOCK_EXIT_CODE", "42")]).await;
    drop(wrapper.stdin.take());

    let status = wrapper.wait().await.unwrap();
    assert_eq!(
        status.code(),
        Some(42),
        "Wrapper must exit with the same code as the child"
    );
}

#[tokio::test]
async fn test_exit_code_one() {
    let mock = fixture_path("mock_codex_exit.sh");
    let mut wrapper = spawn_wrapper_with_env(&mock, vec![("MOCK_EXIT_CODE", "1")]).await;
    drop(wrapper.stdin.take());

    let status = wrapper.wait().await.unwrap();
    assert_eq!(status.code(), Some(1));
}

// ============================================================
// VAL-WRAP-005: Signal forwarding (SIGINT, SIGTERM)
// ============================================================

#[cfg(unix)]
#[tokio::test]
async fn test_sigterm_forwarded_to_child() {
    let signal_log = format!("/tmp/mock_codex_signal_{}", std::process::id());
    let mock = fixture_path("mock_codex_signal.sh");

    let mut wrapper =
        spawn_wrapper_with_env(&mock, vec![("MOCK_SIGNAL_LOG", &signal_log)]).await;

    let stdout = wrapper.stdout.take().unwrap();
    let mut reader = BufReader::new(stdout);

    // Wait for the mock to be ready
    let mut ready_line = String::new();
    reader.read_line(&mut ready_line).await.unwrap();
    assert!(ready_line.contains("READY"), "Mock should signal readiness");

    // Send SIGTERM to the wrapper
    let wrapper_pid = wrapper.id().unwrap();
    unsafe {
        libc::kill(wrapper_pid as i32, libc::SIGTERM);
    }

    // Wait for wrapper to exit
    let status = wrapper.wait().await.unwrap();
    // The child exits with 143 (128 + 15 for SIGTERM)
    // The wrapper should propagate this
    let code = status.code().unwrap_or(-1);
    assert!(
        code == 143 || code == 0,
        "Wrapper should exit with child's signal-based exit code, got {}",
        code
    );

    // Verify signal was logged
    tokio::time::sleep(std::time::Duration::from_millis(100)).await;
    let log_content = std::fs::read_to_string(&signal_log).unwrap_or_default();
    assert!(
        log_content.contains("SIGTERM"),
        "Child must have received SIGTERM, log: {}",
        log_content
    );

    // Cleanup
    let _ = std::fs::remove_file(&signal_log);
}

#[cfg(unix)]
#[tokio::test]
async fn test_sigint_forwarded_to_child() {
    let signal_log = format!("/tmp/mock_codex_sigint_{}", std::process::id());
    let mock = fixture_path("mock_codex_signal.sh");

    let mut wrapper =
        spawn_wrapper_with_env(&mock, vec![("MOCK_SIGNAL_LOG", &signal_log)]).await;

    let stdout = wrapper.stdout.take().unwrap();
    let mut reader = BufReader::new(stdout);

    // Wait for the mock to be ready
    let mut ready_line = String::new();
    reader.read_line(&mut ready_line).await.unwrap();
    assert!(ready_line.contains("READY"));

    // Send SIGINT to the wrapper
    let wrapper_pid = wrapper.id().unwrap();
    unsafe {
        libc::kill(wrapper_pid as i32, libc::SIGINT);
    }

    // Wait for wrapper to exit
    let status = wrapper.wait().await.unwrap();
    let code = status.code().unwrap_or(-1);
    // Child exits with 130 (128 + 2 for SIGINT)
    assert!(
        code == 130 || code == 0,
        "Wrapper should exit with child's signal-based exit code, got {}",
        code
    );

    // Verify signal was logged
    tokio::time::sleep(std::time::Duration::from_millis(100)).await;
    let log_content = std::fs::read_to_string(&signal_log).unwrap_or_default();
    assert!(
        log_content.contains("SIGINT"),
        "Child must have received SIGINT, log: {}",
        log_content
    );

    // Cleanup
    let _ = std::fs::remove_file(&signal_log);
}

// ============================================================
// VAL-WRAP-006: Wrapper exits within 5s of child exit
// ============================================================

#[tokio::test]
async fn test_wrapper_exits_within_5s_of_child() {
    let mock = fixture_path("mock_codex_exit.sh");
    let mut wrapper = spawn_wrapper_with_env(&mock, vec![("MOCK_EXIT_CODE", "0")]).await;
    drop(wrapper.stdin.take());

    let start = Instant::now();
    let status = wrapper.wait().await.unwrap();
    let elapsed = start.elapsed();

    assert!(status.success());
    assert!(
        elapsed.as_secs() < 5,
        "Wrapper must exit within 5s of child exit, took {:?}",
        elapsed
    );
}

// ============================================================
// VAL-WRAP-015: Invalid binary path → non-zero exit
// ============================================================

#[tokio::test]
async fn test_invalid_binary_path_exits_nonzero() {
    let mut wrapper = spawn_wrapper("/nonexistent/binary/path").await;
    drop(wrapper.stdin.take());

    let status = wrapper.wait().await.unwrap();
    assert!(
        !status.success(),
        "Wrapper must exit with non-zero code for invalid binary path"
    );

    // Check stderr for error message
    let mut stderr_buf = Vec::new();
    if let Some(mut stderr) = wrapper.stderr.take() {
        let _ = tokio::time::timeout(
            std::time::Duration::from_secs(1),
            stderr.read_to_end(&mut stderr_buf),
        )
        .await;
    }
    let stderr_str = String::from_utf8_lossy(&stderr_buf);
    assert!(
        stderr_str.contains("nonexistent") || stderr_str.contains("Failed") || stderr_str.contains("No such file"),
        "Error message should mention the invalid path, got: {}",
        stderr_str
    );
}

// ============================================================
// VAL-WRAP-016: Stdin EOF propagated to child stdin
// ============================================================

#[tokio::test]
async fn test_stdin_eof_propagated_to_child() {
    let eof_log = format!("/tmp/mock_codex_eof_{}", std::process::id());
    let mock = fixture_path("mock_codex_stdin_eof.sh");

    let mut wrapper = spawn_wrapper_with_env(&mock, vec![("MOCK_EOF_LOG", &eof_log)]).await;

    let mut stdin = wrapper.stdin.take().unwrap();

    // Write a message then close stdin
    stdin
        .write_all(b"{\"msg\":\"last\"}\n")
        .await
        .unwrap();
    drop(stdin); // Close stdin → EOF

    let status = wrapper.wait().await.unwrap();
    assert!(status.success());

    // Verify EOF was detected by the child
    tokio::time::sleep(std::time::Duration::from_millis(100)).await;
    let log_content = std::fs::read_to_string(&eof_log).unwrap_or_default();
    assert!(
        log_content.contains("EOF_DETECTED"),
        "Child must detect EOF on stdin, log: {}",
        log_content
    );

    // Cleanup
    let _ = std::fs::remove_file(&eof_log);
}

// ============================================================
// Additional: Stderr passthrough
// ============================================================

#[tokio::test]
async fn test_stderr_passthrough() {
    let mock = fixture_path("mock_codex_echo.sh");
    let stderr_msg = "test stderr output";

    let mut wrapper =
        spawn_wrapper_with_env(&mock, vec![("MOCK_STDERR_MSG", stderr_msg)]).await;

    drop(wrapper.stdin.take()); // Close stdin

    let status = wrapper.wait().await.unwrap();
    assert!(status.success());

    // Read stderr
    let mut stderr_buf = Vec::new();
    if let Some(mut stderr) = wrapper.stderr.take() {
        let _ = tokio::time::timeout(
            std::time::Duration::from_secs(2),
            stderr.read_to_end(&mut stderr_buf),
        )
        .await;
    }
    let stderr_str = String::from_utf8_lossy(&stderr_buf);
    assert!(
        stderr_str.contains(stderr_msg),
        "Stderr must be passed through, got: {}",
        stderr_str
    );
}
