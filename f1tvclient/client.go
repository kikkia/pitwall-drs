package f1tvclient

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"
)

const (
	f1tvBaseURL    = "https://livetiming.formula1.com/signalr"
	clientProtocol = "1.5"
	hubName        = "Streaming"
)

type NegotiateResponse struct {
	ConnectionToken string `json:"ConnectionToken"`
}

type MessageHandler func([]byte)

type F1TVClient struct {
	conn           *websocket.Conn
	messageHandler MessageHandler
	stopChan       chan struct{}
	wg             sync.WaitGroup
	isRunning      bool // In high concurrency maybe a mutex would be needed
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

	close(c.stopChan)
	if c.conn != nil {
		c.conn.Close()
	}
	c.wg.Wait()
	fmt.Println("F1TV Client stopped.")
	c.stopChan = make(chan struct{}) // Re-initialize stopChan for future starts
}

func (c *F1TVClient) run() {
	defer func() {
		c.isRunning = false
		c.wg.Done()
	}()

	for {
		select {
		case <-c.stopChan:
			return
		default:
			if !c.isRunning { // Check isRunning again in case Stop() was called while in select
				return
			}

			fmt.Println("Attempting to connect to F1TV SignalR...")
			token, cookie, err := negotiate()
			if err != nil {
				fmt.Printf("Negotiation failed: %v. Retrying in 5 seconds...\n", err)
				time.Sleep(5 * time.Second)
				continue
			}
			fmt.Println("Negotiation successful.")

			conn, err := connectF1TVWebSocket(token, cookie)
			if err != nil {
				fmt.Printf("F1TV WebSocket connection failed: %v. Retrying in 5 seconds...\n", err)
				time.Sleep(5 * time.Second)
				continue
			}
			c.conn = conn
			fmt.Println("Connected to F1TV WebSocket.")

			sendSubscribeMessage(c.conn)

			c.readMessages()

			fmt.Println("F1TV connection lost. Attempting to reconnect in 5 seconds...")
			c.conn = nil
			time.Sleep(5 * time.Second)
		}
	}
}

func (c *F1TVClient) IsRunning() bool {
	return c.isRunning
}

func (c *F1TVClient) readMessages() {
	defer func() {
		if c.conn != nil {
			c.conn.Close()
		}
	}()
	for {
		select {
		case <-c.stopChan:
			return
		default:
			_, message, err := c.conn.ReadMessage()
			if err != nil {
				return
			}

			if c.messageHandler != nil {
				c.messageHandler(message)
			}
		}
	}
}

func negotiate() (string, string, error) {
	connectionData := url.QueryEscape(fmt.Sprintf(`[{"name":"%s"}]`, hubName))
	negotiateURL := fmt.Sprintf("%s/negotiate?connectionData=%s&clientProtocol=%s", f1tvBaseURL, connectionData, clientProtocol)

	resp, err := http.Get(negotiateURL)
	if err != nil {
		return "", "", fmt.Errorf("http GET error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", "", fmt.Errorf("negotiate failed with status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	bodyBytes, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read negotiate response body: %w", err)
	}

	var negotiateResp NegotiateResponse
	err = json.Unmarshal(bodyBytes, &negotiateResp)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse negotiate response JSON: %w", err)
	}

	cookie := ""
	setCookieHeaders, ok := resp.Header["Set-Cookie"]
	if ok && len(setCookieHeaders) > 0 {
		// split off options like 'path' - simplified
		parts := strings.Split(setCookieHeaders[0], ";")
		cookie = parts[0]
	}

	if negotiateResp.ConnectionToken == "" {
		return "", "", fmt.Errorf("connection token is empty in negotiate response")
	}

	return negotiateResp.ConnectionToken, cookie, nil
}

func connectF1TVWebSocket(token, cookie string) (*websocket.Conn, error) {
	encodedToken := url.QueryEscape(token)
	connectionData := url.QueryEscape(fmt.Sprintf(`[{"name":"%s"}]`, hubName))
	wsURL := fmt.Sprintf("wss://livetiming.formula1.com/signalr/connect?clientProtocol=%s&transport=webSockets&connectionToken=%s&connectionData=%s", clientProtocol, encodedToken, connectionData)

	headers := http.Header{}
	headers.Add("User-Agent", "f1-testing")
	headers.Add("Accept-Encoding", "gzip,identity")
	if cookie != "" {
		headers.Add("Cookie", cookie)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		return nil, fmt.Errorf("websocket dial error: %w", err)
	}

	return conn, nil
}

func sendSubscribeMessage(conn *websocket.Conn) {

	topics := []string{
		"Heartbeat",
		"CarData.z",
		"Position.z",
		"CarData",
		"Position",
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
		"ChampionshipPrediction",
		"TeamRadio",
		"TyreStintSeries",
	}

	subscribeMessage := map[string]interface{}{
		"H": hubName,
		"M": "Subscribe",
		"A": []interface{}{topics},
		"I": 1,
	}

	message, err := json.Marshal(subscribeMessage)
	if err != nil {
		fmt.Printf("Failed to marshal subscribe message: %v\n", err)
		return
	}

	err = conn.WriteMessage(websocket.TextMessage, message)
	if err != nil {
		fmt.Printf("Failed to send subscribe message: %v\n", err)
	} else {
		fmt.Println("Subscribe message sent to F1TV.")
	}
}
