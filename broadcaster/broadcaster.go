package broadcaster

import (
	"f1sockets/metrics"
	"f1sockets/ratelimiter"
	"fmt"
	"net/http"
	"sync"

	"github.com/gorilla/websocket"
)

// Manages connected browser WebSocket clients and broadcasts messages.
type Broadcaster struct {
	clients           map[*websocket.Conn]bool
	connectionLimiter *ratelimiter.ConnectionLimiter
	sync.RWMutex
	upgrader websocket.Upgrader
}

func NewBroadcaster(connectionLimiter *ratelimiter.ConnectionLimiter) *Broadcaster {
	return &Broadcaster{
		clients:           make(map[*websocket.Conn]bool),
		connectionLimiter: connectionLimiter,
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				// atm allow from all origins
				return true
			},
		},
	}
}

// Handles the initial websocket request and then the connection
func (b *Broadcaster) HandleConnections(w http.ResponseWriter, r *http.Request, initialMessage []byte) {
	ip := ratelimiter.GetClientIP(r)
	if !b.connectionLimiter.AddConnection(ip) {
		http.Error(w, "Too many connections from your IP", http.StatusTooManyRequests)
		return
	}
	defer b.connectionLimiter.RemoveConnection(ip)

	conn, err := b.upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Printf("Failed to upgrade HTTP to WebSocket: %v\n", err)
		return
	}
	defer conn.Close()

	// Send initial state to the new client
	if initialMessage != nil {
		err = conn.WriteMessage(websocket.TextMessage, initialMessage)
		if err != nil {
			fmt.Printf("Error sending initial state to browser client %s: %v\n", conn.RemoteAddr(), err)
		}
	}

	b.Lock()
	b.clients[conn] = true
	b.Unlock()
	metrics.IncConnections()

	fmt.Printf("Browser client connected: %s. Total clients: %d\n", conn.RemoteAddr(), len(b.clients))

	// Keep the connection open until the client disconnects
	for {
		// We don't expect messages from the client, but ReadMessage blocks
		// and will return an error if the client disconnects.
		_, _, err := conn.ReadMessage()
		if err != nil {
			// fmt.Printf("Browser client disconnected: %s. Error: %v\n", conn.RemoteAddr(), err) // Log in calling code
			break
		}
	}

	b.Lock()
	delete(b.clients, conn)
	b.Unlock()
	metrics.DecConnections()
	fmt.Printf("Browser client removed: %s. Total clients: %d\n", conn.RemoteAddr(), len(b.clients))
}

// Broadcast a given message to all connected clients
func (b *Broadcaster) Broadcast(message []byte) {
	b.RLock()
	defer b.RUnlock()

	for client := range b.clients {
		err := client.WriteMessage(websocket.TextMessage, message)
		if err != nil {
			fmt.Printf("Error sending message to browser client %s: %v\n", client.RemoteAddr(), err)
			// Consider adding logic here to mark client for removal if WriteMessage consistently fails
		}
	}
}

// GetClientCount returns the number of currently connected WebSocket clients.
func (b *Broadcaster) GetClientCount() int {
	b.RLock()
	defer b.RUnlock()
	return len(b.clients)
}
