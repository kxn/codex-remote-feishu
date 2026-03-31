use std::path::{Path, PathBuf};
use std::time::Duration;

use futures::{SinkExt, StreamExt};
use serde_json::{json, Value};
use tokio::io::{AsyncBufReadExt, AsyncReadExt, AsyncWriteExt, BufReader};
use tokio::net::TcpListener;
use tokio::process::{Child, ChildStdout, Command};
use tokio::sync::{mpsc, Mutex};
use tokio::time::timeout;
use tokio_tungstenite::{accept_async, tungstenite::Message};

fn fixture_path(name: &str) -> String {
    let mut path = PathBuf::from(env!("CARGO_MANIFEST_DIR"));
    path.push("tests");
    path.push("fixtures");
    path.push(name);
    path.to_string_lossy().to_string()
}

fn wrapper_log_path(pid: u32) -> PathBuf {
    std::env::temp_dir().join(format!("codex-relay-wrapper-{pid}.log"))
}

async fn read_wrapper_log(pid: u32) -> String {
    let log_path = wrapper_log_path(pid);
    let deadline = std::time::Instant::now() + Duration::from_secs(2);

    loop {
        match tokio::fs::read_to_string(&log_path).await {
            Ok(contents) => return contents,
            Err(error)
                if error.kind() == std::io::ErrorKind::NotFound
                    && std::time::Instant::now() < deadline =>
            {
                tokio::time::sleep(Duration::from_millis(25)).await;
            }
            Err(_) => return String::new(),
        }
    }
}

fn remove_wrapper_log(pid: u32) {
    let _ = std::fs::remove_file(wrapper_log_path(pid));
}

async fn spawn_wrapper_process(
    args: &[&str],
    env_vars: Vec<(&str, &str)>,
    current_dir: Option<&Path>,
) -> Child {
    let wrapper_bin = env!("CARGO_BIN_EXE_codex-relay-wrapper");
    let mut cmd = Command::new(wrapper_bin);
    cmd.args(args)
        .stdin(std::process::Stdio::piped())
        .stdout(std::process::Stdio::piped())
        .stderr(std::process::Stdio::piped())
        .kill_on_drop(true);

    if let Some(dir) = current_dir {
        cmd.current_dir(dir);
    }

    for (key, value) in env_vars {
        cmd.env(key, value);
    }

    cmd.spawn().expect("failed to spawn wrapper")
}

async fn read_stdout_line(reader: &mut BufReader<ChildStdout>) -> String {
    let mut line = String::new();
    let bytes = timeout(Duration::from_secs(2), reader.read_line(&mut line))
        .await
        .expect("timed out waiting for stdout line")
        .expect("failed to read stdout");
    assert!(bytes > 0, "expected stdout line, got EOF");
    line
}

async fn expect_no_stdout_line(reader: &mut BufReader<ChildStdout>, wait: Duration) {
    let mut line = String::new();
    match timeout(wait, reader.read_line(&mut line)).await {
        Err(_) => {}
        Ok(Ok(0)) => {}
        Ok(Ok(_)) => panic!("unexpected stdout line: {line}"),
        Ok(Err(error)) => panic!("failed reading stdout: {error}"),
    }
}

async fn read_stderr(child: &mut Child) -> String {
    let mut stderr = Vec::new();
    if let Some(mut handle) = child.stderr.take() {
        let _ = timeout(Duration::from_secs(2), handle.read_to_end(&mut stderr)).await;
    }
    String::from_utf8_lossy(&stderr).into_owned()
}

enum ServerControl {
    Send(Value),
    CloseCurrent,
}

struct MockRelayServer {
    url: String,
    incoming_rx: Mutex<mpsc::UnboundedReceiver<Value>>,
    control_tx: mpsc::UnboundedSender<ServerControl>,
}

