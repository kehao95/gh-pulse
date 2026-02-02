# gh-pulse

**GitHub. Spoken in JSON.**

Stream GitHub webhooks to your terminal as JSONL.

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
