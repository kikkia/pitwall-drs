package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"context"
	"f1sockets/api"
	"f1sockets/auth"
	"f1sockets/broadcaster"
	"f1sockets/f1tvclient"
	"f1sockets/metrics"
	"f1sockets/model"
	"f1sockets/ratelimiter"
	"f1sockets/recorder"
	"f1sockets/valkeyclient"

	"golang.org/x/time/rate"
)

const (
	listenAddr = ":8080"
)

var (
	globalState            *model.GlobalState
	customEventBroadcaster model.CustomEventBroadcaster
	sessionEndedCount      int
)

var (
	RECORD_LOGS        = true
	autoConnect        = false
	valkeyAddr         string
	skippedFeedUpdates = 0
	f1tvClient         *f1tvclient.F1TVClient
)

func init() {
	flag.BoolVar(&autoConnect, "auto-connect", false, "Automatically connect/disconnect to F1TV based on session times")
	flag.StringVar(&valkeyAddr, "valkey-addr", os.Getenv("VALKEY_ADDR"), "Address for the Valkey instance. If not set, Valkey is disabled. Can also be set via VALKEY_ADDR env var.")
}

func main() {
	flag.Parse()

	metrics.Init()

	fmt.Printf("Starting F1TV SignalR Proxy on %s\n", listenAddr)

	connectionLimiter := ratelimiter.NewConnectionLimiter(100)

	// Recorder setup
	sessionRecorder := recorder.NewRecorder(2*time.Second, func() *model.GlobalState {
		return globalState
	}, RECORD_LOGS)
	sessionRecorder.Start()
	defer sessionRecorder.Stop()

	browserBroadcaster := broadcaster.NewBroadcaster(connectionLimiter, sessionRecorder)
	customEventBroadcaster = model.NewCustomEventBroadcaster(browserBroadcaster.Broadcast)

	var valkey *valkeyclient.ValkeyClient
	if valkeyAddr != "" {
		fmt.Printf("Valkey integration enabled, connecting to %s\n", valkeyAddr)
		valkey = valkeyclient.NewValkeyClient(valkeyAddr)
		auth.SetValkeyClient(valkey)
	}

	seasonLoader := api.NewSeasonLoader(24*time.Hour, valkey)
	seasonLoader.Start()
	defer seasonLoader.Stop()

	fmt.Println("Waiting for initial season data to load...")
	seasonLoader.WaitUntilReady()
	fmt.Println("Season data loaded.")

	f1tvClient = f1tvclient.NewF1TVClient(func(message []byte) {
		// Always broadcast and record the raw message
		browserBroadcaster.Broadcast(message)
		sessionRecorder.Record(message)

		// Try to unmarshal as initial state first {"R":{...}}
		var rMessage struct {
			R json.RawMessage `json:"R"`
		}
		if json.Unmarshal(message, &rMessage) == nil && rMessage.R != nil {
			var err error
			globalState, err = model.NewGlobalState(message, customEventBroadcaster, globalState)
			if err != nil {
				fmt.Printf("Failed to parse global state message: %v\n", err)
			} else {
				// Reset the counter on successful global state initialization
				skippedFeedUpdates = 0
				fmt.Println("Global state successfully initialized.")
			}
			return
		}

		// Try to unmarshal as a feed update ["Topic", {...data...}]
		var feedArgs []json.RawMessage
		if json.Unmarshal(message, &feedArgs) == nil {
			if len(feedArgs) >= 2 {
				if globalState != nil {
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

					err := globalState.ApplyFeedUpdate([]interface{}{topic, payload})
					if err != nil {
						// This can be noisy, so maybe comment out for production
						// fmt.Printf("Failed to apply feed update for topic %s: %v\n", topic, err)
					}
					skippedFeedUpdates = 0
				} else {
					skippedFeedUpdates++
					if skippedFeedUpdates%10 == 0 {
						fmt.Printf("Skipping feed update, global state not yet initialized (%d skipped).\n", skippedFeedUpdates)
					}
					if skippedFeedUpdates >= 50 { // Increased threshold
						fmt.Println("50 consecutive feed updates skipped, restarting F1TV client.")
						f1tvClient.ForceReconnect()
						skippedFeedUpdates = 0 // Reset after triggering reconnect
					}
				}
			}
			return
		}

		// If it's neither, log it as an unknown message format
		fmt.Printf("Unknown message format received: %s\n", string(message))
	})

	if autoConnect {
		fmt.Println("Auto-connect mode enabled. F1TV client will connect/disconnect based on session times.")

		checkAndManageConnection(f1tvClient, seasonLoader, valkey)

		go manageF1TVConnection(f1tvClient, seasonLoader, valkey)
	} else {
		fmt.Println("Auto-connect mode disabled. F1TV client starting immediately.")
		f1tvClient.Start()
		defer f1tvClient.Stop()
	}

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		if globalState == nil {
			// Initialize with a dummy broadcaster if not already set
			globalState = model.NewEmptyGlobalState()
			globalState.Broadcaster = customEventBroadcaster
		}

		initialState, err := globalState.GetStateAsJSON()
		if err != nil {
			fmt.Printf("Error retrieving initial global state: %v\n", err)
			initialState = nil
		}
		browserBroadcaster.HandleConnections(w, r, initialState)
	})

	http.Handle("/state", metrics.InstrumentHandler(http.HandlerFunc(handleState)))
	http.Handle("/season", metrics.InstrumentHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		schedule := seasonLoader.GetSeasonSchedule()

		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*") // For local dev

		if len(schedule.Events) == 0 {
			w.WriteHeader(http.StatusServiceUnavailable)
			json.NewEncoder(w).Encode(map[string]string{"message": "Season data not yet available or failed to load."})
			return
		}

		err := json.NewEncoder(w).Encode(schedule)
		if err != nil {
			fmt.Printf("Error encoding season schedule JSON: %v\n", err)
			http.Error(w, "Internal server error", http.StatusInternalServerError)
		}
	})))

	http.Handle("/recordings", metrics.InstrumentHandler(http.HandlerFunc(api.HandleRecordings)))

	limiter := ratelimiter.NewIPRateLimiter(rate.Every(time.Minute), 15) // 15 requests per minute
	fileHandler := http.StripPrefix("/recordings/", api.RecordingsFileHandler())

	http.Handle("/recordings/", metrics.InstrumentHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := ratelimiter.GetClientIP(r)
		if !limiter.GetLimiter(ip).Allow() {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		fileHandler.ServeHTTP(w, r)
	})))

	http.Handle("/track/", metrics.InstrumentHandler(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := ratelimiter.GetClientIP(r)
		if !limiter.GetLimiter(ip).Allow() {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		api.HandleTrack(w, r)
	})))

	err := http.ListenAndServe(listenAddr, nil)
	if err != nil {
		fmt.Printf("HTTP server failed: %v\n", err)
	}
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

