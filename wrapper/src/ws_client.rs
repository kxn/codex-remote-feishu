use std::sync::atomic::{AtomicBool, Ordering};
use std::sync::Arc;
use std::time::Duration;

use futures::{SinkExt, StreamExt};
use serde_json::{json, Value};
use tokio::sync::mpsc;
use tokio::task::JoinHandle;
use tokio::time::timeout;
use tokio_tungstenite::connect_async;
use tokio_tungstenite::tungstenite::Message;
use tracing::{debug, info, warn};

use crate::proxy::ChildInputCommand;

const INITIAL_RECONNECT_DELAY: Duration = Duration::from_millis(100);
const MAX_RECONNECT_DELAY: Duration = Duration::from_secs(5);
const CONNECT_TIMEOUT: Duration = Duration::from_secs(5);

#[derive(Debug, Clone)]
pub struct RelayRegistration {
    pub session_id: String,
    pub display_name: String,
    pub workspace_path: String,
    pub wrapper_version: String,
    pub pid: u32,
}

#[derive(Debug, Clone)]
pub enum RelayForwardingMode {
    Always,
    WhenAttached,
}

#[derive(Debug, Clone)]
pub enum OutboundRelayMessage {
    Classified {
        forwarding_mode: RelayForwardingMode,
        classification: &'static str,
        method: Option<String>,
        thread_id: Option<String>,
        turn_id: Option<String>,
        raw: String,
        payload: Option<Value>,
    },
    AutoDetach {
        reason: &'static str,
    },
}

#[derive(Debug)]
pub enum RelayTaskCommand {
    Outbound(OutboundRelayMessage),
    Shutdown,
}

pub fn should_forward_outbound_message(attached: bool, message: &OutboundRelayMessage) -> bool {
    match message {
        OutboundRelayMessage::Classified {
            forwarding_mode: RelayForwardingMode::Always,
            ..
        }
        | OutboundRelayMessage::AutoDetach { .. } => true,
        OutboundRelayMessage::Classified {
            forwarding_mode: RelayForwardingMode::WhenAttached,
            ..
        } => attached,
    }
}

pub type RelayTaskSender = mpsc::UnboundedSender<RelayTaskCommand>;

pub fn send_relay_message(
    relay_tx: &RelayTaskSender,
    message: OutboundRelayMessage,
) -> Result<(), mpsc::error::SendError<RelayTaskCommand>> {
    relay_tx.send(RelayTaskCommand::Outbound(message))
}

pub fn shutdown_relay_client(
    relay_tx: RelayTaskSender,
) -> Result<(), mpsc::error::SendError<RelayTaskCommand>> {
    relay_tx.send(RelayTaskCommand::Shutdown)
}

pub fn spawn_relay_client(
    relay_url: String,
    registration: RelayRegistration,
    attached: Arc<AtomicBool>,
    child_input_tx: mpsc::UnboundedSender<ChildInputCommand>,
) -> (RelayTaskSender, JoinHandle<()>) {
    let (command_tx, command_rx) = mpsc::unbounded_channel();
    let handle = tokio::spawn(run_relay_client(
        relay_url,
        registration,
        attached,
        child_input_tx,
        command_rx,
    ));

    (command_tx, handle)
}

