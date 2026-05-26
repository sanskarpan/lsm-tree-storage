// Package gateway — see rest.go for the package doc.
package gateway

import (
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"lsm-engine/internal/events"
)

const (
	wsWriteWait      = 10 * time.Second
	wsPongWait       = 60 * time.Second
	wsPingPeriod     = (wsPongWait * 9) / 10
	wsMaxMessageSize = 1 << 20
)

// HubOptions configures WebSocket auth and origin policy.
type HubOptions struct {
	AllowedOrigins []string
	APIToken       string
}

// WSHub manages all WebSocket connections and fans out events
type WSHub struct {
	mu             sync.RWMutex
	clients        map[*wsClient]struct{}
	allowedOrigins []string
	apiToken       string
}

type wsClient struct {
	conn *websocket.Conn
	send chan []byte
}

// NewWSHub creates a new WebSocket hub.
func NewWSHub(bus *events.EventBus, opts HubOptions) *WSHub {
	hub := &WSHub{
		clients:        make(map[*wsClient]struct{}),
		allowedOrigins: append([]string(nil), opts.AllowedOrigins...),
		apiToken:       opts.APIToken,
	}
	// Subscribe to all events
	if bus != nil {
		bus.SubscribeAll(func(evt events.Event) {
			data, err := json.Marshal(evt)
			if err != nil {
				return
			}
			hub.broadcast(data)
		})
	}
	return hub
}

func (h *WSHub) broadcast(data []byte) {
	h.mu.RLock()
	clients := make([]*wsClient, 0, len(h.clients))
	for c := range h.clients {
		clients = append(clients, c)
	}
	h.mu.RUnlock()

	for _, c := range clients {
		select {
		case c.send <- data:
		default:
			h.unregister(c)
			_ = c.conn.Close()
		}
	}
}

func (h *WSHub) unregister(client *wsClient) {
	h.mu.Lock()
	if _, ok := h.clients[client]; ok {
		delete(h.clients, client)
		close(client.send)
	}
	h.mu.Unlock()
}

// ClientCount returns the number of currently connected WebSocket clients.
func (h *WSHub) ClientCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return len(h.clients)
}

func (h *WSHub) authorize(r *http.Request) bool {
	if h.apiToken == "" {
		return true
	}
	if tokenAuthorized(h.apiToken, parseBearerToken(r.Header.Get("Authorization"))) {
		return true
	}
	if tokenAuthorized(h.apiToken, strings.TrimSpace(r.URL.Query().Get("access_token"))) {
		return true
	}
	return false
}

// ServeWS upgrades the HTTP connection to WebSocket and registers the client with the hub.
func (h *WSHub) ServeWS(w http.ResponseWriter, r *http.Request) {
	if !h.authorize(r) {
		w.Header().Set("WWW-Authenticate", `Bearer realm="lsm-engine"`)
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	upgrader := websocket.Upgrader{
		HandshakeTimeout: wsWriteWait,
		CheckOrigin: func(r *http.Request) bool {
			return originAllowed(r.Header.Get("Origin"), requestScheme(r), r.Host, h.allowedOrigins)
		},
	}
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	client := &wsClient{conn: conn, send: make(chan []byte, 256)}

	h.mu.Lock()
	h.clients[client] = struct{}{}
	h.mu.Unlock()

	go func() {
		ticker := time.NewTicker(wsPingPeriod)
		defer func() {
			ticker.Stop()
			h.unregister(client)
			_ = conn.Close()
		}()
		for {
			select {
			case data, ok := <-client.send:
				_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
				if !ok {
					_ = conn.WriteMessage(websocket.CloseMessage, []byte{})
					return
				}
				if err := conn.WriteMessage(websocket.TextMessage, data); err != nil {
					return
				}
			case <-ticker.C:
				_ = conn.SetWriteDeadline(time.Now().Add(wsWriteWait))
				if err := conn.WriteMessage(websocket.PingMessage, nil); err != nil {
					return
				}
			}
		}
	}()

	conn.SetReadLimit(wsMaxMessageSize)
	_ = conn.SetReadDeadline(time.Now().Add(wsPongWait))
	conn.SetPongHandler(func(string) error {
		return conn.SetReadDeadline(time.Now().Add(wsPongWait))
	})
	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			h.unregister(client)
			return
		}
	}
}
