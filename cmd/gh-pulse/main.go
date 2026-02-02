package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/kehao95/gh-pulse/internal/assertion"
	"github.com/kehao95/gh-pulse/internal/client"
	"github.com/spf13/cobra"
)

var version = "dev"

type exitError struct {
	code int
}

func (e exitError) Error() string {
	return fmt.Sprintf("exit with code %d", e.code)
}

func (e exitError) ExitCode() int {
	return e.code
}

func runWithSignals(run func(context.Context) error) error {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
	defer signal.Stop(sigCh)

	errCh := make(chan error, 1)
	go func() {
		errCh <- run(ctx)
	}()

	select {
	case sig := <-sigCh:
		cancel()
		<-errCh
		if sig == os.Interrupt {
			return exitError{code: 130}
		}
		return exitError{code: 143}
	case err := <-errCh:
		if errors.Is(err, context.Canceled) {
			return nil
		}
		return err
	}
}

func main() {
	rootCmd := &cobra.Command{
		Use:   "gh-pulse",
		Short: "Stream and capture GitHub webhook events from smee.io",
		Long: `gh-pulse connects to a smee.io channel and streams GitHub webhook events.

Events are emitted as JSON Lines (one event per line) so you can pipe them
into scripts, filters, and assertion checks. Use stream for live output or
capture to buffer events until an exit condition is met.`,
	}
	rootCmd.Version = version

	var streamURL string
	var events []string
	var successOn []string
	var failureOn []string
	var timeoutSeconds int
	var captureURL string
	var captureEvents []string
	var captureSuccessOn []string
	var captureFailureOn []string
	var captureTimeoutSeconds int
	streamCmd := &cobra.Command{
		Use:   "stream --url <smee-channel>",
		Short: "Stream GitHub webhooks as JSONL to stdout",
		Long: `Connect to a smee.io channel and output GitHub webhook events as JSONL.

Each webhook received is printed as a single JSON line to stdout.
Connection status and errors go to stderr.

Exit codes:
  0   - Success assertion matched (--success-on)
  1   - Failure assertion matched (--failure-on)
  124 - Timeout reached (--timeout)
  130 - Interrupted (Ctrl+C)`,
		Example: `  # Stream all events
  gh-pulse stream --url https://smee.io/my-channel

  # Wait for push, exit 0 when received
  gh-pulse stream --url https://smee.io/my-channel --success-on "event=push" --timeout 60

  # Filter to only pull_request events
  gh-pulse stream --url https://smee.io/my-channel --event pull_request`,
		RunE: func(cmd *cobra.Command, args []string) error {
			successAssertions, err := assertion.ParseAssertions(successOn, 0)
			if err != nil {
				return err
			}
			failureAssertions, err := assertion.ParseAssertions(failureOn, 1)
			if err != nil {
				return err
			}
			timeout := time.Duration(timeoutSeconds) * time.Second

			return runWithSignals(func(ctx context.Context) error {
				err := client.Run(ctx, client.Config{
					URL:               streamURL,
					Events:            events,
					SuccessAssertions: successAssertions,
					FailureAssertions: failureAssertions,
					Timeout:           timeout,
				})
				if errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			})
		},
	}
	streamCmd.Flags().StringVar(&streamURL, "url", "", "smee.io channel URL (required)")
	streamCmd.Flags().StringArrayVar(&events, "event", nil, "filter by GitHub event type (can repeat)")
	streamCmd.Flags().StringArrayVar(&successOn, "success-on", nil, "exit 0 when JSON path matches (e.g., 'event=push')")
	streamCmd.Flags().StringArrayVar(&failureOn, "failure-on", nil, "exit 1 when JSON path matches")
	streamCmd.Flags().IntVar(&timeoutSeconds, "timeout", 0, "exit 124 after N seconds (0 = no timeout)")
	_ = streamCmd.MarkFlagRequired("url")

	captureCmd := &cobra.Command{
		Use:   "capture --url <smee-channel>",
		Short: "Buffer GitHub webhooks and dump on exit",
		Long: `Connect to a smee.io channel and buffer GitHub webhook events.

When an exit condition is met, all buffered events are printed as JSONL to stdout.
Connection status and errors go to stderr.

Exit codes:
  0   - Success assertion matched (--success-on)
  1   - Failure assertion matched (--failure-on)
  124 - Timeout reached (--timeout)
  130 - Interrupted (Ctrl+C)`,
		Example: `  # Capture until a push event arrives
  gh-pulse capture --url https://smee.io/my-channel --success-on "event=push" --timeout 60

  # Capture pull_request events for 10 seconds
  gh-pulse capture --url https://smee.io/my-channel --event pull_request --timeout 10

  # Fail when a workflow_run event is received
  gh-pulse capture --url https://smee.io/my-channel --failure-on "event=workflow_run" --timeout 120`,
		RunE: func(cmd *cobra.Command, args []string) error {
			if len(captureSuccessOn) == 0 && len(captureFailureOn) == 0 && captureTimeoutSeconds == 0 {
				return fmt.Errorf("capture mode requires at least one exit condition (--success-on, --failure-on, or --timeout)")
			}
			successAssertions, err := assertion.ParseAssertions(captureSuccessOn, 0)
			if err != nil {
				return err
			}
			failureAssertions, err := assertion.ParseAssertions(captureFailureOn, 1)
			if err != nil {
				return err
			}
			timeout := time.Duration(captureTimeoutSeconds) * time.Second

			return runWithSignals(func(ctx context.Context) error {
				err := client.RunCapture(ctx, client.Config{
					URL:               captureURL,
					Events:            captureEvents,
					SuccessAssertions: successAssertions,
					FailureAssertions: failureAssertions,
					Timeout:           timeout,
				})
				if errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			})
		},
	}
	captureCmd.Flags().StringVar(&captureURL, "url", "", "smee.io channel URL (required)")
	captureCmd.Flags().StringArrayVar(&captureEvents, "event", nil, "filter by GitHub event type (can repeat)")
	captureCmd.Flags().StringArrayVar(&captureSuccessOn, "success-on", nil, "exit 0 when JSON path matches (e.g., 'event=push')")
	captureCmd.Flags().StringArrayVar(&captureFailureOn, "failure-on", nil, "exit 1 when JSON path matches")
	captureCmd.Flags().IntVar(&captureTimeoutSeconds, "timeout", 0, "exit 124 after N seconds (0 = no timeout)")
	_ = captureCmd.MarkFlagRequired("url")

	rootCmd.AddCommand(streamCmd, captureCmd)

	if err := rootCmd.Execute(); err != nil {
		var exitErr interface{ ExitCode() int }
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}
