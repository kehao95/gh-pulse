# gh-pulse

Bridge GitHub webhooks to a local CLI via WebSocket.

## Build

```bash
go build ./cmd/gh-pulse
```

## Run server

```bash
./gh-pulse serve --port 8080
```

## Run client

```bash
./gh-pulse stream --server ws://localhost:8080/ws --event push --event pull_request
```

## Send test webhook

```bash
curl -X POST http://localhost:8080/webhook \
  -H "X-GitHub-Event: push" \
  -H "X-GitHub-Delivery: test-123" \
  -d '{"ref":"refs/heads/main"}'
```

## Real GitHub Setup

End-to-end test with real GitHub webhooks.

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
GH_PULSE_WEBHOOK_SECRET=<secret> ./gh-pulse serve --port 8080
```

### Start ngrok tunnel (separate terminal)

```bash
ngrok http 8080
```

### Create GitHub webhook

```bash
gh api repos/kehao95/gh-pulse/hooks --method POST \
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

Create a small commit, open a PR, or file an issue in `kehao95/gh-pulse`.

### Verify output

```bash
tail -n 1 events.jsonl
```

### Cleanup webhook

```bash
gh api repos/kehao95/gh-pulse/hooks/<webhook-id> --method DELETE
```
