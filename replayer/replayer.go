package replayer

import (
	"bufio"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strings"
	"time"
)

const (
	timestampLayout = time.RFC3339
)

type RecordedMessage struct {
	Timestamp time.Time
	Payload   []byte
}

type Replayer struct {
	filePath             string
	speedFactor          float64
	waitForClient        bool
	timeFactor           float64
	onMessage            func([]byte)
	firstClientConnected chan struct{}
}

func NewReplayer(filePath string, speedFactor float64, waitForClient bool, onMessage func([]byte)) *Replayer {
	if speedFactor <= 0 {
		speedFactor = 1.0
	}
	return &Replayer{
		filePath:             filePath,
		speedFactor:          speedFactor,
		waitForClient:        waitForClient,
		timeFactor:           speedFactor,
		onMessage:            onMessage,
		firstClientConnected: make(chan struct{}),
	}
}

func (r *Replayer) NotifyFirstClient() {
	if r.waitForClient {
		select {
		case <-r.firstClientConnected:
			// channel already closed
		default:
			close(r.firstClientConnected)
		}
	}
}

// Example: [2026-03-14T02:47:49Z] ["CarData.z","7Zax....","2026-03-14T02:47:46.8509745Z"]
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

	if len(payloadStr) == 0 {
		return nil, fmt.Errorf("empty payload")
	}

	if !strings.HasPrefix(payloadStr, "{") && !strings.HasPrefix(payloadStr, "[") {
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

func (r *Replayer) Start() {
	log.Println("Replay logic started.")
	if r.waitForClient {
		log.Println("Waiting for first client...")
		<-r.firstClientConnected
		log.Printf("First client detected. Starting replay...")
		time.Sleep(2 * time.Second) 
	}

	log.Printf("Starting replay from file: %s", r.filePath)

	file, err := os.Open(r.filePath)
	if err != nil {
		log.Printf("ERROR: Failed to open recording file '%s': %v", r.filePath, err)
		return
	}
	defer file.Close()

	var messages []RecordedMessage
	scanner := bufio.NewScanner(file)
	maxLineBytes := 16 * 1024 * 1024
	buf := make([]byte, maxLineBytes)
	scanner.Buffer(buf, maxLineBytes)

	for scanner.Scan() {
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

	firstMsg.Payload = adjustFirstMessageClock(firstMsg.Payload, firstMsg.Timestamp)

	r.processAndEmitMessage(firstMsg.Payload)
	previousTimestamp = firstMsg.Timestamp

	totalMessages := len(messages)
	for i := 1; i < totalMessages; i++ {
		msg := messages[i]
		delay := msg.Timestamp.Sub(previousTimestamp)

		if delay < 0 {
			delay = 0
		}

		if delay > 0 {
			delay = time.Duration(float64(delay) / r.timeFactor)
			time.Sleep(delay)
		}

		r.processAndEmitMessage(msg.Payload)
		previousTimestamp = msg.Timestamp

		if (i+1)%500 == 0 || (i+1) == totalMessages {
			progress := float64(i+1) / float64(totalMessages) * 100
			log.Printf("Replay progress: %d/%d (%.2f%%)", i+1, totalMessages, progress)
		}
	}

	log.Println("Replay finished or stopped.")
}

func (r *Replayer) processAndEmitMessage(payload []byte) {
	var signalRMessage interface{}
	if err := json.Unmarshal(payload, &signalRMessage); err != nil {
		r.onMessage(payload)
		return
	}

	if updateExtrapolatedClock(signalRMessage) {
		if modifiedPayload, err := json.Marshal(signalRMessage); err == nil {
			payload = modifiedPayload
		}
	}

	r.onMessage(payload)
}

func adjustFirstMessageClock(payload []byte, timestamp time.Time) []byte {
	var signalRMessage map[string]interface{}
	if err := json.Unmarshal(payload, &signalRMessage); err != nil {
		return payload
	}

	rData, ok := signalRMessage["R"].(map[string]interface{})
	if !ok {
		return payload
	}

	ecData, ok := rData["ExtrapolatedClock"].(map[string]interface{})
	if !ok {
		return payload
	}

	utc, ok := ecData["Utc"].(string)
	if !ok {
		return payload
	}

	extrapolatedClockTime, err := time.Parse(time.RFC3339Nano, utc)
	if err != nil {
		extrapolatedClockTime, err = time.Parse(time.RFC3339, utc)
	}

	if err == nil {
		diff := extrapolatedClockTime.Sub(timestamp)
		newUtc := time.Now().Add(diff).UTC().Format(time.RFC3339Nano)
		log.Printf("Replacing R ExtrapolatedClock 'Utc' property with adjusted time: %s", newUtc)
		ecData["Utc"] = newUtc

		if modifiedPayload, err := json.Marshal(signalRMessage); err == nil {
			return modifiedPayload
		}
	}

	return payload
}

func updateExtrapolatedClock(signalRMessage interface{}) bool {
	msg, ok := signalRMessage.(map[string]interface{})
	if !ok {
		return false
	}

	mArray, ok := msg["M"].([]interface{})
	if !ok {
		return false
	}

	modified := false
	for _, msgInterface := range mArray {
		msgMap, ok := msgInterface.(map[string]interface{})
		if !ok {
			continue
		}
		if hub, _ := msgMap["H"].(string); hub != "Streaming" {
			continue
		}
		if method, _ := msgMap["M"].(string); method != "feed" {
			continue
		}
		args, ok := msgMap["A"].([]interface{})
		if !ok || len(args) < 2 {
			continue
		}
		if eventName, _ := args[0].(string); eventName != "ExtrapolatedClock" {
			continue
		}
		clockData, ok := args[1].(map[string]interface{})
		if !ok {
			continue
		}

		now := time.Now().UTC().Format(time.RFC3339Nano)
		clockData["Utc"] = now
		if len(args) > 2 {
			args[2] = now
		}
		modified = true
	}
	return modified
}
