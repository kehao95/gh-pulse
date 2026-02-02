package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/kehao95/gh-pulse/internal/assertion"
	"github.com/kehao95/gh-pulse/internal/client"
	"github.com/kehao95/gh-pulse/internal/server"
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
		Short: "Bridge GitHub webhooks to local CLI via WebSocket",
	}

	var port int
	serveCmd := &cobra.Command{
		Use:   "serve",
		Short: "Start the webhook server",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runWithSignals(func(ctx context.Context) error {
				err := server.Run(ctx, server.Config{Port: port})
				if errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			})
		},
	}
	serveCmd.Flags().IntVar(&port, "port", 8080, "Port to listen on")

	var serverURL string
	var events []string
	var successOn []string
	var failureOn []string
	streamCmd := &cobra.Command{
		Use:   "stream",
		Short: "Connect to the WebSocket stream",
		RunE: func(cmd *cobra.Command, args []string) error {
			successAssertions, err := assertion.ParseAssertions(successOn, 0)
			if err != nil {
				return err
			}
			failureAssertions, err := assertion.ParseAssertions(failureOn, 1)
			if err != nil {
				return err
			}

			return runWithSignals(func(ctx context.Context) error {
				err := client.Run(ctx, client.Config{
					ServerURL:         serverURL,
					Events:            events,
					SuccessAssertions: successAssertions,
					FailureAssertions: failureAssertions,
				})
				if errors.Is(err, context.Canceled) {
					return nil
				}
				return err
			})
		},
	}
	streamCmd.Flags().StringVar(&serverURL, "server", "ws://localhost:8080/ws", "WebSocket server URL")
	streamCmd.Flags().StringArrayVar(&events, "event", nil, "Subscribe to GitHub event types")
	streamCmd.Flags().StringArrayVar(&successOn, "success-on", nil, "Exit 0 when assertion matches")
	streamCmd.Flags().StringArrayVar(&failureOn, "failure-on", nil, "Exit 1 when assertion matches")

	rootCmd.AddCommand(serveCmd, streamCmd)

	if err := rootCmd.Execute(); err != nil {
		var exitErr interface{ ExitCode() int }
		if errors.As(err, &exitErr) {
			os.Exit(exitErr.ExitCode())
		}
		os.Exit(1)
	}
}
