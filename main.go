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
	flag.BoolVar(&autoConnect, "auto-connect", true, "Automatically connect/disconnect to F1TV based on session times")
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
	}

	seasonLoader := api.NewSeasonLoader(24*time.Hour, valkey)
	seasonLoader.Start()
	defer seasonLoader.Stop()

	fmt.Println("Waiting for initial season data to load...")
	seasonLoader.WaitUntilReady()
	fmt.Println("Season data loaded.")

	f1tvClient = f1tvclient.NewF1TVClient(func(message []byte) {
		var signalRMessage map[string]interface{}
		if err := json.Unmarshal(message, &signalRMessage); err == nil {
			// R at top level denotes a global state update message
			if _, ok := signalRMessage["R"].(map[string]interface{}); ok {
				var err error
				globalState, err = model.NewGlobalState(message, customEventBroadcaster)
				if err != nil {
					fmt.Printf("Failed to parse global state message: %v\n", err)
				} else {
					// Reset the counter on successful global state initialization
					skippedFeedUpdates = 0
				}
			}
			if mArray, ok := signalRMessage["M"].([]interface{}); ok {
				for _, msgInterface := range mArray {
					if msgMap, ok := msgInterface.(map[string]interface{}); ok {
						// Check if it's a "feed" message
						if hub, hubOk := msgMap["H"].(string); hubOk && hub == "Streaming" {
							if method, methodOk := msgMap["M"].(string); methodOk && method == "feed" {
								if args, argsOk := msgMap["A"].([]interface{}); argsOk {
									if globalState != nil {
										err := globalState.ApplyFeedUpdate(args)
										if err != nil {
											fmt.Printf("Failed to apply feed update: %v\n Update Args: %v\n", err, args)
										}
										// Reset counter on any feed update attempt when global state is present
										skippedFeedUpdates = 0
									} else {
										fmt.Println("Skipping feed update as global state is not yet initialized.")
										skippedFeedUpdates++
										if skippedFeedUpdates >= 20 {
											fmt.Println("20 consecutive feed updates skipped, restarting F1TV client.")
											f1tvClient.ForceReconnect()
											skippedFeedUpdates = 0 // Reset after triggering reconnect
										}
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

		browserBroadcaster.Broadcast(message)
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
	const checkInterval = 1 * time.Minute

	// Run once immediately on start to avoid initial delay
	checkAndManageConnection(client, loader, valkey)

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for range ticker.C {
		checkAndManageConnection(client, loader, valkey)
	}
}