async fn run_relay_client(
    relay_url: String,
    registration: RelayRegistration,
    attached: Arc<AtomicBool>,
    child_input_tx: mpsc::UnboundedSender<ChildInputCommand>,
    mut command_rx: mpsc::UnboundedReceiver<RelayTaskCommand>,
) {
    let mut reconnect_delay = INITIAL_RECONNECT_DELAY;

    loop {
        let connection = timeout(CONNECT_TIMEOUT, connect_async(relay_url.as_str())).await;
        let websocket = match connection {
            Ok(Ok((stream, _response))) => {
                info!(relay_url = %relay_url, session_id = %registration.session_id, "connected to relay server");
                reconnect_delay = INITIAL_RECONNECT_DELAY;
                stream
            }
            Ok(Err(error)) => {
                warn!(relay_url = %relay_url, error = %error, "failed to connect to relay server; continuing in degraded mode");
                tokio::time::sleep(reconnect_delay).await;
                reconnect_delay = next_backoff(reconnect_delay);
                continue;
            }
            Err(_) => {
                warn!(relay_url = %relay_url, timeout_ms = CONNECT_TIMEOUT.as_millis(), "timed out connecting to relay server; continuing in degraded mode");
                tokio::time::sleep(reconnect_delay).await;
                reconnect_delay = next_backoff(reconnect_delay);
                continue;
            }
        };

        let (mut write, mut read) = websocket.split();
        let register = registration_message(&registration);

        if let Err(error) = write.send(Message::Text(register.to_string().into())).await {
            warn!(relay_url = %relay_url, error = %error, "failed to send relay registration");
            handle_disconnect(&attached, &mut command_rx);
            tokio::time::sleep(reconnect_delay).await;
            reconnect_delay = next_backoff(reconnect_delay);
            continue;
        }

        info!(session_id = %registration.session_id, "registered wrapper session with relay server");

        let disconnected = loop {
            tokio::select! {
                command = command_rx.recv() => {
                    match command {
                        Some(RelayTaskCommand::Outbound(message)) => {
                            let payload = outbound_message_to_json(&registration.session_id, message);
                            if let Err(error) = write.send(Message::Text(payload.to_string().into())).await {
                                warn!(relay_url = %relay_url, error = %error, "failed sending message to relay server");
                                break true;
                            }
                        }
                        Some(RelayTaskCommand::Shutdown) | None => {
                            let _ = write.send(Message::Close(None)).await;
                            break false;
                        }
                    }
                }
                inbound = read.next() => {
                    match inbound {
                        Some(Ok(Message::Text(text))) => {
                            handle_incoming_message(text.as_ref(), &attached, &child_input_tx);
                        }
                        Some(Ok(Message::Binary(bytes))) => {
                            match String::from_utf8(bytes.to_vec()) {
                                Ok(text) => handle_incoming_message(&text, &attached, &child_input_tx),
                                Err(error) => warn!(error = %error, "received non-utf8 binary relay message; ignoring"),
                            }
                        }
                        Some(Ok(Message::Ping(payload))) => {
                            if let Err(error) = write.send(Message::Pong(payload)).await {
                                warn!(relay_url = %relay_url, error = %error, "failed responding to relay ping");
                                break true;
                            }
                        }
                        Some(Ok(Message::Close(frame))) => {
                            debug!(?frame, "relay server closed websocket connection");
                            break true;
                        }
                        Some(Ok(_)) => {}
                        Some(Err(error)) => {
                            warn!(relay_url = %relay_url, error = %error, "relay websocket error");
                            break true;
                        }
                        None => {
                            warn!(relay_url = %relay_url, "relay websocket connection closed");
                            break true;
                        }
                    }
                }
            }
        };

        if !disconnected {
            return;
        }

        handle_disconnect(&attached, &mut command_rx);
        tokio::time::sleep(reconnect_delay).await;
        reconnect_delay = next_backoff(reconnect_delay);
    }
}

fn registration_message(registration: &RelayRegistration) -> Value {
    json!({
        "type": "register",
        "sessionId": registration.session_id,
        "displayName": registration.display_name,
        "metadata": {
            "version": registration.wrapper_version,
            "workspacePath": registration.workspace_path,
            "pid": registration.pid,
        }
    })
}

fn outbound_message_to_json(session_id: &str, message: OutboundRelayMessage) -> Value {
    match message {
        OutboundRelayMessage::Classified {
            forwarding_mode: _,
            classification,
            method,
            thread_id,
            turn_id,
            raw,
            payload,
        } => {
            json!({
                "type": "message",
                "sessionId": session_id,
                "direction": "out",
                "classification": classification,
                "method": method,
                "threadId": thread_id,
                "turnId": turn_id,
                "raw": raw,
                "payload": payload,
            })
        }
        OutboundRelayMessage::AutoDetach { reason } => {
            json!({
                "type": "auto-detach",
                "sessionId": session_id,
                "reason": reason,
            })
        }
    }
}

