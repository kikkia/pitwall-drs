package main

import (
	"bufio"
	"encoding/json"
	"flag"
	"fmt"
	"log"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"f1sockets/broadcaster"
	"f1sockets/model"
	"f1sockets/ratelimiter"
	"f1sockets/recorder"
)

const (
	timestampLayout            = time.RFC3339
	REPORTING_MESSAGE_INTERVAL = 100
)

var (
	replayListenAddr  string
	recordingFilePath string
	startDelay        time.Duration
	speedFactor       float64
	waitForClient     bool
	maxLineBytes      int
	timeFactor        float64 = 1.0

	globalState            *model.GlobalState
	browserBroadcaster     *broadcaster.Broadcaster
	customEventBroadcaster model.CustomEventBroadcaster

	firstClientConnected = make(chan struct{})
	once                 sync.Once

	PROFILE_MESSAGE_HANDLER = false

	messageHandlerTotalDuration time.Duration
	messageHandlerMessageCount  int
	messageHandlerMutex         sync.Mutex
)

type RecordedMessage struct {
	Timestamp time.Time
	Payload   []byte
}

func main() {
	flag.StringVar(&replayListenAddr, "addr", "0.0.0.0:8080", "HTTP listen address")
	flag.StringVar(&recordingFilePath, "file", "recordings/2026/Chinese_Grand_Prix/Sprint.txt", "Recording file path")
	flag.DurationVar(&startDelay, "start-delay", 2*time.Second, "Delay before replay starts after first client")
	flag.Float64Var(&speedFactor, "speed", 1.0, "Playback speed factor (e.g. 0.5=half speed, 2.0=double speed)")
	flag.BoolVar(&waitForClient, "wait-for-client", true, "Wait for first client connection before replay")
	flag.IntVar(&maxLineBytes, "max-line-bytes", 16*1024*1024, "Max line size for scanner buffer")
	flag.Parse()

	if speedFactor <= 0 {
		speedFactor = 1.0
	}
	timeFactor = speedFactor

	log.Printf("Starting F1 Replay Server on %s", replayListenAddr)
	log.Printf("Will replay events from: %s", recordingFilePath)

	connectionLimiter := ratelimiter.NewConnectionLimiter(100)

	// Recorder setup
	sessionRecorder := recorder.NewRecorder(2*time.Second, func() *model.GlobalState {
		return globalState
	}, false)
	sessionRecorder.Start()
	defer sessionRecorder.Stop()

	browserBroadcaster = broadcaster.NewBroadcaster(connectionLimiter, sessionRecorder)
	customEventBroadcaster = model.NewCustomEventBroadcaster(browserBroadcaster.Broadcast)
	globalState = model.NewEmptyGlobalState()
	globalState.Broadcaster = customEventBroadcaster

	go runReplayLogic()

	http.HandleFunc("/ws", handleReplayConnections)
	http.HandleFunc("/state", handleState)

	err := http.ListenAndServe(replayListenAddr, nil)
	if err != nil {
		log.Fatalf("Replay HTTP server failed: %v\n", err)
	}
}

func handleReplayConnections(w http.ResponseWriter, r *http.Request) {
	log.Printf("Handling connection")

	initialState, err := globalState.GetStateAsJSON()
	if err != nil {
		fmt.Printf("Error retrieving global state json for initial message: %v\n", err)
		initialState = nil
	}

	once.Do(func() {
		log.Println("First client connected, signaling replay start...")
		close(firstClientConnected)
	})

	browserBroadcaster.HandleConnections(w, r, initialState)
}

