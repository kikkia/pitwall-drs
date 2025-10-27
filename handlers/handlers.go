package handlers

import (
	"encoding/json"
	"f1sockets/broadcaster"
	"f1sockets/f1tvclient"
	"f1sockets/model"
	"fmt"
	"strings"
)

type MessageHandlers struct {
	GlobalState            **model.GlobalState
	CustomEventBroadcaster model.CustomEventBroadcaster
	BrowserBroadcaster     *broadcaster.Broadcaster
	F1TVClient             *f1tvclient.F1TVClient
	SkippedFeedUpdates     *int
}

func NewMessageHandlers(gs **model.GlobalState, ceb model.CustomEventBroadcaster, bb *broadcaster.Broadcaster, f1c *f1tvclient.F1TVClient, sfu *int) *MessageHandlers {
	return &MessageHandlers{
		GlobalState:            gs,
		CustomEventBroadcaster: ceb,
		BrowserBroadcaster:     bb,
		F1TVClient:             f1c,
		SkippedFeedUpdates:     sfu,
	}
}

func (h *MessageHandlers) HandleNewStreamMessage(message []byte) {
	h.BrowserBroadcaster.Broadcast(message)

	var rMessage struct {
		R json.RawMessage `json:"R"`
	}
	if json.Unmarshal(message, &rMessage) == nil && rMessage.R != nil {
		var err error
		*h.GlobalState, err = model.NewGlobalState(message, h.CustomEventBroadcaster, *h.GlobalState)
		if err != nil {
			fmt.Printf("Failed to parse global state message: %v\n", err)
		} else {
			*h.SkippedFeedUpdates = 0
			fmt.Println("Global state successfully initialized.")
		}
		return
	}

	var feedArgs []json.RawMessage
	if json.Unmarshal(message, &feedArgs) == nil {
		if len(feedArgs) >= 2 {
			if *h.GlobalState != nil {
				var topic string
				if err := json.Unmarshal(feedArgs[0], &topic); err != nil {
					fmt.Printf("Failed to unmarshal feed topic: %v\n", err)
					return
				}

				var payload interface{}
				if err := json.Unmarshal(feedArgs[1], &payload); err != nil {
					fmt.Printf("Failed to unmarshal feed payload: %v\n", err)
					return
				}

				err := (*h.GlobalState).ApplyFeedUpdate([]interface{}{topic, payload})
				if err != nil {
					// fmt.Printf("Failed to apply feed update for topic %s: %v\n", topic, err)
				}
				*h.SkippedFeedUpdates = 0
			} else {
				*h.SkippedFeedUpdates++
				if *h.SkippedFeedUpdates%10 == 0 {
					fmt.Printf("Skipping feed update, global state not yet initialized (%d skipped).\n", *h.SkippedFeedUpdates)
				}
				if *h.SkippedFeedUpdates >= 50 {
					fmt.Println("50 consecutive feed updates skipped, restarting F1TV client.")
					h.F1TVClient.ForceReconnect()
					*h.SkippedFeedUpdates = 0
				}
			}
		}
		return
	}

	fmt.Printf("Unknown message format received: %s\n", string(message))
}

func (h *MessageHandlers) HandleLegacyStreamMessage(message []byte) {
	h.BrowserBroadcaster.Broadcast(message)

	var msgData struct {
		R json.RawMessage   `json:"R"`
		M []json.RawMessage `json:"M"`
	}

	if err := json.Unmarshal(message, &msgData); err != nil {
		if !json.Valid(message) || !strings.Contains(string(message), "\"M\":[]") {
			fmt.Printf("Unknown legacy message format received: %s\n", string(message))
		}
		return
	}

	// Process initial state if it exists
	if msgData.R != nil {
		var err error
		// We need to re-marshal the message with only the 'R' field for NewGlobalState
		initialStateMsg, _ := json.Marshal(map[string]json.RawMessage{"R": msgData.R})
		*h.GlobalState, err = model.NewGlobalState(initialStateMsg, h.CustomEventBroadcaster, *h.GlobalState)
		if err != nil {
			fmt.Printf("Failed to parse global state message from legacy stream: %v\n", err)
		} else {
			*h.SkippedFeedUpdates = 0
			fmt.Println("Global state successfully initialized from legacy stream.")
		}
	}

	// Process feed data if it exists
	if len(msgData.M) > 0 {
		h.handleLegacyData(msgData.M)
	}
}

func (h *MessageHandlers) handleLegacyData(messages []json.RawMessage) {
	for _, msg := range messages {
		var messageData struct {
			H string
			M string
			A []json.RawMessage
		}
		if err := json.Unmarshal(msg, &messageData); err != nil {
			fmt.Printf("Error unmarshalling legacy message data: %v\n", err)
			continue
		}

		if messageData.H == "Streaming" && messageData.M == "feed" && len(messageData.A) > 0 {
			var feedArgs []json.RawMessage
			if err := json.Unmarshal(messageData.A[0], &feedArgs); err == nil {
				if len(feedArgs) >= 2 {
					if *h.GlobalState != nil {
						var topic string
						if err := json.Unmarshal(feedArgs[0], &topic); err != nil {
							fmt.Printf("Failed to unmarshal feed topic from legacy message: %v\n", err)
							return
						}

						var payload interface{}
						if err := json.Unmarshal(feedArgs[1], &payload); err != nil {
							fmt.Printf("Failed to unmarshal feed payload from legacy message: %v\n", err)
							return
						}

						err := (*h.GlobalState).ApplyFeedUpdate([]interface{}{topic, payload})
						if err != nil {
							// fmt.Printf("Failed to apply legacy feed update for topic %s: %v\n", topic, err)
						}
					}
				}
			}
		}
	}
}