func resetGlobalState() {
	if globalState != nil {
		fmt.Println("Resetting global state for new connection.")
	}
	globalState = model.NewEmptyGlobalState()
	globalState.Broadcaster = customEventBroadcaster
}

// findActiveEvent checks the schedule for a currently active event based on now time and buffer.
func findActiveEvent(schedule *model.SeasonSchedule, now time.Time, buffer time.Duration) *model.Event {
	for _, event := range schedule.Events {
		connectTime := event.StartTime.Add(-buffer)
		disconnectTime := event.EndTime.Add(buffer * 4)

		if now.After(connectTime) && now.Before(disconnectTime) {
			return &event
		}
	}
	return nil
}

// findRaceWeekendEvents finds all events for the next upcoming race weekend.
func findRaceWeekendEvents(schedule *model.SeasonSchedule, now time.Time) []model.Event {
	var nextEvent *model.Event
	minDiff := time.Duration(-1)

	// First, find the very next event
	for i := range schedule.Events {
		event := schedule.Events[i]
		diff := event.StartTime.Sub(now)
		if diff > 0 && (minDiff == -1 || diff < minDiff) {
			minDiff = diff
			nextEvent = &event
		}
	}

	if nextEvent == nil {
		return nil // No upcoming events
	}

	// Now, find all events for that weekend (same location, close in time)
	var weekendEvents []model.Event
	weekendWindow := 4 * 24 * time.Hour // 4 days to be safe
	for _, event := range schedule.Events {
		if event.Location == nextEvent.Location {
			if event.StartTime.After(nextEvent.StartTime.Add(-weekendWindow)) && event.StartTime.Before(nextEvent.StartTime.Add(weekendWindow)) {
				weekendEvents = append(weekendEvents, event)
			}
		}
	}
	return weekendEvents
}

// needsTokenRefresh checks if the auth token will expire before the end of the next race weekend.
func needsTokenRefresh(schedule *model.SeasonSchedule, now time.Time) bool {
	weekendEvents := findRaceWeekendEvents(schedule, now)
	if len(weekendEvents) == 0 {
		return false // No upcoming events, no need to refresh
	}

	// Find the end time of the last event in the weekend
	var lastEndTime time.Time
	for _, event := range weekendEvents {
		if event.EndTime.After(lastEndTime) {
			lastEndTime = event.EndTime
		}
	}

	// If the token expires before the end of the race weekend, we need a new one.
	tokenExpiry := auth.TokenExpiresAt()
	if tokenExpiry.IsZero() || tokenExpiry.Before(lastEndTime) {
		fmt.Printf("Token refresh needed. Current expiry: %s, Race weekend ends: %s\n", tokenExpiry.Format(time.RFC3339), lastEndTime.Format(time.RFC3339))
		return true
	}

	return false
}

