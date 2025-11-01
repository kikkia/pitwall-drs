package f1tvclient

import (
	"bytes"
	"compress/zlib"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"sync"

	"f1sockets/auth"

	"github.com/gorilla/websocket"
	"github.com/philippseith/signalr"
)

const (
	oldStreamUrl   = "https://livetiming.formula1.com/signalr"
	signalRUrl     = "https://livetiming.formula1.com/signalrcore"
	clientProtocol = "1.5"
	hubName        = "Streaming"
)

type NegotiateResponse struct {
	ConnectionToken string `json:"ConnectionToken"`
}

type MessageHandler func([]byte)

type f1Receiver struct {
	signalr.Receiver
	client *F1TVClient
}

func (r *f1Receiver) Feed(rawTopic json.RawMessage, rawData json.RawMessage, rawTs json.RawMessage) {
	message := []json.RawMessage{rawTopic, rawData, rawTs}

	if r.client.newStreamHandler == nil {
		return
	}

	passThrough := func() {
		bytes, err := json.Marshal(message)
		if err == nil {
			r.client.newStreamHandler(bytes)
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

		reconstructedMessage := []json.RawMessage{
			message[0],
			json.RawMessage(decompressedData),
		}
		if len(message) > 2 {
			reconstructedMessage = append(reconstructedMessage, message[2:]...)
		}

		finalBytes, err := json.Marshal(reconstructedMessage)
		if err == nil {
			r.client.newStreamHandler(finalBytes)
		}
	} else {
		passThrough()
	}
}

type F1TVClient struct {
	conn                signalr.Client
	legacyConn          *websocket.Conn
	newStreamHandler    MessageHandler
	legacyStreamHandler MessageHandler
	stopChan            chan struct{}
	wg                  sync.WaitGroup
	isRunning           bool
	cancel              context.CancelFunc
}

func NewF1TVClient(newStreamHandler, legacyStreamHandler MessageHandler) *F1TVClient {
	return &F1TVClient{
		newStreamHandler:    newStreamHandler,
		legacyStreamHandler: legacyStreamHandler,
		stopChan:            make(chan struct{}),
	}
}

func (c *F1TVClient) Start() {
	if c.isRunning {
		return
	}
	c.isRunning = true

	c.wg.Add(1)
	go c.run()
}

func (c *F1TVClient) Stop() {
	if !c.isRunning {
		return
	}
	c.isRunning = false

	close(c.stopChan)
	if c.cancel != nil {
		c.cancel()
	}
	if c.legacyConn != nil {
		c.legacyConn.Close()
	}
	c.wg.Wait()
	fmt.Println("F1TV Client stopped.")
	c.stopChan = make(chan struct{})
}

func (c *F1TVClient) run() {
	defer c.wg.Done()

	token, err := auth.Authenticate()
	if err != nil {
		fmt.Printf("Authentication not available: %v. Falling back to legacy client.\n", err)
		c.runLegacy()
		return
	}
	c.runNewStream(token)
}

func (c *F1TVClient) IsRunning() bool {
	return c.isRunning
}

func (c *F1TVClient) ForceReconnect() {
	if c.conn != nil {
		fmt.Println("Forcing reconnection.")
		c.conn.Stop()
	} else if c.legacyConn != nil {
		fmt.Println("Forcing reconnection by closing the websocket.")
		c.legacyConn.Close()
	}
}
