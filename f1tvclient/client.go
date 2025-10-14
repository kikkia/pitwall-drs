package f1tvclient

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/json"
	"f1sockets/auth"
	"fmt"
	"io"
	"net/http"
	"sync"

	"github.com/go-kit/log"
	"github.com/philippseith/signalr"
)

const (
	f1tvBaseURL = "https://livetiming.formula1.com/signalrcore"
)

type MessageHandler func([]byte)

// f1Receiver implements the signalr.Receiver interface
type f1Receiver struct {
	signalr.Receiver
	client *F1TVClient
}

// Feed is the callback for the "feed" hub method from the SignalR stream.
func (r *f1Receiver) Feed(message []json.RawMessage) {
	if r.client.messageHandler == nil {
		return
	}

	// Default behavior: pass the message through as is.
	passThrough := func() {
		bytes, err := json.Marshal(message)
		if err == nil {
			r.client.messageHandler(bytes)
		}
	}

	if len(message) < 2 {
		passThrough()
		return
	}

	var topic string
	if err := json.Unmarshal(message[0], &topic); err != nil {
		passThrough()
		return
	}

	if len(topic) > 2 && topic[len(topic)-2:] == ".z" {
		// Compressed message
		var encodedData string
		if err := json.Unmarshal(message[1], &encodedData); err != nil {
			passThrough()
			return
		}

		decodedData, err := base64.StdEncoding.DecodeString(encodedData)
		if err != nil {
			passThrough()
			return
		}

		b := bytes.NewReader(decodedData)
		z, err := zlib.NewReader(b)
		if err != nil {
			passThrough()
			return
		}
		defer z.Close()

		decompressedData, err := io.ReadAll(z)
		if err != nil {
			passThrough()
			return
		}

		// Reconstruct the message with the decompressed data.
		reconstructedMessage := []json.RawMessage{
			message[0], // The topic, e.g., `"CarData.z"`
			json.RawMessage(decompressedData),
		}
		if len(message) > 2 {
			reconstructedMessage = append(reconstructedMessage, message[2:]...)
		}

		finalBytes, err := json.Marshal(reconstructedMessage)
		if err == nil {
			r.client.messageHandler(finalBytes)
		}
	} else {
		// Not a compressed message
		passThrough()
	}
}

type F1TVClient struct {
	conn           signalr.Client
	messageHandler MessageHandler
	stopChan       chan struct{}
	wg             sync.WaitGroup
	isRunning      bool
	cancel         context.CancelFunc
}

func NewF1TVClient(handler MessageHandler) *F1TVClient {
	return &F1TVClient{
		messageHandler: handler,
		stopChan:       make(chan struct{}),
	}
}

func (c *F1TVClient) Start() {
	if c.isRunning {
		return // Already running
	}
	c.isRunning = true

	c.wg.Add(1)
	go c.run()
}

func (c *F1TVClient) Stop() {
	if !c.isRunning {
		return // Not running
	}
	c.isRunning = false

	// closing stopChan will stop the run loop
	close(c.stopChan)
	// this will stop the signalr client and its connection
	if c.cancel != nil {
		c.cancel()
	}
	c.wg.Wait()
	fmt.Println("F1TV Client stopped.")
	c.stopChan = make(chan struct{}) // Re-initialize stopChan for future starts
}

func (c *F1TVClient) run() {
	defer c.wg.Done()

	var clientCtx context.Context
	clientCtx, c.cancel = context.WithCancel(context.Background())

	token, err := auth.Authenticate()
	if err != nil {
		fmt.Printf("Authentication failed: %v. Client will not run.\n", err)
		return
	}

	headers := func() http.Header {
		h := http.Header{}
		h.Add("Authorization", "Bearer "+token)
		return h
	}

	client, err := signalr.NewClient(clientCtx,
		signalr.WithHttpConnection(clientCtx, f1tvBaseURL, signalr.WithHTTPHeaders(headers)),
		signalr.WithReceiver(&f1Receiver{client: c}),
		signalr.MaximumReceiveMessageSize(2*1024*1024), // 2MB
		signalr.Logger(log.NewNopLogger(), false),
	)
	if err != nil {
		fmt.Printf("Error creating SignalR client: %v. Client will not run.\n", err)
		return
	}
	c.conn = client

	stateChan := make(chan signalr.ClientState, 1)
	cancelObserve := c.conn.ObserveStateChanged(stateChan)
	defer cancelObserve()

	c.conn.Start()

	for {
		select {
		case <-c.stopChan:
			return
		case state := <-stateChan:
			switch state {
			case signalr.ClientConnected:
				fmt.Println("Connected to F1TV SignalR.")
				c.subscribeToTopics()
			case signalr.ClientClosed:
				fmt.Println("F1TV connection lost. Attempting to reconnect...")
			}
		}
	}
}

func (c *F1TVClient) IsRunning() bool {
	return c.isRunning
}

func (c *F1TVClient) ForceReconnect() {
	if c.conn != nil {
		fmt.Println("Forcing reconnection.")
		c.conn.Stop()
	}
}

func (c *F1TVClient) subscribeToTopics() {
	topics := []string{
		"Heartbeat",
		"CarData.z",
		"Position.z",
		"ExtrapolatedClock",
		"TopThree",
		"TimingStats",
		"TimingAppData",
		"WeatherData",
		"TrackStatus",
		"DriverList",
		"RaceControlMessages",
		"SessionInfo",
		"SessionData",
		"LapCount",
		"TimingData",
	}

	go func() {
		result := <-c.conn.Invoke("Subscribe", topics)
		if result.Error != nil {
			fmt.Printf("Failed to send subscribe message: %v\n", result.Error)
		} else {
			fmt.Println("Successfully subscribed to F1TV topics.")
			// The result of subscribe is the initial state.
			// We need to wrap it in a format that NewGlobalState expects.
			wrappedMessage, err := json.Marshal(map[string]interface{}{"R": result.Value})
			if err != nil {
				fmt.Printf("Error wrapping subscription result for global state: %v\n", err)
				return
			}
			if c.messageHandler != nil {
				c.messageHandler(wrappedMessage)
			}
		}
	}()
}
