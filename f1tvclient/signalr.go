package f1tvclient

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/go-kit/log"
	"github.com/philippseith/signalr"
)

func (c *F1TVClient) runNewStream(token string) {
	var clientCtx context.Context
	clientCtx, c.cancel = context.WithCancel(context.Background())

	headers := func() http.Header {
		h := http.Header{}
		h.Add("Authorization", "Bearer "+token)
		return h
	}

	client, err := signalr.NewClient(clientCtx,
		signalr.WithHttpConnection(clientCtx, signalRUrl, signalr.WithHTTPHeaders(headers)),
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
			fmt.Println("Stopped the F1TV Connection")
			return
		case state := <-stateChan:
			switch state {
			case signalr.ClientConnected:
				fmt.Println("Connected to F1TV SignalR.")
				c.subscribeToTopics()
			case signalr.ClientClosed:
				fmt.Println("F1TV connection lost. Attempting to reconnect...")
				fmt.Println("DEBUG: Client state changed to: Closed.")
			default:
				fmt.Printf("DEBUG: Client state changed to: UNKNOWN (%v)\n", state)
			}
		}
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
		"TimingData",
		"ChampionshipPrediction",
		"TeamRadio",
		"TyreStintSeries",
	}

	go func() {
		result := <-c.conn.Invoke("Subscribe", topics)
		if result.Error != nil {
			fmt.Printf("Failed to send subscribe message: %v\n", result.Error)
		} else {
			fmt.Println("Successfully subscribed to F1TV topics.")
			wrappedMessage, err := json.Marshal(map[string]interface{}{"R": result.Value})
			if err != nil {
				fmt.Printf("Error wrapping subscription result for global state: %v\n", err)
				return
			}
			if c.newStreamHandler != nil {
				c.newStreamHandler(wrappedMessage)
			}
		}
	}()
}
