package model

import (
	"encoding/json"
	"fmt"
)

// CustomEventBroadcaster defines an interface for broadcasting custom game events.
type CustomEventBroadcaster interface {
	BroadcastLapHistory(driverNum string, completedLap CompletedLap)
	BroadcastDriverListUpdates(drivers map[string]DriverInfo)
}

type CustomEventBroadcasterImpl struct {
	broadcastFunc func([]byte)
}

// NewCustomEventBroadcaster creates a new CustomEventBroadcasterImpl with the given broadcast function.
func NewCustomEventBroadcaster(f func([]byte)) *CustomEventBroadcasterImpl {
	return &CustomEventBroadcasterImpl{broadcastFunc: f}
}

// BroadcastLapHistory broadcasts the given lap history.
func (b *CustomEventBroadcasterImpl) BroadcastLapHistory(driverNum string, completedLap CompletedLap) {
	jsonMessage, err := FormatLapHistoryMessage(driverNum, completedLap)
	if err != nil {
		fmt.Printf("Error marshalling lap history message: %v\n", err)
		return
	}
	b.broadcastFunc(jsonMessage)
}

// BroadcastDriverListUpdates broadcasts the given driver list updates.
func (b *CustomEventBroadcasterImpl) BroadcastDriverListUpdates(drivers map[string]DriverInfo) {
	jsonMessage, err := FormatDriverListUpdateMessage(drivers)
	if err != nil {
		fmt.Printf("Error marshalling driver list update message: %v\n", err)
		return
	}
	b.broadcastFunc(jsonMessage)
}

// FormatLapHistoryMessage creates the JSON message for a lap history update.
func FormatLapHistoryMessage(driverNum string, completedLap CompletedLap) ([]byte, error) {
	message := map[string]interface{}{
		"M": []map[string]interface{}{
			{
				"H": "Streaming",
				"M": "feed",
				"A": []interface{}{
					"LapHistory",
					map[string]interface{}{
						"RacingNumber": driverNum,
						"CompletedLap": completedLap, // Send only the new completed lap
					},
				},
			},
		},
	}
	return json.Marshal(message)
}

// FormatDriverListUpdateMessage creates the JSON message for a driver list update.
func FormatDriverListUpdateMessage(drivers map[string]DriverInfo) ([]byte, error) {
	updatePayload := make(map[string]interface{})
	for driverNum, driverInfo := range drivers {
		// We only want to send the starting position
		updatePayload[driverNum] = map[string]interface{}{
			"StartingPosition": driverInfo.StartingPosition,
		}
	}

	message := map[string]interface{}{
		"M": []map[string]interface{}{
			{
				"H": "Streaming",
				"M": "feed",
				"A": []interface{}{
					"DriverList",
					updatePayload,
				},
			},
		},
	}
	return json.Marshal(message)
}
