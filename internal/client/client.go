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

	"github.com/kehao95/gh-pulse/internal/assertion"
	"github.com/kehao95/gh-pulse/internal/message"
	"github.com/kehao95/gh-pulse/internal/sse"
)

type Config struct {
	URL               string
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
	client := sse.NewClient(cfg.URL, logger)
	return runWithTimeout(ctx, cfg.Timeout, func(runCtx context.Context) error {
		return client.Run(runCtx, func(msg message.EventMessage) error {
			if !eventAllowed(cfg.Events, msg.Event) {
				return nil
			}
			encoded, err := json.Marshal(msg)
			if err != nil {
				logger.Printf("failed to encode event: %v", err)
				return nil
			}
			if _, err := stdout.Write(encoded); err != nil {
				return err
			}
			if err := stdout.WriteByte('\n'); err != nil {
				return err
			}
			if err := stdout.Flush(); err != nil {
				return err
			}

			if matchesAssertions(encoded, cfg.SuccessAssertions) {
				return exitError{code: 0}
			}
			if matchesAssertions(encoded, cfg.FailureAssertions) {
				return exitError{code: 1}
			}
			return nil
		})
	})
}

func RunCapture(ctx context.Context, cfg Config) error {
	logger := log.New(os.Stderr, "", log.LstdFlags)
	stdout := bufio.NewWriter(os.Stdout)
	buffer := make([][]byte, 0, 128)
	var bufferBytes int64
	warned := false
	client := sse.NewClient(cfg.URL, logger)

	err := runWithTimeout(ctx, cfg.Timeout, func(runCtx context.Context) error {
		return client.Run(runCtx, func(msg message.EventMessage) error {
			if !eventAllowed(cfg.Events, msg.Event) {
				return nil
			}
			encoded, err := json.Marshal(msg)
			if err != nil {
				logger.Printf("failed to encode event: %v", err)
				return nil
			}
			buffer = append(buffer, encoded)
			bufferBytes += int64(len(encoded))
			if !warned && bufferBytes >= warnBufferBytes {
				logger.Printf("capture buffer exceeded 100MB")
				warned = true
			}
			if bufferBytes >= maxBufferBytes {
				return fatalError{err: fmt.Errorf("capture buffer exceeded 500MB")}
			}

			if matchesAssertions(encoded, cfg.SuccessAssertions) {
				return exitError{code: 0}
			}
			if matchesAssertions(encoded, cfg.FailureAssertions) {
				return exitError{code: 1}
			}
			return nil
		})
	})
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
	return err
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

func runWithTimeout(ctx context.Context, timeout time.Duration, run func(context.Context) error) error {
	runCtx, cancel := context.WithCancel(ctx)
	defer cancel()

	done := make(chan error, 1)
	go func() {
		done <- run(runCtx)
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
		cancel()
		<-done
		return ctx.Err()
	case err := <-done:
		return err
	case <-timeoutCh:
		cancel()
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

func eventAllowed(events []string, candidate string) bool {
	if len(events) == 0 {
		return true
	}
	for _, event := range events {
		if event == candidate {
			return true
		}
	}
	return false
}
