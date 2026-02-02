# gh-pulse

**GitHub. Spoken in JSON.**

Stream GitHub webhooks to your terminal as JSONL.

## Install

```bash
go install github.com/kehao95/gh-pulse/cmd/gh-pulse@latest
```

Or build from source:

```bash
git clone https://github.com/kehao95/gh-pulse
cd gh-pulse
go build ./cmd/gh-pulse
```

## Quick Start

1. Pick a smee.io channel (any unique URL works):
   ```bash
   export SMEE_URL="https://smee.io/my-repo-name"
   ```

2. Configure GitHub webhook -> Settings -> Webhooks -> Add:
   - Payload URL: `$SMEE_URL`
   - Content type: `application/json`

3. Stream events:
   ```bash
   gh-pulse stream --url "$SMEE_URL"
   ```

That's it. No server, no registration, no tokens.

## Testing

Verify the full pipeline works:

```bash
# Terminal 1: Start streaming (pick any unique channel name)
./gh-pulse stream --url https://smee.io/test-gh-pulse-$(whoami) --timeout 30

# Terminal 2: Send a test webhook
curl -X POST https://smee.io/test-gh-pulse-$(whoami) \
  -H "Content-Type: application/json" \
  -H "X-GitHub-Event: push" \
  -H "X-GitHub-Delivery: test-123" \
  -d '{"ref":"refs/heads/main","commits":[]}'
```

Terminal 1 should output:

```json
{"type":"event","event":"push","delivery_id":"test-123","payload":{"ref":"refs/heads/main","commits":[]}}
```

## Commands

```text
gh-pulse stream --url <smee_url> [--event <event>] [--success-on <assertion>] [--failure-on <assertion>] [--timeout <seconds>]
gh-pulse capture --url <smee_url> [--event <event>] [--success-on <assertion>] [--failure-on <assertion>] [--timeout <seconds>]
```

## Assertions

```text
path=value     # equality
path=~regex    # regex
path exists    # existence
```

Example:

```bash
gh-pulse stream --url "$SMEE_URL" --success-on "event=push"
```

## Exit Codes

| Code | Meaning |
| --- | --- |
| 0 | Success assertion matched |
| 1 | Failure assertion matched or fatal error |
| 124 | Timeout reached |
| 130 | Interrupted (SIGINT) |
| 143 | Terminated (SIGTERM) |

## Examples

Wait for a deployment to succeed:

```bash
gh-pulse stream \
  --url "$SMEE_URL" \
  --event deployment_status \
  --success-on "deployment_status.state=success" \
  --failure-on "deployment_status.state=~(failure|error)" \
  --timeout 1800
```

Capture a burst of events for review:

```bash
gh-pulse capture \
  --url "$SMEE_URL" \
  --event push \
  --event pull_request \
  --timeout 300 > events.jsonl
```
