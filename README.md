# gh-pulse

GitHub. Spoken in JSON.

[![Go](https://img.shields.io/badge/go-1.25%2B-00ADD8?style=flat-square&logo=go&logoColor=white)](https://golang.org/)
[![License: MIT](https://img.shields.io/badge/license-MIT-blue?style=flat-square)](LICENSE)
[![Status: Alpha](https://img.shields.io/badge/status-alpha-orange?style=flat-square)](#)

## Overview

gh-pulse is a tiny bridge that receives GitHub webhooks and streams them to your terminal as JSONL over WebSocket. It solves the "how do I wait on GitHub state changes in a script" problem by turning webhook traffic into a local, scriptable event stream with assertion-based exits.

Use it to gate deploys on PR merges, run forensic replay of events, or pipe real-time GitHub activity into any CLI tool that speaks JSONL.

## Quick Start

```bash
go build ./cmd/gh-pulse
./gh-pulse serve --port 8080
```

In another terminal:

```bash
./gh-pulse stream --server ws://localhost:8080/ws --event pull_request
```

Send a test webhook:

```bash
curl -X POST http://localhost:8080/webhook \
  -H "X-GitHub-Event: pull_request" \
  -H "X-GitHub-Delivery: test-123" \
  -d '{"action":"opened"}'
```

## Commands

### gh-pulse serve

Start the webhook server that accepts GitHub POSTs and broadcasts events to WebSocket clients.

```bash
gh-pulse serve [flags]

Flags:
  --port int    Port to listen on (default 8080)
```

Notes:
- Webhook endpoint: `http://localhost:<port>/webhook`
- WebSocket endpoint: `ws://localhost:<port>/ws`
- Signature verification is enabled when `GH_PULSE_WEBHOOK_SECRET` is set; otherwise it is skipped with a warning.

### gh-pulse stream

Connect to the WebSocket stream and write JSONL events to stdout in real time.

```bash
gh-pulse stream [flags]

Flags:
  --server string        WebSocket server URL (default "ws://localhost:8080/ws")
  --event string         Subscribe to GitHub event types (repeatable)
  --success-on string    Exit 0 when assertion matches (repeatable)
  --failure-on string    Exit 1 when assertion matches (repeatable)
  --timeout int          Exit 124 after timeout in seconds (0 = no timeout)
```

Behavior:
- If no `--event` is provided, all event types are streamed.
- Assertions match against the JSON payload and exit the process immediately when they match.
- Output is line-delimited JSON (`.jsonl`), one event per line.

### gh-pulse capture

Connect to the WebSocket stream, buffer events in memory, and dump the entire buffer on exit.

```bash
gh-pulse capture [flags]

Flags:
  --server string        WebSocket server URL (default "ws://localhost:8080/ws")
  --event string         Subscribe to GitHub event types (repeatable)
  --success-on string    Exit 0 when assertion matches (repeatable)
  --failure-on string    Exit 1 when assertion matches (repeatable)
  --timeout int          Exit 124 after timeout in seconds (0 = no timeout)
```

Notes:
- Capture requires at least one exit condition (`--success-on`, `--failure-on`, or `--timeout`).
- The buffer warns at 100MB and hard-fails at 500MB.

## Assertion Syntax

Assertions are used with `--success-on` and `--failure-on` flags. They evaluate against the JSON payload of each event.

```text
path=value     # equality
path=~regex    # regex
path exists    # existence
```

Examples:

```bash
--success-on "pull_request.merged=true"
--failure-on "pull_request.state=~closed"
--success-on "deployment_status.state exists"
```

Paths are dot-separated, and array indices are supported (e.g., `commits.0.author.name`).

## Exit Codes

| Code | Meaning |
| --- | --- |
| 0 | Success assertion matched |
| 1 | Failure assertion matched or fatal error |
| 124 | Timeout reached |
| 130 | Interrupted (SIGINT) |
| 143 | Terminated (SIGTERM) |

## Examples

### Deploy & Wait (PRD Scenario A)

Wait for a deployment to reach a healthy state before continuing.

```bash
./gh-pulse stream \
  --server ws://localhost:8080/ws \
  --event deployment_status \
  --success-on "deployment_status.state=success" \
  --failure-on "deployment_status.state=~(failure|error)" \
  --timeout 1800
```

### Forensic Analysis (PRD Scenario B)

Capture and replay a burst of events for postmortem analysis.

```bash
./gh-pulse capture \
  --server ws://localhost:8080/ws \
  --event push \
  --event pull_request \
  --event workflow_run \
  --timeout 300 > events.jsonl
```

Then inspect with jq:

```bash
jq -r '.event + " " + .delivery_id' events.jsonl | head
```

## Real GitHub Setup

End-to-end test with a real GitHub webhook using ngrok.

### Build

```bash
go build ./cmd/gh-pulse
```

### Generate secret

```bash
openssl rand -hex 20
```

### Start server

```bash
export GH_PULSE_WEBHOOK_SECRET=<secret>
./gh-pulse serve --port 8080
```

### Start ngrok tunnel (separate terminal)

```bash
ngrok http 8080
```

### Create GitHub webhook

```bash
gh api repos/<owner>/<repo>/hooks --method POST \
  -f url="https://<ngrok-url>/webhook" \
  -f content_type="json" \
  -f secret="<secret>" \
  -F events[]="push" \
  -F events[]="pull_request" \
  -F active=true
```

Save the `id` from the response so you can delete it later.

### Connect client

```bash
./gh-pulse stream --server ws://localhost:8080/ws --event push --event pull_request > events.jsonl 2>status.log
```

### Trigger event

Create a small commit, open a PR, or file an issue in the target repo.

### Verify output

```bash
tail -n 1 events.jsonl
```

### Cleanup webhook

```bash
gh api repos/<owner>/<repo>/hooks/<webhook-id> --method DELETE
```

## Output Schema

gh-pulse emits JSONL where each line is a JSON envelope with the GitHub event payload.

```json
{
  "type": "event",
  "event": "pull_request",
  "delivery_id": "test-123",
  "truncated": false,
  "payload": {
    "action": "opened",
    "pull_request": {
      "number": 42
    }
  }
}
```

Notes:
- `payload` is the original GitHub webhook JSON (possibly truncated for oversized payloads).
- When truncation occurs, `payload._truncated` includes keys and counts that were shortened.