impl MockRelayServer {
    async fn start() -> Self {
        let listener = TcpListener::bind("127.0.0.1:0")
            .await
            .expect("failed to bind mock relay server");
        let addr = listener.local_addr().expect("failed to get listener address");

        let (incoming_tx, incoming_rx) = mpsc::unbounded_channel();
        let (control_tx, mut control_rx) = mpsc::unbounded_channel();

        tokio::spawn(async move {
            loop {
                let (stream, _) = match listener.accept().await {
                    Ok(value) => value,
                    Err(_) => break,
                };

                let websocket = match accept_async(stream).await {
                    Ok(value) => value,
                    Err(_) => continue,
                };

                let (mut write, mut read) = websocket.split();

                loop {
                    tokio::select! {
                        command = control_rx.recv() => {
                            match command {
                                Some(ServerControl::Send(value)) => {
                                    if write.send(Message::Text(value.to_string().into())).await.is_err() {
                                        break;
                                    }
                                }
                                Some(ServerControl::CloseCurrent) => {
                                    let _ = write.send(Message::Close(None)).await;
                                    break;
                                }
                                None => return,
                            }
                        }
                        message = read.next() => {
                            match message {
                                Some(Ok(Message::Text(text))) => {
                                    if let Ok(value) = serde_json::from_str::<Value>(&text) {
                                        let _ = incoming_tx.send(value);
                                    }
                                }
                                Some(Ok(Message::Binary(bytes))) => {
                                    if let Ok(text) = String::from_utf8(bytes.to_vec()) {
                                        if let Ok(value) = serde_json::from_str::<Value>(&text) {
                                            let _ = incoming_tx.send(value);
                                        }
                                    }
                                }
                                Some(Ok(Message::Close(_))) | None => break,
                                Some(Ok(_)) => {}
                                Some(Err(_)) => break,
                            }
                        }
                    }
                }
            }
        });

        Self {
            url: format!("ws://{addr}"),
            incoming_rx: Mutex::new(incoming_rx),
            control_tx,
        }
    }

    fn url(&self) -> &str {
        &self.url
    }

    async fn next_message(&self) -> Value {
        let mut incoming_rx = self.incoming_rx.lock().await;
        timeout(Duration::from_secs(3), incoming_rx.recv())
            .await
            .expect("timed out waiting for relay message")
            .expect("relay connection closed unexpectedly")
    }

    async fn expect_no_message(&self, wait: Duration) {
        let mut incoming_rx = self.incoming_rx.lock().await;
        match timeout(wait, incoming_rx.recv()).await {
            Err(_) => {}
            Ok(None) => {}
            Ok(Some(message)) => panic!("unexpected relay message: {message}"),
        }
    }

    fn send(&self, value: Value) {
        self.control_tx
            .send(ServerControl::Send(value))
            .expect("failed to send mock relay command");
    }

