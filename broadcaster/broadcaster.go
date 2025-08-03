package broadcaster

import (
	"f1sockets/metrics"
	"f1sockets/ratelimiter"
	"f1sockets/recorder"
	"log"
	"net/http"
	"time"

	"github.com/gorilla/websocket"
)

const (
	writeWait      = 10 * time.Second
	pongWait       = 60 * time.Second
	pingPeriod     = (pongWait * 9) / 10
	maxMessageSize = 512
)

// Client is a middleman between the websocket connection and the broadcaster.
type Client struct {
	broadcaster *Broadcaster
	conn        *websocket.Conn
	send        chan []byte
	ip          string
}

// Manages connected browser WebSocket clients and broadcasts messages.
type Broadcaster struct {
	clients           map[*Client]bool
	connectionLimiter *ratelimiter.ConnectionLimiter
	recorder          *recorder.Recorder
	broadcast         chan []byte
	register          chan *Client
	unregister        chan *Client
	upgrader          websocket.Upgrader
}

func NewBroadcaster(connectionLimiter *ratelimiter.ConnectionLimiter, recorder *recorder.Recorder) *Broadcaster {
	b := &Broadcaster{
		clients:           make(map[*Client]bool),
		connectionLimiter: connectionLimiter,
		recorder:          recorder,
		broadcast:         make(chan []byte, 256),
		register:          make(chan *Client),
		unregister:        make(chan *Client),
		upgrader: websocket.Upgrader{
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		},
	}
	go b.run()
	go b.startMetricsEmitter()
	return b
}

func (b *Broadcaster) run() {
	for {
		select {
		case client := <-b.register:
			b.clients[client] = true
			metrics.OpenConnection()
			log.Printf("client connected from %s, total clients: %d", client.ip, len(b.clients))
		case client := <-b.unregister:
			if _, ok := b.clients[client]; ok {
				b.connectionLimiter.RemoveConnection(client.ip)
				delete(b.clients, client)
				close(client.send)
				metrics.CloseConnection()
				log.Printf("client disconnected from %s, total clients: %d", client.ip, len(b.clients))
			}
		case message := <-b.broadcast:
			b.recorder.Record(message)
			for client := range b.clients {
				select {
				case client.send <- message:
				default:
					log.Printf("client %s send buffer full. disconnecting.", client.ip)
					close(client.send)
					delete(b.clients, client)
				}
			}
		}
	}
}

func (c *Client) readPump() {
	defer func() {
		c.broadcaster.unregister <- c
		c.conn.Close()
	}()
	c.conn.SetReadLimit(maxMessageSize)
	c.conn.SetReadDeadline(time.Now().Add(pongWait))
	c.conn.SetPongHandler(func(string) error { c.conn.SetReadDeadline(time.Now().Add(pongWait)); return nil })
	for {
		if _, _, err := c.conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("readPump error for client %s: %v", c.ip, err)
			}
			break
		}
	}
}

func (c *Client) writePump() {
	ticker := time.NewTicker(pingPeriod)
	defer func() {
		ticker.Stop()
		c.conn.Close()
	}()
	for {
		select {
		case message, ok := <-c.send:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if !ok {
				// The broadcaster closed the channel.
				c.conn.WriteMessage(websocket.CloseMessage, []byte{})
				return
			}
			if err := c.conn.WriteMessage(websocket.TextMessage, message); err != nil {
				log.Printf("writePump error for client %s: %v", c.ip, err)
				return
			}
		case <-ticker.C:
			c.conn.SetWriteDeadline(time.Now().Add(writeWait))
			if err := c.conn.WriteMessage(websocket.PingMessage, nil); err != nil {
				log.Printf("ping error for client %s: %v", c.ip, err)
				return
			}
		}
	}
}

// HandleConnections upgrades the HTTP server connection to a WebSocket connection
// and creates a new client.
func (b *Broadcaster) HandleConnections(w http.ResponseWriter, r *http.Request, initialMessage []byte) {
	ip := ratelimiter.GetClientIP(r)
	if !b.connectionLimiter.AddConnection(ip) {
		http.Error(w, "Too many connections from your IP", http.StatusTooManyRequests)
		return
	}

	conn, err := b.upgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade HTTP to WebSocket for ip %s: %v\n", ip, err)
		b.connectionLimiter.RemoveConnection(ip)
		return
	}

	client := &Client{broadcaster: b, conn: conn, send: make(chan []byte, 256), ip: ip}
	b.register <- client

	go client.writePump()
	go client.readPump()

	if initialMessage != nil {
		client.send <- initialMessage
	}
}

// Broadcast a given message to all connected clients
func (b *Broadcaster) Broadcast(message []byte) {
	b.broadcast <- message
}

// GetClientCount returns the number of currently connected WebSocket clients.
func (b *Broadcaster) GetClientCount() int {
	return len(b.clients)
}

// Emits the total number of connected clients to metrics every minute
func (b *Broadcaster) startMetricsEmitter() {
	ticker := time.NewTicker(1 * time.Minute)
	defer ticker.Stop()

	for range ticker.C {
		metrics.TotalConnections(len(b.clients))
	}
}
