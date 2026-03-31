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
pub enum OutboundRelayMessage {
    Classified {
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

pub fn spawn_relay_client(
    relay_url: String,
    registration: RelayRegistration,
    attached: Arc<AtomicBool>,
    child_input_tx: mpsc::UnboundedSender<ChildInputCommand>,
) -> (mpsc::UnboundedSender<OutboundRelayMessage>, JoinHandle<()>) {
    let (outbound_tx, outbound_rx) = mpsc::unbounded_channel();
    let handle = tokio::spawn(run_relay_client(
        relay_url,
        registration,
        attached,
        child_input_tx,
        outbound_rx,
    ));

    (outbound_tx, handle)
}

async fn run_relay_client(
    relay_url: String,
    registration: RelayRegistration,
    attached: Arc<AtomicBool>,
    child_input_tx: mpsc::UnboundedSender<ChildInputCommand>,
    mut outbound_rx: mpsc::UnboundedReceiver<OutboundRelayMessage>,
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
            tokio::time::sleep(reconnect_delay).await;
            reconnect_delay = next_backoff(reconnect_delay);
            continue;
        }

        info!(session_id = %registration.session_id, "registered wrapper session with relay server");

        let disconnected = loop {
            tokio::select! {
                outbound = outbound_rx.recv() => {
                    match outbound {
                        Some(message) => {
                            let payload = outbound_message_to_json(&registration.session_id, message);
                            if let Err(error) = write.send(Message::Text(payload.to_string().into())).await {
                                warn!(relay_url = %relay_url, error = %error, "failed sending message to relay server");
                                break true;
                            }
                        }
                        None => break false,
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