fn handle_incoming_message(
    text: &str,
    attached: &Arc<AtomicBool>,
    child_input_tx: &mpsc::UnboundedSender<ChildInputCommand>,
) {
    let parsed = match serde_json::from_str::<Value>(text) {
        Ok(value) => value,
        Err(error) => {
            warn!(error = %error, raw = %text, "failed to parse relay server message");
            return;
        }
    };

    match parsed.get("type").and_then(Value::as_str) {
        Some("attach-status-changed") => {
            let is_attached = parsed
                .get("attached")
                .and_then(Value::as_bool)
                .unwrap_or(false);
            attached.store(is_attached, Ordering::SeqCst);
            debug!(attached = is_attached, "updated relay attachment state");
        }
        Some("input") => {
            if !attached.load(Ordering::SeqCst) {
                debug!("ignoring relay input because no user is attached");
                return;
            }

            if let Some(command) = build_input_command(&parsed) {
                let _ = child_input_tx.send(command);
            } else {
                debug!(message = %parsed, "ignoring unsupported relay input message");
            }
        }
        Some("approval-response") => {
            if !attached.load(Ordering::SeqCst) {
                debug!("ignoring relay approval response because no user is attached");
                return;
            }

            if let Some(command) = build_approval_response_command(&parsed) {
                let _ = child_input_tx.send(command);
            } else {
                debug!(message = %parsed, "ignoring malformed relay approval response");
            }
        }
        Some("interrupt" | "turn/interrupt") => {
            if !attached.load(Ordering::SeqCst) {
                debug!("ignoring relay interrupt because no user is attached");
                return;
            }

            let _ = child_input_tx.send(ChildInputCommand::Write(turn_interrupt_message()));
        }
        Some("turn/start") => {
            if !attached.load(Ordering::SeqCst) {
                debug!("ignoring relay turn/start because no user is attached");
                return;
            }

            if let Some(command) = build_direct_turn_start_command(&parsed) {
                let _ = child_input_tx.send(command);
            }
        }
        Some(other) => {
            debug!(message_type = other, "ignoring unsupported relay message type");
        }
        None => {
            debug!(message = %parsed, "ignoring relay message without type");
        }
    }
}

fn build_input_command(value: &Value) -> Option<ChildInputCommand> {
    if let Some(content) = value.get("content").and_then(Value::as_str) {
        return Some(ChildInputCommand::Write(turn_start_message(content)));
    }

    match value.get("method").and_then(Value::as_str) {
        Some("turn/start") => build_direct_turn_start_command(value),
        Some("turn/interrupt") => Some(ChildInputCommand::Write(turn_interrupt_message())),
        _ => None,
    }
}

fn build_approval_response_command(value: &Value) -> Option<ChildInputCommand> {
    let request_id = value.get("requestId").filter(is_jsonrpc_request_id)?.clone();
    let decision = Value::String(value.get("decision")?.as_str()?.to_owned());

    Some(ChildInputCommand::Write(
        json!({
            "id": request_id,
            "result": {
                "decision": decision,
            }
        })
        .to_string()
        .into_bytes()
        .into_iter()
        .chain(std::iter::once(b'\n'))
        .collect(),
    ))
}

fn is_jsonrpc_request_id(value: &&Value) -> bool {
    matches!(value, Value::String(_) | Value::Number(_))
}

fn build_direct_turn_start_command(value: &Value) -> Option<ChildInputCommand> {
    let params = value.get("params").cloned().or_else(|| {
        value.get("content")
            .and_then(Value::as_str)
            .map(|content| json!({ "prompt": content }))
    })?;

    Some(ChildInputCommand::Write(
        json!({
            "method": "turn/start",
            "params": params,
        })
        .to_string()
        .into_bytes()
        .into_iter()
        .chain(std::iter::once(b'\n'))
        .collect(),
    ))
}

fn turn_start_message(prompt: &str) -> Vec<u8> {
    json!({
        "method": "turn/start",
        "params": {
            "prompt": prompt,
        }
    })
    .to_string()
    .into_bytes()
    .into_iter()
    .chain(std::iter::once(b'\n'))
    .collect()
}

fn turn_interrupt_message() -> Vec<u8> {
    json!({
        "method": "turn/interrupt",
    })
    .to_string()
    .into_bytes()
    .into_iter()
    .chain(std::iter::once(b'\n'))
    .collect()
}