    fn close_current_connection(&self) {
        self.control_tx
            .send(ServerControl::CloseCurrent)
            .expect("failed to close current relay connection");
    }
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn registers_with_relay_on_startup() {
    let relay = MockRelayServer::start().await;
    let mock_codex = fixture_path("mock_codex_echo.sh");
    let base_dir = std::env::temp_dir().join(format!("wrapper-ws-register-{}", std::process::id()));
    let workspace_dir = base_dir.join("relay-workspace");
    std::fs::create_dir_all(&workspace_dir).expect("failed to create workspace directory");

    let mut wrapper = spawn_wrapper_process(
        &["--codex-binary", &mock_codex, "--relay-url", relay.url()],
        Vec::new(),
        Some(&workspace_dir),
    )
    .await;

    let register = relay.next_message().await;
    assert_eq!(register.get("type").and_then(Value::as_str), Some("register"));
    assert!(
        register
            .get("sessionId")
            .and_then(Value::as_str)
            .is_some_and(|value| !value.is_empty()),
        "register message should include a non-empty sessionId: {register}"
    );
    assert_eq!(
        register.get("displayName").and_then(Value::as_str),
        Some("relay-workspace")
    );
    assert_eq!(
        register.pointer("/metadata/version").and_then(Value::as_str),
        Some(env!("CARGO_PKG_VERSION"))
    );
    assert_eq!(
        register.pointer("/metadata/workspacePath").and_then(Value::as_str),
        Some(workspace_dir.to_string_lossy().as_ref())
    );
    assert!(
        register
            .pointer("/metadata/pid")
            .and_then(Value::as_u64)
            .is_some_and(|pid| pid > 0),
        "register message should include wrapper pid metadata: {register}"
    );

    drop(wrapper.stdin.take());
    let status = wrapper.wait().await.expect("failed to wait for wrapper");
    assert!(status.success(), "wrapper should exit successfully");

    let _ = std::fs::remove_dir_all(&base_dir);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn stdio_proxy_continues_when_relay_is_unreachable() {
    let reserved_port = TcpListener::bind("127.0.0.1:0")
        .await
        .expect("failed to reserve port");
    let addr = reserved_port
        .local_addr()
        .expect("failed to read reserved port");
    drop(reserved_port);

    let mock_codex = fixture_path("mock_codex_echo.sh");
    let mut wrapper = spawn_wrapper_process(
        &[
            "--codex-binary",
            &mock_codex,
            "--relay-url",
            &format!("ws://{addr}"),
        ],
        Vec::new(),
        None,
    )
    .await;
    let wrapper_pid = wrapper.id().unwrap();

    let mut stdin = wrapper.stdin.take().expect("missing wrapper stdin");
    let stdout = wrapper.stdout.take().expect("missing wrapper stdout");
    let mut stdout = BufReader::new(stdout);

    stdin
        .write_all(b"{\"method\":\"item/agentMessage/delta\",\"params\":{\"delta\":\"hello\"}}\n")
        .await
        .expect("failed to write to wrapper stdin");
    drop(stdin);

    let echoed = read_stdout_line(&mut stdout).await;
    assert_eq!(
        echoed,
        "{\"method\":\"item/agentMessage/delta\",\"params\":{\"delta\":\"hello\"}}\n"
    );

    let status = wrapper.wait().await.expect("failed to wait for wrapper");
    assert!(status.success(), "wrapper should continue in degraded mode");

    let stderr = read_stderr(&mut wrapper).await;
    assert!(
        stderr.is_empty(),
        "wrapper tracing should not contaminate stderr, got: {stderr}"
    );

    let log_output = read_wrapper_log(wrapper_pid).await;
    assert!(
        log_output.contains("relay")
            && (log_output.contains("connect") || log_output.contains("degraded mode")),
        "expected relay connectivity warning in wrapper log, got: {log_output}"
    );
    remove_wrapper_log(wrapper_pid);
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn forwards_only_agent_and_server_request_messages_when_attached() {
    let relay = MockRelayServer::start().await;
    let mock_codex = fixture_path("mock_codex_echo.sh");
    let mut wrapper = spawn_wrapper_process(
        &["--codex-binary", &mock_codex, "--relay-url", relay.url()],
        Vec::new(),
        None,
    )
    .await;

    let mut stdin = wrapper.stdin.take().expect("missing wrapper stdin");
    let stdout = wrapper.stdout.take().expect("missing wrapper stdout");
    let mut stdout = BufReader::new(stdout);

    let register = relay.next_message().await;
    let session_id = register
        .get("sessionId")
        .and_then(Value::as_str)
        .expect("register message missing sessionId")
        .to_string();

    stdin
        .write_all(
            b"{\"method\":\"item/agentMessage/delta\",\"params\":{\"delta\":\"before attach\"}}\n",
        )
        .await
        .expect("failed to write pre-attach agent message");
    let _ = read_stdout_line(&mut stdout).await;
    relay.expect_no_message(Duration::from_millis(250)).await;

    relay.send(json!({
        "type": "attach-status-changed",
        "attached": true,
        "userId": "user-1"
    }));
    tokio::time::sleep(Duration::from_millis(100)).await;

    stdin
        .write_all(b"{\"method\":\"thread/started\",\"params\":{\"threadId\":\"thread-1\"}}\n")
        .await
        .expect("failed to write thread started");
    let _ = read_stdout_line(&mut stdout).await;
    relay.expect_no_message(Duration::from_millis(250)).await;

    stdin
        .write_all(
            b"{\"method\":\"turn/started\",\"params\":{\"threadId\":\"thread-1\",\"turnId\":\"turn-1\"}}\n",
        )
        .await
        .expect("failed to write turn started");
    let _ = read_stdout_line(&mut stdout).await;
    relay.expect_no_message(Duration::from_millis(250)).await;

    stdin
        .write_all(
            b"{\"method\":\"item/agentMessage/delta\",\"params\":{\"delta\":\"forward me\"}}\n",
        )
        .await
        .expect("failed to write attached agent message");
    let _ = read_stdout_line(&mut stdout).await;

    let forwarded_agent = relay.next_message().await;
    assert_eq!(forwarded_agent.get("type").and_then(Value::as_str), Some("message"));
    assert_eq!(
        forwarded_agent.get("sessionId").and_then(Value::as_str),
        Some(session_id.as_str())
    );
    assert_eq!(
        forwarded_agent.get("classification").and_then(Value::as_str),
        Some("agentMessage")
    );
    assert_eq!(
        forwarded_agent.get("threadId").and_then(Value::as_str),
        Some("thread-1")
    );
    assert_eq!(
        forwarded_agent.get("turnId").and_then(Value::as_str),
        Some("turn-1")
    );

    stdin
        .write_all(
            b"{\"method\":\"item/started\",\"params\":{\"item\":{\"type\":\"commandExecution\",\"command\":\"ls\"}}}\n",
        )
        .await
        .expect("failed to write tool call message");
    let _ = read_stdout_line(&mut stdout).await;
    relay.expect_no_message(Duration::from_millis(250)).await;

    stdin
        .write_all(b"{\"method\":\"custom/unknown\",\"params\":{\"value\":1}}\n")
        .await
        .expect("failed to write unknown message");
    let _ = read_stdout_line(&mut stdout).await;
    relay.expect_no_message(Duration::from_millis(250)).await;

    stdin
        .write_all(b"{\"method\":\"serverRequest/approval\",\"params\":{\"id\":\"req-1\"}}\n")
        .await
        .expect("failed to write server request");
    let _ = read_stdout_line(&mut stdout).await;

    let forwarded_request = relay.next_message().await;
    assert_eq!(
        forwarded_request.get("classification").and_then(Value::as_str),
        Some("serverRequest")
    );
    assert_eq!(
        forwarded_request.get("sessionId").and_then(Value::as_str),
        Some(session_id.as_str())
    );

    drop(stdin);
    let status = wrapper.wait().await.expect("failed to wait for wrapper");
    assert!(status.success(), "wrapper should exit successfully");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn injects_server_commands_only_while_attached() {
    let relay = MockRelayServer::start().await;
    let mock_codex = fixture_path("mock_codex_echo.sh");
    let mut wrapper = spawn_wrapper_process(
        &["--codex-binary", &mock_codex, "--relay-url", relay.url()],
        Vec::new(),
        None,
    )
    .await;

    let mut stdin = wrapper.stdin.take().expect("missing wrapper stdin");
    let stdout = wrapper.stdout.take().expect("missing wrapper stdout");
    let mut stdout = BufReader::new(stdout);

    let _ = relay.next_message().await;
    relay.send(json!({
        "type": "attach-status-changed",
        "attached": true,
        "userId": "user-1"
    }));

    relay.send(json!({
        "type": "input",
        "content": "remote hello"
    }));
    let remote_prompt = read_stdout_line(&mut stdout).await;
    let remote_prompt_json: Value =
        serde_json::from_str(remote_prompt.trim()).expect("remote prompt should be valid JSON");
    assert_eq!(
        remote_prompt_json.get("method").and_then(Value::as_str),
        Some("turn/start")
    );
    assert_eq!(
        remote_prompt_json.pointer("/params/prompt").and_then(Value::as_str),
        Some("remote hello")
    );

    stdin
        .write_all(
            b"{\"method\":\"item/agentMessage/delta\",\"params\":{\"delta\":\"still attached\"}}\n",
        )
        .await
        .expect("failed to write agent message after remote prompt");
    let _ = read_stdout_line(&mut stdout).await;
    let forwarded_after_remote_prompt = relay.next_message().await;
    assert_eq!(
        forwarded_after_remote_prompt
            .get("classification")
            .and_then(Value::as_str),
        Some("agentMessage")
    );

    relay.send(json!({
        "type": "interrupt"
    }));
    let remote_interrupt = read_stdout_line(&mut stdout).await;
    let remote_interrupt_json: Value = serde_json::from_str(remote_interrupt.trim())
        .expect("remote interrupt should be valid JSON");
    assert_eq!(
        remote_interrupt_json.get("method").and_then(Value::as_str),
        Some("turn/interrupt")
    );

    relay.send(json!({
        "type": "attach-status-changed",
        "attached": false
    }));
    relay.send(json!({
        "type": "input",
        "content": "ignore me"
    }));
    expect_no_stdout_line(&mut stdout, Duration::from_millis(300)).await;

    drop(stdin);
    let status = wrapper.wait().await.expect("failed to wait for wrapper");
    assert!(status.success(), "wrapper should exit successfully");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn local_turn_start_auto_detaches_until_reattached() {
    let relay = MockRelayServer::start().await;
    let mock_codex = fixture_path("mock_codex_echo.sh");
    let mut wrapper = spawn_wrapper_process(
        &["--codex-binary", &mock_codex, "--relay-url", relay.url()],
        Vec::new(),
        None,
    )
    .await;

    let mut stdin = wrapper.stdin.take().expect("missing wrapper stdin");
    let stdout = wrapper.stdout.take().expect("missing wrapper stdout");
    let mut stdout = BufReader::new(stdout);

    let register = relay.next_message().await;
    let session_id = register
        .get("sessionId")
        .and_then(Value::as_str)
        .expect("register message missing sessionId")
        .to_string();

    relay.send(json!({
        "type": "attach-status-changed",
        "attached": true,
        "userId": "user-1"
    }));
    tokio::time::sleep(Duration::from_millis(100)).await;

    stdin
        .write_all(b"{\"method\":\"turn/start\",\"params\":{\"prompt\":\"local input\"}}\n")
        .await
        .expect("failed to write local turn/start");
    let _ = read_stdout_line(&mut stdout).await;

    let detach_notice = relay.next_message().await;
    assert_eq!(
        detach_notice.get("type").and_then(Value::as_str),
        Some("auto-detach")
    );
    assert_eq!(
        detach_notice.get("sessionId").and_then(Value::as_str),
        Some(session_id.as_str())
    );
    assert_eq!(
        detach_notice.get("reason").and_then(Value::as_str),
        Some("local-input")
    );

    stdin
        .write_all(
            b"{\"method\":\"item/agentMessage/delta\",\"params\":{\"delta\":\"stay local\"}}\n",
        )
        .await
        .expect("failed to write post-detach agent message");
    let _ = read_stdout_line(&mut stdout).await;
    relay.expect_no_message(Duration::from_millis(250)).await;

    relay.send(json!({
        "type": "input",
        "content": "should be ignored while detached"
    }));
    expect_no_stdout_line(&mut stdout, Duration::from_millis(300)).await;

    relay.send(json!({
        "type": "attach-status-changed",
        "attached": true,
        "userId": "user-2"
    }));
    tokio::time::sleep(Duration::from_millis(100)).await;

    stdin
        .write_all(
            b"{\"method\":\"item/agentMessage/delta\",\"params\":{\"delta\":\"forward after reattach\"}}\n",
        )
        .await
        .expect("failed to write reattached agent message");
    let _ = read_stdout_line(&mut stdout).await;
    let forwarded_after_reattach = relay.next_message().await;
    assert_eq!(
        forwarded_after_reattach
            .get("classification")
            .and_then(Value::as_str),
        Some("agentMessage")
    );

    drop(stdin);
    let status = wrapper.wait().await.expect("failed to wait for wrapper");
    assert!(status.success(), "wrapper should exit successfully");
}

#[tokio::test(flavor = "multi_thread", worker_threads = 2)]
async fn reconnects_and_reregisters_after_disconnect() {
    let relay = MockRelayServer::start().await;
    let mock_codex = fixture_path("mock_codex_echo.sh");
    let mut wrapper = spawn_wrapper_process(
        &["--codex-binary", &mock_codex, "--relay-url", relay.url()],
        Vec::new(),
        None,
    )
    .await;

    let first_register = relay.next_message().await;
    let first_session_id = first_register
        .get("sessionId")
        .and_then(Value::as_str)
        .expect("first register missing sessionId")
        .to_string();

    relay.close_current_connection();

    let second_register = relay.next_message().await;
    assert_eq!(second_register.get("type").and_then(Value::as_str), Some("register"));
    assert_eq!(
        second_register.get("sessionId").and_then(Value::as_str),
        Some(first_session_id.as_str())
    );

    drop(wrapper.stdin.take());
    let status = wrapper.wait().await.expect("failed to wait for wrapper");
    assert!(status.success(), "wrapper should exit successfully");
}
