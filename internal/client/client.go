package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"time"

	"github.com/gorilla/websocket"
)

type Config struct {
	ServerURL string
}

func Run(ctx context.Context, cfg Config) error {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	stdout := bufio.NewWriter(os.Stdout)
	backoff := time.Second

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		logger.Printf("connecting to %s", cfg.ServerURL)
		conn, _, err := websocket.DefaultDialer.DialContext(ctx, cfg.ServerURL, nil)
		if err != nil {
			logger.Printf("connect failed: %v", err)
			wait(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}

		logger.Printf("connected to %s", cfg.ServerURL)
		backoff = time.Second

		err = readLoop(ctx, conn, stdout, logger)
		_ = conn.Close()
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Printf("disconnected: %v", err)
		} else {
			logger.Printf("disconnected")
		}
	}
}

func readLoop(ctx context.Context, conn *websocket.Conn, stdout *bufio.Writer, logger *log.Logger) error {
	done := make(chan error, 1)
	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				done <- err
				return
			}
			if !json.Valid(message) {
				logger.Printf("invalid json from server: %s", string(message))
				continue
			}
			if _, err := stdout.Write(message); err != nil {
				done <- err
				return
			}
			if err := stdout.WriteByte('\n'); err != nil {
				done <- err
				return
			}
			if err := stdout.Flush(); err != nil {
				done <- err
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	}
}

func wait(ctx context.Context, d time.Duration) {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
	case <-timer.C:
	}
}

func nextBackoff(current time.Duration) time.Duration {
	next := current * 2
	if next > 30*time.Second {
		return 30 * time.Second
	}
	return next
}
