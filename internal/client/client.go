package client

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kehao95/gh-pulse/internal/assertion"
)

type Config struct {
	ServerURL         string
	Events            []string
	SuccessAssertions []assertion.Assertion
	FailureAssertions []assertion.Assertion
	Timeout           time.Duration
}

const (
	warnBufferBytes = 100 * 1024 * 1024
	maxBufferBytes  = 500 * 1024 * 1024
)

type exitError struct {
	code int
}

func (e exitError) Error() string {
	return fmt.Sprintf("exit with code %d", e.code)
}

func (e exitError) ExitCode() int {
	return e.code
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

		if err := sendSubscribe(conn, cfg.Events); err != nil {
			logger.Printf("subscribe failed: %v", err)
			_ = conn.Close()
			wait(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}

		err = readLoop(ctx, conn, stdout, logger, cfg.SuccessAssertions, cfg.FailureAssertions, cfg.Timeout)
		_ = conn.Close()
		if err != nil {
			var exitErr interface{ ExitCode() int }
			if errors.As(err, &exitErr) {
				return err
			}
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Printf("disconnected: %v", err)
		} else {
			logger.Printf("disconnected")
		}
	}
}

func RunCapture(ctx context.Context, cfg Config) error {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	stdout := bufio.NewWriter(os.Stdout)
	backoff := time.Second

	buffer := make([][]byte, 0, 128)
	var bufferBytes int64
	warned := false

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

		if err := sendSubscribe(conn, cfg.Events); err != nil {
			logger.Printf("subscribe failed: %v", err)
			_ = conn.Close()
			wait(ctx, backoff)
			backoff = nextBackoff(backoff)
			continue
		}

		err = readLoopCapture(ctx, conn, logger, &buffer, &bufferBytes, &warned, cfg.SuccessAssertions, cfg.FailureAssertions, cfg.Timeout)
		_ = conn.Close()
		if err != nil {
			var exitErr interface{ ExitCode() int }
			if errors.As(err, &exitErr) {
				if dumpErr := dumpBuffer(stdout, buffer); dumpErr != nil {
					return dumpErr
				}
				return err
			}
			var fatalErr fatalError
			if errors.As(err, &fatalErr) {
				return fatalErr
			}
		}
		if err != nil && !errors.Is(err, context.Canceled) {
			logger.Printf("disconnected: %v", err)
		} else {
			logger.Printf("disconnected")
		}
	}
}

func readLoop(ctx context.Context, conn *websocket.Conn, stdout *bufio.Writer, logger *log.Logger, successAssertions []assertion.Assertion, failureAssertions []assertion.Assertion, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				done <- err
				return
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

			if !json.Valid(message) {
				logger.Printf("invalid json from server: %s", string(message))
			}

			if matchesAssertions(message, successAssertions) {
				done <- exitError{code: 0}
				return
			}
			if matchesAssertions(message, failureAssertions) {
				done <- exitError{code: 1}
				return
			}
		}
	}()

	var timeoutCh <-chan time.Time
	var timer *time.Timer
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		defer timer.Stop()
		timeoutCh = timer.C
	}

	select {
	case <-ctx.Done():
		return ctx.Err()
	case err := <-done:
		return err
	case <-timeoutCh:
		return exitError{code: 124}
	}
}

type fatalError struct {
	err error
}

func (e fatalError) Error() string {
	return e.err.Error()
}

func (e fatalError) Unwrap() error {
	return e.err
}

func readLoopCapture(ctx context.Context, conn *websocket.Conn, logger *log.Logger, buffer *[][]byte, bufferBytes *int64, warned *bool, successAssertions []assertion.Assertion, failureAssertions []assertion.Assertion, timeout time.Duration) error {
	done := make(chan error, 1)
	go func() {
		for {
			_, message, err := conn.ReadMessage()
			if err != nil {
				done <- err
				return
			}
			*buffer = append(*buffer, message)
			*bufferBytes += int64(len(message))
			if !*warned && *bufferBytes >= warnBufferBytes {
				logger.Printf("capture buffer exceeded 100MB")
				*warned = true
			}
			if *bufferBytes >= maxBufferBytes {
				done <- fatalError{err: fmt.Errorf("capture buffer exceeded 500MB")}
				return
			}

			if !json.Valid(message) {
				logger.Printf("invalid json from server: %s", string(message))
			}

			if matchesAssertions(message, successAssertions) {
				done <- exitError{code: 0}
				return
			}
			if matchesAssertions(message, failureAssertions) {
				done <- exitError{code: 1}
				return
			}
		}
	}()

	var timeoutCh <-chan time.Time
	var timer *time.Timer
	if timeout > 0 {
		timer = time.NewTimer(timeout)
		defer timer.Stop()
		timeoutCh = timer.C
	}

	select {
	case <-ctx.Done():
		_ = conn.Close()
		return ctx.Err()
	case err := <-done:
		return err
	case <-timeoutCh:
		_ = conn.Close()
		<-done
		return exitError{code: 124}
	}
}

func dumpBuffer(stdout *bufio.Writer, buffer [][]byte) error {
	for _, message := range buffer {
		if _, err := stdout.Write(message); err != nil {
			return err
		}
		if err := stdout.WriteByte('\n'); err != nil {
			return err
		}
	}
	return stdout.Flush()
}

func matchesAssertions(message []byte, assertions []assertion.Assertion) bool {
	if len(assertions) == 0 {
		return false
	}

	var payload interface{}
	if err := json.Unmarshal(message, &payload); err != nil {
		return false
	}

	for _, rule := range assertions {
		value, ok := valueAtPath(payload, rule.Path)
		switch rule.Operator {
		case "exists":
			if ok {
				return true
			}
		case "eq":
			if ok && stringifyJSON(value) == rule.Value {
				return true
			}
		case "regex":
			if ok {
				re, err := regexp.Compile(rule.Value)
				if err != nil {
					continue
				}
				if re.MatchString(stringifyJSON(value)) {
					return true
				}
			}
		}
	}

	return false
}

func valueAtPath(payload interface{}, path string) (interface{}, bool) {
	current := payload
	for _, part := range strings.Split(path, ".") {
		switch node := current.(type) {
		case map[string]interface{}:
			child, ok := node[part]
			if !ok {
				return nil, false
			}
			current = child
		case []interface{}:
			idx, err := strconv.Atoi(part)
			if err != nil || idx < 0 || idx >= len(node) {
				return nil, false
			}
			current = node[idx]
		default:
			return nil, false
		}
	}
	return current, true
}

func stringifyJSON(value interface{}) string {
	switch v := value.(type) {
	case string:
		return v
	default:
		encoded, err := json.Marshal(v)
		if err != nil {
			return fmt.Sprint(v)
		}
		return string(encoded)
	}
}

func sendSubscribe(conn *websocket.Conn, events []string) error {
	if events == nil {
		events = []string{}
	}
	msg := subscribeMessage{
		Type:   "subscribe",
		Events: events,
	}
	encoded, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	return conn.WriteMessage(websocket.TextMessage, encoded)
}

type subscribeMessage struct {
	Type   string   `json:"type"`
	Events []string `json:"events"`
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
