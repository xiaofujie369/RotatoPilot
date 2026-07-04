package realtime

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"github.com/coder/websocket"
)

type Hub struct {
	mu      sync.Mutex
	clients map[*websocket.Conn]struct{}
}

func New() *Hub                      { return &Hub{clients: map[*websocket.Conn]struct{}{}} }
func (h *Hub) Add(c *websocket.Conn) { h.mu.Lock(); h.clients[c] = struct{}{}; h.mu.Unlock() }
func (h *Hub) Remove(c *websocket.Conn) {
	h.mu.Lock()
	delete(h.clients, c)
	h.mu.Unlock()
	_ = c.Close(websocket.StatusNormalClosure, "")
}
func (h *Hub) Broadcast(event string, data any) {
	b, _ := json.Marshal(map[string]any{"event": event, "data": data, "timestamp": time.Now().UTC()})
	h.mu.Lock()
	clients := make([]*websocket.Conn, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.Unlock()
	for _, c := range clients {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		err := c.Write(ctx, websocket.MessageText, b)
		cancel()
		if err != nil {
			h.Remove(c)
		}
	}
}
