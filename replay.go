package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"f1sockets/model"

	"github.com/gorilla/websocket"
)

const (
	replayListenAddr  = "localhost:8080"
	recordingFilePath = "recordings/f1tv_events_spain_race.txt"
	startDelay        = 5 * time.Second

	timestampLayout = time.RFC3339
)

var (
	timeFactor  int64 = 5
	globalState       = model.NewEmptyGlobalState()
)

type RecordedMessage struct {
	Timestamp time.Time
	Payload   []byte
}

type ReplayClientManager struct {
	clients              map[*websocket.Conn]bool
	clientsMux           sync.RWMutex
	firstClientConnected chan struct{}
	once                 sync.Once
}

var replayManager = ReplayClientManager{
	clients:              make(map[*websocket.Conn]bool),
	firstClientConnected: make(chan struct{}),
}

var replayUpgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		return true
	},
}

func main() {
	log.Printf("Starting F1 Replay Server on %s", replayListenAddr)
	log.Printf("Will replay events from: %s", recordingFilePath)

	go runReplayLogic()

	http.HandleFunc("/ws", handleReplayConnections)

	err := http.ListenAndServe(replayListenAddr, nil)
	if err != nil {
		log.Fatalf("Replay HTTP server failed: %v\n", err)
	}
}

func handleReplayConnections(w http.ResponseWriter, r *http.Request) {
	log.Printf("Handling connection")
	conn, err := replayUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("Failed to upgrade replay connection: %v", err)
		return
	}
	defer conn.Close()

	var beginningState []byte
	beginningState, err = globalState.GetStateAsJSON()
	if err != nil {
		fmt.Printf("Error retrieving global state json for initial message %s: %v\n", conn.RemoteAddr(), err)
		return
	}

	err = conn.WriteMessage(1, beginningState)
	if err != nil {
		fmt.Printf("Error sending global state message to browser client %s: %v\n", conn.RemoteAddr(), err)
		return
	}

	replayManager.clientsMux.Lock()
	replayManager.clients[conn] = true
	isFirstClient := len(replayManager.clients) == 1
	replayManager.clientsMux.Unlock()

	log.Printf("Replay client connected: %s. Total clients: %d\n", conn.RemoteAddr(), len(replayManager.clients))

	if isFirstClient {
		replayManager.once.Do(func() {
			log.Println("First client connected, signaling replay start...")
			close(replayManager.firstClientConnected) // Close the channel to signal
		})
	}

	for {
		if _, _, err := conn.ReadMessage(); err != nil {
			if websocket.IsUnexpectedCloseError(err, websocket.CloseGoingAway, websocket.CloseAbnormalClosure) {
				log.Printf("Replay client read error: %v", err)
			} else {
				log.Printf("Replay client disconnected normally: %s", conn.RemoteAddr())
			}
			break
		}
	}

	replayManager.clientsMux.Lock()
	delete(replayManager.clients, conn)
	log.Printf("Replay client removed: %s. Total clients: %d\n", conn.RemoteAddr(), len(replayManager.clients))
	replayManager.clientsMux.Unlock()
}

// Parses a line from the recording file
// Example: [2025-04-21T01:17:26+09:00] {"C":"d-F0ECA...}
func parseLogLine(line string) (*RecordedMessage, error) {
	line = strings.TrimSpace(line)
	if len(line) == 0 || !strings.HasPrefix(line, "[") {
		return nil, fmt.Errorf("invalid line format: missing timestamp prefix")
	}

	endTimestampIndex := strings.Index(line, "]")
	if endTimestampIndex == -1 || endTimestampIndex+1 >= len(line) {
		return nil, fmt.Errorf("invalid line format: timestamp closing bracket not found")
	}

	timestampStr := line[1:endTimestampIndex]
	payloadStr := strings.TrimSpace(line[endTimestampIndex+1:])

	// Check if payload is just "{}" - still valid JSON, keep it. Check if it's empty.
	if len(payloadStr) == 0 {
		return nil, fmt.Errorf("empty payload")
	}

	if !strings.HasPrefix(payloadStr, "{") && !strings.HasPrefix(payloadStr, "[") {
		log.Printf("Skipping line with non-JSON payload: %s", payloadStr)
		return nil, fmt.Errorf("payload does not look like JSON")
	}

	timestamp, err := time.Parse(timestampLayout, timestampStr)
	if err != nil {
		return nil, fmt.Errorf("failed to parse timestamp '%s': %w", timestampStr, err)
	}

	return &RecordedMessage{
		Timestamp: timestamp,
		Payload:   []byte(payloadStr),
	}, nil
}