func handleState(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	state := globalState

	w.Header().Set("Content-Type", "application/json")
	// Ehh for local dev rn
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if state == nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"message": "Driver data not yet available"})
		return
	}

	err := json.NewEncoder(w).Encode(state)
	if err != nil {
		fmt.Printf("Error encoding driver list JSON: %v\n", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
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
	log.Println("Replay logic started.")
	if waitForClient {
		log.Println("Waiting for first client...")
		<-firstClientConnected
		log.Printf("First client detected. Waiting %s before starting replay...", startDelay)
		time.Sleep(startDelay)
	}
	log.Printf("Starting replay from file: %s", recordingFilePath)

	file, err := os.Open(recordingFilePath)
	if err != nil {
		log.Printf("ERROR: Failed to open recording file '%s': %v", recordingFilePath, err)
		return
	}
	defer file.Close()

	var messages []RecordedMessage
	scanner := bufio.NewScanner(file)
	// Increase the buffer size to handle long lines
	if maxLineBytes < 1024*1024 {
		maxLineBytes = 1024 * 1024
	}
	buf := make([]byte, maxLineBytes)
	scanner.Buffer(buf, maxLineBytes)
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

	var signalRMessage map[string]interface{}
	if err := json.Unmarshal(firstMsg.Payload, &signalRMessage); err == nil {
		if rData, ok := signalRMessage["R"].(map[string]interface{}); ok {
			if ecData, ok := rData["ExtrapolatedClock"].(map[string]interface{}); ok {
				if utc, ok := ecData["Utc"].(string); ok {
					extrapolatedClockTime, err := time.Parse(time.RFC3339Nano, utc)
					if err != nil {
						extrapolatedClockTime, err = time.Parse(time.RFC3339, utc)
					}

					if err == nil {
						diff := extrapolatedClockTime.Sub(firstMsg.Timestamp)
						newUtc := time.Now().Add(diff).UTC().Format(time.RFC3339Nano)
						log.Printf("Replacing R ExtrapolatedClock 'Utc' property with current time adjusted by diff: %s", newUtc)
						ecData["Utc"] = newUtc

						modifiedPayload, err := json.Marshal(signalRMessage)
						if err == nil {
							firstMsg.Payload = modifiedPayload
						}
					} else {
						log.Printf("Could not parse R ExtrapolatedClock time '%s': %v", utc, err)
					}
				}
			}
		}
	}

	processAndBroadcastMessage(firstMsg.Payload)
	previousTimestamp = firstMsg.Timestamp

	totalMessages := len(messages)
	for i := 1; i < totalMessages; i++ {
		msg := messages[i]
		delay := msg.Timestamp.Sub(previousTimestamp)

		if delay < 0 {
			log.Printf("Warning: Negative delay calculated between message %d and %d. Sending immediately.", i-1, i)
			delay = 0
		}

		if delay > 0 {
			delay = time.Duration(float64(delay) / timeFactor)
			time.Sleep(delay)
		}

		processAndBroadcastMessage(msg.Payload)
		previousTimestamp = msg.Timestamp

		// Log progress every 100 messages or on the last message
		if (i+1)%500 == 0 || (i+1) == totalMessages {
			progress := float64(i+1) / float64(totalMessages) * 100
			log.Printf("Replay progress: %d/%d (%.2f%%)", i+1, totalMessages, progress)
		}
	}

	log.Println("Replay finished or stopped.")
}

func processAndBroadcastMessage(payload []byte) {
	var start time.Time
	if PROFILE_MESSAGE_HANDLER {
		start = time.Now()
	}

	var signalRMessage interface{}
	if err := json.Unmarshal(payload, &signalRMessage); err != nil {
		fmt.Printf("Failed to parse received message as JSON during replay: %v\n", err)
		return
	}

	switch msg := signalRMessage.(type) {
	case map[string]interface{}:
		// Standard object message (e.g., {"R": ...} or {"M": ...})
		if _, ok := msg["R"].(map[string]interface{}); ok {
			newState, err := model.NewGlobalState(payload, customEventBroadcaster, globalState)
			if err != nil {
				fmt.Printf("Failed to parse global state message during replay: %v\n", err)
			} else {
				globalState = newState
			}
		}
		if mArray, ok := msg["M"].([]interface{}); ok {
			for _, msgInterface := range mArray {
				if msgMap, ok := msgInterface.(map[string]interface{}); ok {
					if hub, hubOk := msgMap["H"].(string); hubOk && hub == "Streaming" {
						if method, methodOk := msgMap["M"].(string); methodOk && method == "feed" {
							if args, argsOk := msgMap["A"].([]interface{}); argsOk {
								if len(args) > 0 {
									if eventName, isString := args[0].(string); isString && eventName == "ExtrapolatedClock" {
										// Update timestamp for live replay feel
										if len(args) > 1 {
											if clockData, ok := args[1].(map[string]interface{}); ok {
												now := time.Now().UTC().Format(time.RFC3339Nano)
												clockData["Utc"] = now
												log.Printf("Replaced ExtrapolatedClock 'Utc' property with current time: %s", now)
												if len(args) > 2 {
													args[2] = now
												}
											}
										}
									}
								}
								if globalState != nil {
									err := globalState.ApplyFeedUpdate(args)
									if err != nil {
										fmt.Printf("Failed to apply feed update during replay: %v\n Update Args: %v: %v\n", err, args[0], args)
									}
								}
							}
						}
					}
				}
			}
		}
		// Re-marshal the potentially modified message
		modifiedPayload, err := json.Marshal(msg)
		if err == nil {
			payload = modifiedPayload
		}
	case []interface{}:
		// It's an array, likely a raw feed update. We can try to process it.
		// This handles cases where the message is just the "A" part of a feed update.
		if globalState != nil {
			err := globalState.ApplyFeedUpdate(msg)
			if err != nil {
				fmt.Printf("Failed to apply array-based feed update during replay: %v\n", err)
			}
		}
	default:
		fmt.Printf("Warning: Unhandled message type in replay: %T\n", signalRMessage)
	}

	// Broadcast the message to all connected browser clients
	browserBroadcaster.Broadcast(payload)

	if PROFILE_MESSAGE_HANDLER {
		duration := time.Since(start)

		messageHandlerMutex.Lock()
		messageHandlerTotalDuration += duration
		messageHandlerMessageCount++

		if messageHandlerMessageCount >= REPORTING_MESSAGE_INTERVAL {
			avgDuration := messageHandlerTotalDuration / time.Duration(messageHandlerMessageCount)
			fmt.Printf("Message Handler Performance Report %d clients (%d messages): Total Time: %s, Average Time: %s\n",
				browserBroadcaster.GetClientCount(), messageHandlerMessageCount, messageHandlerTotalDuration, avgDuration)

			// Reset counters
			messageHandlerTotalDuration = 0
			messageHandlerMessageCount = 0
		}
		messageHandlerMutex.Unlock()
	}
}
