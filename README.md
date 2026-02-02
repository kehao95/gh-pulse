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
./gh-pulse stream --server ws://localhost:8080/ws
```

## Send test webhook

```bash
curl -X POST http://localhost:8080/webhook \
  -H "X-GitHub-Event: push" \
  -H "X-GitHub-Delivery: test-123" \
  -d '{"ref":"refs/heads/main"}'
```
