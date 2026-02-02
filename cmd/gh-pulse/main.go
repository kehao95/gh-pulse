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
		_ = <-errCh
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
		Short: "Stream GitHub webhooks to your CLI via SSE",
	}

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
		Use:   "stream",
		Short: "Connect to the smee.io SSE stream",
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
	streamCmd.Flags().StringVar(&streamURL, "url", "", "smee.io channel URL")
	streamCmd.Flags().StringArrayVar(&events, "event", nil, "Subscribe to GitHub event types")
	streamCmd.Flags().StringArrayVar(&successOn, "success-on", nil, "Exit 0 when assertion matches")
	streamCmd.Flags().StringArrayVar(&failureOn, "failure-on", nil, "Exit 1 when assertion matches")
	streamCmd.Flags().IntVar(&timeoutSeconds, "timeout", 0, "Exit 124 after timeout in seconds (0 = no timeout)")
	_ = streamCmd.MarkFlagRequired("url")

	captureCmd := &cobra.Command{
		Use:   "capture",
		Short: "Buffer events and dump on exit",
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
	captureCmd.Flags().StringVar(&captureURL, "url", "", "smee.io channel URL")
	captureCmd.Flags().StringArrayVar(&captureEvents, "event", nil, "Subscribe to GitHub event types")
	captureCmd.Flags().StringArrayVar(&captureSuccessOn, "success-on", nil, "Exit 0 when assertion matches")
	captureCmd.Flags().StringArrayVar(&captureFailureOn, "failure-on", nil, "Exit 1 when assertion matches")
	captureCmd.Flags().IntVar(&captureTimeoutSeconds, "timeout", 0, "Exit 124 after timeout in seconds (0 = no timeout)")
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