fn next_backoff(current: Duration) -> Duration {
    std::cmp::min(current.saturating_mul(2), MAX_RECONNECT_DELAY)
}

fn handle_disconnect(
    attached: &Arc<AtomicBool>,
    command_rx: &mut mpsc::UnboundedReceiver<RelayTaskCommand>,
) {
    attached.store(false, Ordering::SeqCst);

    let mut dropped_messages = 0usize;
    while let Ok(command) = command_rx.try_recv() {
        match command {
            RelayTaskCommand::Outbound(_) => dropped_messages += 1,
            RelayTaskCommand::Shutdown => return,
        }
    }

    if dropped_messages > 0 {
        debug!(
            dropped_messages,
            "dropped queued relay messages after disconnect"
        );
    }
}

#[cfg(test)]
mod tests {
    use super::*;
    use tokio::sync::mpsc::error::TryRecvError;

    #[test]
    fn disconnect_resets_attached_state_and_drops_queued_messages() {
        let attached = Arc::new(AtomicBool::new(true));
        let (command_tx, mut command_rx) = mpsc::unbounded_channel();

        command_tx
            .send(RelayTaskCommand::Outbound(OutboundRelayMessage::AutoDetach {
                reason: "local-input",
            }))
            .expect("failed to enqueue first relay message");
        command_tx
            .send(RelayTaskCommand::Outbound(OutboundRelayMessage::AutoDetach {
                reason: "second-message",
            }))
            .expect("failed to enqueue second relay message");

        handle_disconnect(&attached, &mut command_rx);

        assert!(!attached.load(Ordering::SeqCst));
        assert!(matches!(
            command_rx.try_recv(),
            Err(tokio::sync::mpsc::error::TryRecvError::Empty)
        ));
    }

    #[test]
    fn approval_response_is_forwarded_as_jsonrpc_result() {
        let attached = Arc::new(AtomicBool::new(true));
        let (child_input_tx, mut child_input_rx) = mpsc::unbounded_channel();

        handle_incoming_message(
            r#"{"type":"approval-response","requestId":"req-1","decision":"accept"}"#,
            &attached,
            &child_input_tx,
        );

        let command = child_input_rx
            .try_recv()
            .expect("approval response should be forwarded");
        let ChildInputCommand::Write(bytes) = command else {
            panic!("expected approval response write command");
        };

        assert_eq!(
            String::from_utf8(bytes).expect("approval response should be utf8"),
            "{\"id\":\"req-1\",\"result\":{\"decision\":\"accept\"}}\n"
        );
    }

    #[test]
    fn detached_state_ignores_approval_response_messages() {
        let attached = Arc::new(AtomicBool::new(false));
        let (child_input_tx, mut child_input_rx) = mpsc::unbounded_channel();

        handle_incoming_message(
            r#"{"type":"approval-response","requestId":"req-1","decision":"decline"}"#,
            &attached,
            &child_input_tx,
        );

        assert!(matches!(child_input_rx.try_recv(), Err(TryRecvError::Empty)));
    }

    #[test]
    fn always_forwarded_messages_ignore_attachment_state() {
        let message = OutboundRelayMessage::Classified {
            forwarding_mode: RelayForwardingMode::Always,
            classification: "turnLifecycle",
            method: Some("turn/started".to_owned()),
            thread_id: Some("thread-1".to_owned()),
            turn_id: Some("turn-1".to_owned()),
            raw: "{\"method\":\"turn/started\"}\n".to_owned(),
            payload: None,
        };

        assert!(should_forward_outbound_message(true, &message));
        assert!(should_forward_outbound_message(false, &message));
    }

    #[test]
    fn agent_messages_require_attachment_state() {
        let message = OutboundRelayMessage::Classified {
            forwarding_mode: RelayForwardingMode::WhenAttached,
            classification: "agentMessage",
            method: Some("item/agentMessage/delta".to_owned()),
            thread_id: Some("thread-1".to_owned()),
            turn_id: Some("turn-1".to_owned()),
            raw: "{\"method\":\"item/agentMessage/delta\"}\n".to_owned(),
            payload: None,
        };

        assert!(should_forward_outbound_message(true, &message));
        assert!(!should_forward_outbound_message(false, &message));
    }
}
