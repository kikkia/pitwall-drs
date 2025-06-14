package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"strings"
	"sync"
	"time"

	"f1sockets/broadcaster"
	"f1sockets/f1tvclient"
	"f1sockets/model"
	"f1sockets/season"
)

const (
	listenAddr = "localhost:8080"
)

var (
	globalState           *model.GlobalState
	lapHistoryBroadcaster model.LapUpdateBroadcaster
)

var (
	logBuffer      = make([]string, 0, 100)
	logBufferMutex sync.Mutex
	logFlushTicker = time.NewTicker(2 * time.Second)
	logFilePath    = "recordings/f1tv_events_canada_prac1-2test.txt"
	RECORD_LOGS    = true
)

func main() {
	fmt.Printf("Starting F1TV SignalR Proxy on %s\n", listenAddr)

	browserBroadcaster := broadcaster.NewBroadcaster()
	lapHistoryBroadcaster = model.NewLapHistoryBroadcaster(browserBroadcaster.Broadcast)

	seasonLoader := season.NewSeasonLoader(24 * time.Hour) // Refresh data daily
	seasonLoader.Start()
	defer seasonLoader.Stop() // Ensure it stops on main exit

	f1tvClient := f1tvclient.NewF1TVClient(func(message []byte) {
		if RECORD_LOGS {
			logEntry := fmt.Sprintf("[%s] %s", time.Now().Format(time.RFC3339), message)
			logBufferMutex.Lock()
			logBuffer = append(logBuffer, logEntry)
			logBufferMutex.Unlock()
		}

		var signalRMessage map[string]interface{}
		if err := json.Unmarshal(message, &signalRMessage); err == nil {
			// R at top level denotes a global state update message
			if _, ok := signalRMessage["R"].(map[string]interface{}); ok {
				var err error // shadowing the outer err is fine here
				globalState, err = model.NewGlobalState(message, lapHistoryBroadcaster)
				if err != nil {
					fmt.Printf("Failed to parse global state message: %v\n", err)
				}
			}
			if mArray, ok := signalRMessage["M"].([]interface{}); ok {
				for _, msgInterface := range mArray {
					if msgMap, ok := msgInterface.(map[string]interface{}); ok {
						// Check if it's a "feed" message
						if hub, hubOk := msgMap["H"].(string); hubOk && hub == "Streaming" {
							if method, methodOk := msgMap["M"].(string); methodOk && method == "feed" {
								if args, argsOk := msgMap["A"].([]interface{}); argsOk {
									// Ensure globalState is not nil before trying to update
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

		// Broadcast the message to all connected browser clients
		browserBroadcaster.Broadcast(message)
	})

	f1tvClient.Start()
	defer f1tvClient.Stop()

	// listen for new WebSocket connections
	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		// Ensure globalState is initialized before use
		if globalState == nil {
			// Initialize with a dummy broadcaster if not already set
			globalState = model.NewEmptyGlobalState()
			globalState.LapBroadcaster = lapHistoryBroadcaster
		}

		initialState, err := globalState.GetStateAsJSON()
		if err != nil {
			fmt.Printf("Error retrieving initial global state: %v\n", err)
			initialState = nil // Proceed without initial state if there's an error
		}
		browserBroadcaster.HandleConnections(w, r, initialState)
	})

	// HTTP endpoint to serve the latest DriverList data
	http.HandleFunc("/state", handleState)

	http.HandleFunc("/apply", handleApply)

	// Serve season data
	http.HandleFunc("/season", func(w http.ResponseWriter, r *http.Request) {
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
	})

	if RECORD_LOGS {
		go func() {
			for range logFlushTicker.C {
				logBufferMutex.Lock()
				if len(logBuffer) > 0 {
					toWrite := logBuffer
					logBuffer = make([]string, 0, 100)
					logBufferMutex.Unlock()

					f, err := ioutil.ReadFile(logFilePath)
					if err != nil {
						f = []byte{}
					}
					combined := append(f, []byte(fmt.Sprintf("%s\n", joinWithNewlines(toWrite)))...)
					err = ioutil.WriteFile(logFilePath, combined, 0644)
					if err != nil {
						fmt.Printf("Failed to write log batch: %v\n", err)
					}
				} else {
					logBufferMutex.Unlock()
				}
			}
		}()
	}

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

// Debug for sending a test message
func handleApply(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	bodyBytes, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "Failed to read request body: "+err.Error(), http.StatusInternalServerError)
		return
	}

	var payload map[string]interface{}
	if err := json.Unmarshal(bodyBytes, &payload); err != nil {
		http.Error(w, "Invalid JSON payload: "+err.Error(), http.StatusBadRequest)
		fmt.Printf("Error decoding JSON: %v\nRaw body: %s\n", err, string(bodyBytes))
		return
	}

	if mArray, ok := payload["M"].([]interface{}); ok {
		for _, msgInterface := range mArray {
			if msgMap, ok := msgInterface.(map[string]interface{}); ok {
				// Check if it's a "feed" message
				if hub, hubOk := msgMap["H"].(string); hubOk && hub == "Streaming" {
					if method, methodOk := msgMap["M"].(string); methodOk && method == "feed" {
						if args, argsOk := msgMap["A"].([]interface{}); argsOk {
							// Ensure globalState is not nil before trying to update
							if globalState != nil {
								// Ensure the globalState has the broadcaster set for manual updates
								if globalState.LapBroadcaster == nil {
									globalState.LapBroadcaster = lapHistoryBroadcaster
								}
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

	w.WriteHeader(http.StatusOK)
	w.Write([]byte("Feed update applied successfully"))
}

func joinWithNewlines(lines []string) string {
	return fmt.Sprint(strings.Join(lines, "\n"))
}
