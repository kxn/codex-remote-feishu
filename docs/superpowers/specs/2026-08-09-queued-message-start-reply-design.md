# Queued Message Start Reply Design

## Context

Feishu queue state is currently visible through reactions and typing markers on the
original user message. That is useful when the original message is still in view,
but a long active turn can push queued messages far up the chat. When the active
turn ends and the next queued item starts automatically, the user can miss which
queued message just began running.

## Goal

When a user-visible queued input is later taken from the queue and starts
executing automatically, send a short reply to that original message. The reply
acts as a timeline anchor at the bottom of the chat, so the user can see which
queued message just started.

## Non-Goals

- Do not send this notice for messages that execute immediately without first
  becoming queued.
- Do not send it for system/internal automation such as auto-continue,
  auto-whip, retries, compact follow-ups, or recovery work unless they are
  represented as user-visible queued inputs.
- Do not duplicate the full prompt text in the notice.
- Do not replace existing reactions or typing markers.

## Behavior

The trigger is the queue item transition from `queued` to `dispatching` in the
normal queue dispatch path. That transition means the input was previously
queued and is now being sent to the attached runtime.

For that transition, produce one Feishu reply against the queue item's primary
source message. The reply text should be short:

```text
开始执行这条排队消息。
```

An optional short preview can be added later if needed, but the initial behavior
should avoid copying the prompt body.

Immediate dispatches should remain silent because the user just sent the message
and has direct context. Existing pending-input events should continue to add or
remove queue and typing reactions.

## Implementation Shape

The semantic owner is the orchestrator queue layer, not the wrapper or Feishu
gateway. `dispatchNext` already owns the `queued -> dispatching` transition and
has access to the `QueueItemRecord`, source message id, and surface id.

The orchestrator should emit a normal notice-style UI event when it dispatches a
queued user-visible item. The daemon/Feishu projection layer should deliver that
notice as a reply to the original source message instead of a standalone chat
message. If an item has no source message id, skip the reply and keep the
existing dispatch behavior.

The notification must be emitted before or alongside the agent command event, so
the user sees the anchor as the queued item starts rather than after output
arrives.

## Testing

Add orchestrator tests for:

- enqueue while another queue item is active, complete the active turn, then
  verify the next item dispatch emits the start notice against the queued item's
  source message.
- immediate dispatch does not emit the start notice.
- non-user/internal queue source kinds do not emit the start notice unless they
  are intentionally marked user-visible.

Add projection or daemon tests for:

- the start notice becomes a Feishu reply operation targeting the source message.
- missing source message id does not produce a malformed reply operation.
