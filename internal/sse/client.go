package sse

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/kehao95/gh-pulse/internal/message"
)

type Client struct {
	URL        string
	HTTPClient *http.Client
	Logger     *log.Logger
}

func NewClient(url string, logger *log.Logger) *Client {
	return &Client{URL: url, HTTPClient: http.DefaultClient, Logger: logger}
}

func (c *Client) Run(ctx context.Context, handle func(message.EventMessage) error) error {
	client := c.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	backoff := time.Second

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		if c.Logger != nil {
			c.Logger.Printf("connecting to %s", c.URL)
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.URL, nil)
		if err != nil {
			return err
		}
		req.Header.Set("Accept", "text/event-stream")

		resp, err := client.Do(req)
		if err != nil {
			if c.Logger != nil {
				c.Logger.Printf("connect failed: %v", err)
			}
			wait(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}

		if resp.StatusCode != http.StatusOK {
			if c.Logger != nil {
				c.Logger.Printf("unexpected status: %s", resp.Status)
			}
			_ = resp.Body.Close()
			wait(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}

		if c.Logger != nil {
			c.Logger.Printf("connected to %s", c.URL)
		}
		backoff = time.Second

		err = c.readStream(ctx, resp.Body, handle)
		_ = resp.Body.Close()

		if errors.Is(err, context.Canceled) {
			return err
		}
		if err == nil {
			if c.Logger != nil {
				c.Logger.Printf("disconnected")
			}
			wait(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}
		if errors.Is(err, io.EOF) {
			if c.Logger != nil {
				c.Logger.Printf("disconnected")
			}
			wait(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}
		var streamErr streamError
		if errors.As(err, &streamErr) {
			if c.Logger != nil {
				c.Logger.Printf("disconnected: %v", streamErr.err)
			}
			wait(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}

		return err
	}
}

type streamError struct {
	err error
}

func (e streamError) Error() string {
	return e.err.Error()
}

func (e streamError) Unwrap() error {
	return e.err
}

type smeePayload struct {
	Event      string      `json:"x-github-event"`
	DeliveryID string      `json:"x-github-delivery"`
	Body       interface{} `json:"body"`
}

type sseEvent struct {
	id    string
	event string
	data  []string
}

func (c *Client) readStream(ctx context.Context, body io.Reader, handle func(message.EventMessage) error) error {
	reader := bufio.NewReader(body)
	current := sseEvent{}

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		line, err := reader.ReadString('\n')
		if err != nil {
			return streamError{err: err}
		}

		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if len(current.data) == 0 {
				current = sseEvent{}
				continue
			}
			if current.event == "ready" {
				current = sseEvent{}
				continue
			}

			payload, err := decodeSmeeData(strings.Join(current.data, "\n"))
			if err != nil {
				if c.Logger != nil {
					c.Logger.Printf("failed to decode smee payload: %v", err)
				}
				current = sseEvent{}
				continue
			}

			if err := handle(payload); err != nil {
				return err
			}

			current = sseEvent{}
			continue
		}

		if strings.HasPrefix(line, ":") {
			continue
		}

		field, value := splitSSELine(line)
		switch field {
		case "id":
			current.id = value
		case "event":
			current.event = value
		case "data":
			current.data = append(current.data, value)
		}
	}
}

func splitSSELine(line string) (string, string) {
	idx := strings.Index(line, ":")
	if idx == -1 {
		return line, ""
	}
	field := line[:idx]
	value := line[idx+1:]
	if strings.HasPrefix(value, " ") {
		value = value[1:]
	}
	return field, value
}

func decodeSmeeData(raw string) (message.EventMessage, error) {
	var payload smeePayload
	if err := json.Unmarshal([]byte(raw), &payload); err != nil {
		return message.EventMessage{}, err
	}
	if payload.Event == "" {
		return message.EventMessage{}, fmt.Errorf("missing x-github-event")
	}
	if payload.DeliveryID == "" {
		return message.EventMessage{}, fmt.Errorf("missing x-github-delivery")
	}

	body, err := json.Marshal(payload.Body)
	if err != nil {
		return message.EventMessage{}, fmt.Errorf("failed to encode smee body: %w", err)
	}

	return message.EventMessage{
		Type:       "event",
		Event:      payload.Event,
		DeliveryID: payload.DeliveryID,
		Truncated:  false,
		Payload:    body,
	}, nil
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
