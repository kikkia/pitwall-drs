package main

import (
	"flag"
	"log"
	"net/url"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

var (
	numClients = flag.Int("clients", 1999, "number of concurrent WebSocket clients to connect")
	serverURL  = flag.String("url", "ws://localhost:8080/ws", "WebSocket server URL")
)

func main() {
	flag.Parse()

	log.Printf("Starting WebSocket load tester with %d clients connecting to %s", *numClients, *serverURL)

	var wg sync.WaitGroup
	wg.Add(*numClients)

	for i := 0; i < *numClients; i++ {
		go func(clientId int) {
			defer wg.Done()

			u, err := url.Parse(*serverURL)
			if err != nil {
				log.Printf("Client %d: Failed to parse URL: %v", clientId, err)
				return
			}

			log.Printf("Client %d: Connecting to %s", clientId, u.String())
			conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
			if err != nil {
				log.Printf("Client %d: Failed to connect: %v", clientId, err)
				return
			}
			defer conn.Close()

			log.Printf("Client %d: Connected successfully", clientId)

			// Keep reading messages until the connection is closed
			for {
				// We don't care about the message content, just read it
				_, _, err := conn.ReadMessage()
				if err != nil {
					if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
						log.Printf("Client %d: Connection closed unexpectedly: %v", clientId, err)
					} else {
						log.Printf("Client %d: Connection closed normally", clientId)
					}
					break // Exit the read loop on error/close
				}
				// Message read successfully, do nothing with it
			}
		}(i)

		// Add a small delay between launching clients to avoid thundering herd
		time.Sleep(10 * time.Millisecond)
	}

	// Wait for all client goroutines to finish.
	// In a real load test, you might want to run this for a fixed duration instead.
	wg.Wait()

	log.Println("All clients finished.")
}
