package realtime

import (
	"encoding/json"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	clientSendBuffer = 256
	writeWait        = 10 * time.Second
	pongWait         = 60 * time.Second
	pingPeriod       = (pongWait * 9) / 10
)

type Hub struct {
	mu      sync.RWMutex
	clients map[*Client]struct{}
}

type Client struct {
	Conn *websocket.Conn
	UserID string
	send chan []byte
	done chan struct{}
	once sync.Once
}

func NewHub() *Hub { return &Hub{clients: make(map[*Client]struct{})} }

func (h *Hub) Add(conn *websocket.Conn, userID string) *Client {
	client := &Client{Conn: conn, UserID: userID, send: make(chan []byte, clientSendBuffer), done: make(chan struct{})}
	h.mu.Lock()
	h.clients[client] = struct{}{}
	h.mu.Unlock()
	go client.writePump(func() { h.Remove(client) })
	return client
}

func (h *Hub) OnlineCount() int64 {
	h.mu.RLock()
	users := make(map[string]struct{}, len(h.clients))
	for client := range h.clients {
		users[client.UserID] = struct{}{}
	}
	h.mu.RUnlock()
	return int64(len(users))
}

func (h *Hub) Remove(client *Client) {
	client.once.Do(func() {
		h.mu.Lock()
		delete(h.clients, client)
		h.mu.Unlock()
		close(client.done)
		_ = client.Conn.Close()
	})
}

func (c *Client) SendJSON(value any) error {
	payload, err := json.Marshal(value)
	if err != nil {
		return err
	}
	select {
	case c.send <- payload:
		return nil
	case <-c.done:
		return websocket.ErrCloseSent
	default:
		return websocket.ErrCloseSent
	}
}

func (h *Hub) Broadcast(payload []byte) {
	h.mu.RLock()
	clients := make([]*Client, 0, len(h.clients))
	for client := range h.clients {
		clients = append(clients, client)
	}
	h.mu.RUnlock()
	for _, client := range clients {
		select {
		case client.send <- payload:
		default:
			// Disconnect slow consumers instead of allowing them to grow memory
			// without bound and block the realtime fan-out path.
			h.Remove(client)
		}
	}
}

func (c *Client) writePump(remove func()) {
	ticker := time.NewTicker(pingPeriod)
	defer func() { ticker.Stop(); remove() }()
	for {
		select {
		case payload, ok := <-c.send:
			if !ok {
				return
			}
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.TextMessage, payload); err != nil {
				return
			}
		case <-ticker.C:
			_ = c.Conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.Conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				return
			}
		case <-c.done:
			return
		}
	}
}