func runReplayLogic() {
	log.Println("Replay logic started, waiting for first client...")
	<-replayManager.firstClientConnected // Wait for the signal
	log.Printf("First client detected. Waiting %s before starting replay...", startDelay)
	time.Sleep(startDelay)
	log.Printf("Starting replay from file: %s", recordingFilePath)

	file, err := os.Open(recordingFilePath)
	if err != nil {
		log.Printf("ERROR: Failed to open recording file '%s': %v", recordingFilePath, err)
		return // Cannot proceed without the file
	}
	defer file.Close()

	var messages []RecordedMessage
	scanner := bufio.NewScanner(file)
	lineNum := 0
	for scanner.Scan() {
		lineNum++
		line := scanner.Text()
		msg, err := parseLogLine(line)
		if err != nil {
			continue
		}
		messages = append(messages, *msg)
	}

	if err := scanner.Err(); err != nil {
		log.Printf("ERROR reading recording file: %v", err)
	}

	if len(messages) == 0 {
		log.Println("No valid messages found in the recording file. Nothing to replay.")
		return
	}

	log.Printf("Parsed %d messages. Starting replay loop.", len(messages))

	var previousTimestamp time.Time
	firstMsg := messages[0]
	log.Printf("Sending first message (Timestamp: %s)", firstMsg.Timestamp.Format(time.RFC3339))
	broadcastMessage(firstMsg.Payload)
	previousTimestamp = firstMsg.Timestamp

	raceStart, err := time.Parse(timestampLayout, "2025-06-01T22:03:36+09:00")
	if err != nil {
		log.Printf("REEEEE %s", err)
		return
	}

	for i := 1; i < len(messages); i++ {
		msg := messages[i]
		delay := msg.Timestamp.Sub(previousTimestamp)

		if msg.Timestamp.Equal(raceStart) {
			timeFactor = 1
			log.Printf("Race started, shifting timeFactor back to 1")
		}

		if delay < 0 {
			log.Printf("Warning: Negative delay calculated between message %d and %d. Sending immediately.", i-1, i)
			delay = 0
		}

		replayManager.clientsMux.RLock()
		replayManager.clientsMux.RUnlock()

		if delay > 0 {
			delay = time.Duration((1000 / timeFactor)) * time.Millisecond
			time.Sleep(delay)
		}

		broadcastMessage(msg.Payload)
		previousTimestamp = msg.Timestamp
	}

	log.Println("Replay finished or stopped.")
}

func broadcastMessage(payload []byte) {
	replayManager.clientsMux.RLock()
	defer replayManager.clientsMux.RUnlock()

	applyGlobalState(payload)

	if len(replayManager.clients) == 0 {
		return
	}

	for client := range replayManager.clients {
		err := client.WriteMessage(websocket.TextMessage, payload)
		if err != nil {
			log.Printf("Error sending message to client %s: %v. Will remove on next cycle.", client.RemoteAddr(), err)
		}
	}
}

func applyGlobalState(payload []byte) {
	var signalRMessage map[string]interface{}
	if err := json.Unmarshal(payload, &signalRMessage); err == nil {
		if _, ok := signalRMessage["R"].(map[string]interface{}); ok {
			globalState, err = model.NewGlobalState(payload)
			if err != nil {
				fmt.Printf("Failed to parse global state message: %v\n", err)
			}
		}
		if mArray, ok := signalRMessage["M"].([]interface{}); ok {
			for _, msgInterface := range mArray {
				if msgMap, ok := msgInterface.(map[string]interface{}); ok {
					if hub, hubOk := msgMap["H"].(string); hubOk && hub == "Streaming" {
						if method, methodOk := msgMap["M"].(string); methodOk && method == "feed" {
							if args, argsOk := msgMap["A"].([]interface{}); argsOk {
								if globalState != nil {
									err := globalState.ApplyFeedUpdate(args)
									if err != nil {
										fmt.Printf("Failed to apply feed update: %v\n Update Args: %v\n", err, args)
									}
								} else {
									fmt.Println("Skipping feed update as global state is not yet initialized.")
								}
							}
						}
					}
				}
			}
		}
	} else {
		fmt.Printf("Failed to parse received message as JSON: %v\n", err)
	}
}
