package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"path/filepath"

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
	logBuffer      = make([]string, 0, 1000)
	logBufferMutex sync.Mutex
	logFlushTicker = time.NewTicker(2 * time.Second)
	RECORD_LOGS    = true
	autoConnect    = false
)

func init() {
	flag.BoolVar(&autoConnect, "auto-connect", false, "Automatically connect/disconnect to F1TV based on session times")
}

func main() {
	flag.Parse()

	fmt.Printf("Starting F1TV SignalR Proxy on %s\n", listenAddr)

	browserBroadcaster := broadcaster.NewBroadcaster()
	lapHistoryBroadcaster = model.NewLapHistoryBroadcaster(browserBroadcaster.Broadcast)

	seasonLoader := season.NewSeasonLoader(24 * time.Hour) // Refresh data daily
	seasonLoader.Start()
	defer seasonLoader.Stop() // Ensure it stops on main exit

	f1tvClient := f1tvclient.NewF1TVClient(func(message []byte) {
		if RECORD_LOGS && (globalState == nil || !globalState.IsSessionFinished()) {
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

	if autoConnect {
		fmt.Println("Auto-connect mode enabled. F1TV client will connect/disconnect based on session times.")
		go manageF1TVConnection(f1tvClient, seasonLoader)
	} else {
		fmt.Println("Auto-connect mode disabled. F1TV client starting immediately.")
		f1tvClient.Start()
		defer f1tvClient.Stop()
	}

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
				if !f1tvClient.IsRunning() {
					//fmt.Println("Client not connected, not logging")
					continue
				}

				if getRecordingFilePath() == "" {
					fmt.Println("Filepath not yet present, skipping this logging dump")
					continue
				}

				logBufferMutex.Lock()
				if len(logBuffer) > 0 {
					toWrite := logBuffer
					logBuffer = make([]string, 0, 1000)
					logBufferMutex.Unlock()

					f, err := ioutil.ReadFile(getRecordingFilePath())
					if err != nil {
						f = []byte{}
					}
					combined := append(f, []byte(fmt.Sprintf("%s\n", joinWithNewlines(toWrite)))...)
					err = ioutil.WriteFile(getRecordingFilePath(), combined, 0644)
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

func getRecordingFilePath() string {
	if globalState == nil || globalState.R.SessionInfo == nil {
		fmt.Println("Warning: globalState or SessionInfo is nil, cannot determine recording file path.")
		return ""
	}

	formattedPath := formatSessionPath(globalState.R.SessionInfo.Path)
	fullPath := "recordings/" + formattedPath + ".txt"

	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Printf("Error creating directory %s: %v\n", dir, err)
		return ""
	}

	return fullPath
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

func manageF1TVConnection(client *f1tvclient.F1TVClient, loader *season.SeasonLoader) {
	const checkInterval = 1 * time.Minute
	const bufferDuration = 1 * time.Hour // Connect 1hr before, disconnect 1hr after

	ticker := time.NewTicker(checkInterval) // TODO: On start 1 min delay???
	defer ticker.Stop()

	for range ticker.C {
		now := time.Now()
		schedule := loader.GetSeasonSchedule()

		var activeEvent *model.Event
		for _, event := range schedule.Events {
			connectTime := event.StartTime.Add(-bufferDuration)
			disconnectTime := event.EndTime.Add(bufferDuration)

			if now.After(connectTime) && now.Before(disconnectTime) {
				activeEvent = &event
				break
			}
		}

		if activeEvent != nil {
			if !client.IsRunning() {
				fmt.Printf("Session '%s' is active. Connecting to F1TV client...\n", activeEvent.Summary)
				client.Start()
			}
		} else {
			if client.IsRunning() {
				fmt.Println("No active session. Disconnecting F1TV client...")
				client.Stop()
			}
		}
	}
}

// formatSessionPath takes a session path like "2025/2025-06-15_Canadian_Grand_Prix/2025-06-13_Practice_1/"
// and transforms it to "2025/Canadian_Grand_Prix/Practice_1"
func formatSessionPath(sessionPath string) string {
	parts := strings.Split(sessionPath, "/")
	if len(parts) == 0 {
		return ""
	}

	cleanedParts := []string{parts[0]}

	for i := 1; i < len(parts); i++ {
		part := parts[i]
		if part == "" {
			continue // Skip empty parts, especially if path ends with "/"
		}
		// Check if the part starts with a date pattern ("YYYY-MM-DD_")
		if len(part) >= 11 && part[4] == '-' && part[7] == '-' && part[10] == '_' {
			cleanedParts = append(cleanedParts, part[11:])
		} else {
			cleanedParts = append(cleanedParts, part)
		}
	}

	return strings.Join(cleanedParts, "/")
}
