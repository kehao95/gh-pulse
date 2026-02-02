package server

import (
	"encoding/json"
	"log"
	"sync"

	"github.com/gorilla/websocket"
)

type Hub struct {
	clients    map[*Client]bool
	broadcast  chan broadcastMessage
	register   chan *Client
	unregister chan *Client
}

func newHub() *Hub {
	return &Hub{
		clients:    make(map[*Client]bool),
		broadcast:  make(chan broadcastMessage, 16),
		register:   make(chan *Client),
		unregister: make(chan *Client),
	}
}

func (h *Hub) run() {
	for {
		select {
		case client := <-h.register:
			h.clients[client] = true
		case client := <-h.unregister:
			if _, ok := h.clients[client]; ok {
				delete(h.clients, client)
				close(client.send)
			}
		case message := <-h.broadcast:
			for client := range h.clients {
				if !client.subscribedTo(message.event) {
					continue
				}
				select {
				case client.send <- message.data:
				default:
					delete(h.clients, client)
					close(client.send)
				}
			}
		}
	}
}

type broadcastMessage struct {
	event string
	data  []byte
}

type Client struct {
	hub      *Hub
	conn     *websocket.Conn
	send     chan []byte
	events   []string
	eventsMu sync.RWMutex
	logger   *log.Logger
}

func (c *Client) readPump() {
	defer func() {
		c.hub.unregister <- c
		_ = c.conn.Close()
	}()

	for {
		_, data, err := c.conn.ReadMessage()
		if err != nil {
			return
		}

		var msg subscribeMessage
		if err := json.Unmarshal(data, &msg); err != nil {
			continue
		}
		if msg.Type != "subscribe" {
			continue
		}
		c.setEvents(msg.Events)
		if c.logger != nil {
			c.logger.Printf("ws subscribed remote=%s events=%v", c.conn.RemoteAddr(), msg.Events)
		}
	}
}

func (c *Client) writePump() {
	defer func() {
		_ = c.conn.Close()
	}()

	for message := range c.send {
		if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
			return
		}
	}
}

type subscribeMessage struct {
	Type   string   `json:"type"`
	Events []string `json:"events"`
}

func (c *Client) setEvents(events []string) {
	c.eventsMu.Lock()
	if len(events) == 0 {
		c.events = nil
	} else {
		c.events = append([]string(nil), events...)
	}
	c.eventsMu.Unlock()
}

func (c *Client) subscribedTo(event string) bool {
	c.eventsMu.RLock()
	defer c.eventsMu.RUnlock()
	if len(c.events) == 0 {
		return true
	}
	for _, candidate := range c.events {
		if candidate == event {
			return true
		}
	}
	return false
}