// checkAndManageConnection contains the core logic for deciding whether to connect or disconnect.
func checkAndManageConnection(client *f1tvclient.F1TVClient, loader *api.SeasonLoader, valkey *valkeyclient.ValkeyClient) {
	const bufferDuration = 14 * time.Minute
	now := time.Now()
	schedule := loader.GetSeasonSchedule()

	activeEvent := findActiveEvent(&schedule, now, bufferDuration)

	if activeEvent != nil {
		if valkey != nil {
			completed, err := valkey.IsSessionCompleted(context.Background(), activeEvent.UID)
			if err != nil {
				fmt.Printf("Error checking if session is completed: %v\n", err)
			} else if completed {
				fmt.Printf("Session '%s' is already completed. Skipping connection.\n", activeEvent.Summary)
				if client.IsRunning() {
					fmt.Println("Client is running for a completed session. Stopping it.")
					client.Stop()
				}
				return
			}
		}

		if !client.IsRunning() {
			fmt.Printf("Session '%s' is active. Connecting to F1TV client...\n", activeEvent.Summary)
			resetGlobalState()
			client.Start()
		}

		if client.IsRunning() {
			if handleFinalisedSessionCheck(client, valkey, activeEvent) {
				return
			}
		}

	} else {
		// If no session is active, check if we need to pre-authenticate for the upcoming weekend.
		if needsTokenRefresh(&schedule, now) {
			fmt.Println("Token needs refresh for the upcoming race weekend. Triggering authentication.")
			go func() {
				_, err := auth.Authenticate()
				if err != nil {
					fmt.Printf("Error during pre-authentication: %v\n", err)
				}
			}()
		}

		if client.IsRunning() {
			fmt.Println("No active session. Disconnecting F1TV client...")
			if valkey != nil && globalState != nil {
				err := valkey.SaveState(context.Background(), globalState)
				if err != nil {
					fmt.Printf("Error saving state to Valkey: %v\n", err)
				} else {
					fmt.Println("Global state saved to Valkey.")
				}
			}
			client.Stop()
			sessionEndedCount = 0
		} else if valkey != nil {
			// No active event and client is not running, try to load from Valkey
			if globalState == nil || globalState.R.SessionInfo == nil {
				fmt.Println("No active session, loading latest state from Valkey...")
				loadedState, err := valkey.LoadLatestState(context.Background())
				if err != nil {
					fmt.Printf("Error loading state from Valkey: %v\n", err)
				} else if loadedState != nil {
					globalState = loadedState
					globalState.Broadcaster = customEventBroadcaster
					fmt.Println("Successfully loaded latest state from Valkey.")
				} else {
					fmt.Println("No previous state found in Valkey.")
				}
			}
		}
	}
}

// handleFinalisedSessionCheck checks if the session has ended and stops the client if needed.
// It returns true if the client was stopped, false otherwise.
func handleFinalisedSessionCheck(client *f1tvclient.F1TVClient, valkey *valkeyclient.ValkeyClient, activeEvent *model.Event) bool {
	const endedCountToFinish = 5

	if globalState != nil && globalState.IsSessionFinished() {
		sessionEndedCount++
		fmt.Printf("Session is ended. Check %d of %d.\n", sessionEndedCount, endedCountToFinish)
	} else {
		sessionEndedCount = 0
	}

	if sessionEndedCount >= endedCountToFinish {
		fmt.Println("Session finalised. Disconnecting F1TV client...")

		if valkey != nil {
			err := valkey.AddCompletedSession(context.Background(), activeEvent.UID)
			if err != nil {
				fmt.Printf("Error adding completed session to Valkey: %v\n", err)
			}

			if globalState != nil {
				err := valkey.SaveState(context.Background(), globalState)
				if err != nil {
					fmt.Printf("Error saving state to Valkey: %v\n", err)
				} else {
					fmt.Println("Global state saved to Valkey.")
				}
			}
		}

		client.Stop()
		sessionEndedCount = 0
		return true
	}
	return false
}

func manageF1TVConnection(client *f1tvclient.F1TVClient, loader *api.SeasonLoader, valkey *valkeyclient.ValkeyClient) {
	const checkInterval = 2 * time.Minute

	// Run once immediately on start to avoid initial delay
	checkAndManageConnection(client, loader, valkey)

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for range ticker.C {
		checkAndManageConnection(client, loader, valkey)
	}
}
