package realtime

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestHubPerUserLimitAndRelease(t *testing.T) {
	h := NewHub()
	clients := make(chan *Client, 1)
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := (&websocket.Upgrader{}).Upgrade(w, r, nil)
		if err != nil {
			return
		}
		client := h.Add(conn, "123")
		if client == nil {
			conn.Close()
		}
		clients <- client
	}))
	defer s.Close()
	dial := func() *Client {
		t.Helper()
		conn, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(s.URL, "http"), nil)
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { conn.Close() })
		select {
		case c := <-clients:
			return c
		case <-time.After(time.Second):
			t.Fatal("connection timed out")
			return nil
		}
	}
	first := dial()
	if first == nil {
		t.Fatal("first rejected")
	}
	defer h.Remove(first)
	for i := 0; i < 2; i++ {
		c := dial()
		if c == nil {
			t.Fatal("early rejection")
		}
		defer h.Remove(c)
	}
	if c := dial(); c != nil {
		h.Remove(c)
		t.Fatal("fourth connection accepted")
	}
	if h.OnlineCount() != 1 {
		t.Fatal("online count should count unique users")
	}
	h.Remove(first)
	c := dial()
	if c == nil {
		t.Fatal("released slot unavailable")
	}
	defer h.Remove(c)
}
