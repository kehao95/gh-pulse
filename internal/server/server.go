package server

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"time"

	"github.com/gorilla/websocket"
	"github.com/kehao95/gh-pulse/internal/message"
)

type Config struct {
	Port int
}

func Run(ctx context.Context, cfg Config) error {
	logger := log.New(os.Stderr, "", log.LstdFlags)

	hub := newHub()
	go hub.run()

	mux := http.NewServeMux()
	mux.HandleFunc("/webhook", func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "failed to read body", http.StatusBadRequest)
			return
		}

		if ok, err := verifyWebhookSignature(body, r.Header.Get("X-Hub-Signature-256"), os.Getenv("GH_PULSE_WEBHOOK_SECRET"), logger); !ok {
			logger.Printf("webhook signature verification failed: %v", err)
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}

		event := r.Header.Get("X-GitHub-Event")
		delivery := r.Header.Get("X-GitHub-Delivery")
		payload, truncated, truncations, err := truncatePayloadIfNeeded(body)
		if err != nil {
			logger.Printf("payload truncation failed event=%q delivery=%q: %v", event, delivery, err)
			payload = json.RawMessage(body)
		}
		if truncated {
			logger.Printf("payload truncated event=%q delivery=%q bytes=%d fields=%s", event, delivery, len(body), truncationFields(truncations))
		}
		msg := message.EventMessage{
			Type:       "event",
			Event:      event,
			DeliveryID: delivery,
			Truncated:  truncated,
			Payload:    payload,
		}
		encoded, err := json.Marshal(msg)
		if err != nil {
			http.Error(w, "failed to encode message", http.StatusInternalServerError)
			return
		}

		select {
		case hub.broadcast <- broadcastMessage{event: event, data: encoded}:
		default:
			logger.Printf("broadcast dropped event=%q delivery=%q", event, delivery)
		}

		logger.Printf("webhook received event=%q delivery=%q bytes=%d", event, delivery, len(body))

		w.WriteHeader(http.StatusOK)
	})

	upgrader := websocket.Upgrader{
		CheckOrigin: func(r *http.Request) bool {
			return true
		},
	}

	mux.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			logger.Printf("ws upgrade failed: %v", err)
			return
		}
		logger.Printf("ws connected from %s", r.RemoteAddr)

		client := &Client{
			hub:    hub,
			conn:   conn,
			send:   make(chan []byte, 16),
			logger: logger,
		}
		hub.register <- client

		go client.writePump()
		client.readPump()

		logger.Printf("ws disconnected from %s", r.RemoteAddr)
	})

	srv := &http.Server{
		Addr:              fmt.Sprintf(":%d", cfg.Port),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
	}

	go func() {
		<-ctx.Done()
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = srv.Shutdown(shutdownCtx)
	}()

	logger.Printf("listening on :%d", cfg.Port)
	err := srv.ListenAndServe()
	if err == http.ErrServerClosed {
		return nil
	}
	return err
}
