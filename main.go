package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"strings"
	"sync"
	"time"

	"path/filepath"

	"context"
	"f1sockets/broadcaster"
	"f1sockets/f1tvclient"
	"f1sockets/filehandler"
	"f1sockets/model"
	"f1sockets/ratelimiter"
	"f1sockets/season"
	"f1sockets/valkeyclient"

	"golang.org/x/time/rate"
)

const (
	listenAddr = ":8080"
)

var (
	globalState           *model.GlobalState
	lapHistoryBroadcaster model.LapUpdateBroadcaster
)

var (
	logBuffer          = make([]string, 0, 1000)
	logBufferMutex     sync.Mutex
	logFlushTicker     = time.NewTicker(2 * time.Second)
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

	fmt.Printf("Starting F1TV SignalR Proxy on %s\n", listenAddr)

	browserBroadcaster := broadcaster.NewBroadcaster()
	lapHistoryBroadcaster = model.NewLapHistoryBroadcaster(browserBroadcaster.Broadcast)

	seasonLoader := season.NewSeasonLoader(24 * time.Hour)
	seasonLoader.Start()
	defer seasonLoader.Stop()

	fmt.Println("Waiting for initial season data to load...")
	seasonLoader.WaitUntilReady()
	fmt.Println("Season data loaded.")

	f1tvClient = f1tvclient.NewF1TVClient(func(message []byte) {
		if RECORD_LOGS && (globalState == nil || !globalState.IsSessionFinished()) {
			logEntry := fmt.Sprintf("[%s] %s\n", time.Now().Format(time.RFC3339), message)
			logBufferMutex.Lock()
			logBuffer = append(logBuffer, logEntry)
			logBufferMutex.Unlock()
		}

		var signalRMessage map[string]interface{}
		if err := json.Unmarshal(message, &signalRMessage); err == nil {
			// R at top level denotes a global state update message
			if _, ok := signalRMessage["R"].(map[string]interface{}); ok {
				var err error
				globalState, err = model.NewGlobalState(message, lapHistoryBroadcaster)
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
		var valkey *valkeyclient.ValkeyClient
		if valkeyAddr != "" {
			fmt.Printf("Valkey integration enabled, connecting to %s\n", valkeyAddr)
			valkey = valkeyclient.NewValkeyClient(valkeyAddr)
		}

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
			globalState.LapBroadcaster = lapHistoryBroadcaster
		}

		initialState, err := globalState.GetStateAsJSON()
		if err != nil {
			fmt.Printf("Error retrieving initial global state: %v\n", err)
			initialState = nil
		}
		browserBroadcaster.HandleConnections(w, r, initialState)
	})

	http.HandleFunc("/state", handleState)

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

	http.HandleFunc("/recordings", filehandler.HandleRecordings)

	limiter := ratelimiter.NewIPRateLimiter(rate.Every(time.Minute), 15) // 15 requests per minute
	fileHandler := http.StripPrefix("/recordings/", filehandler.RecordingsFileHandler())

	http.Handle("/recordings/", http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := ratelimiter.GetClientIP(r)
		if !limiter.GetLimiter(ip).Allow() {
			http.Error(w, "Too Many Requests", http.StatusTooManyRequests)
			return
		}
		fileHandler.ServeHTTP(w, r)
	}))

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

					filePath := getRecordingFilePath()
					if filePath == "" {
						fmt.Println("Filepath not yet present, skipping this logging dump")
						continue
					}

					f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
					if err != nil {
						fmt.Printf("Failed to open log file for appending: %v\n", err)
						continue
					}

					if _, err := f.WriteString(joinWithNewlines(toWrite)); err != nil {
						fmt.Printf("Failed to write log batch: %v\n", err)
					}
					f.Close()
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

func joinWithNewlines(lines []string) string {
	return fmt.Sprint(strings.Join(lines, "\n"))
}
func resetGlobalState() {
	if globalState != nil {
		fmt.Println("Resetting global state for new connection.")
	}
	globalState = model.NewEmptyGlobalState()
	globalState.LapBroadcaster = lapHistoryBroadcaster
}

// findActiveEvent checks the schedule for a currently active event based on now time and buffer.
func findActiveEvent(schedule *model.SeasonSchedule, now time.Time, buffer time.Duration) *model.Event {
	for _, event := range schedule.Events {
		connectTime := event.StartTime.Add(-buffer)
		disconnectTime := event.EndTime.Add(buffer)

		if now.After(connectTime) && now.Before(disconnectTime) {
			return &event
		}
	}
	return nil
}

// checkAndManageConnection contains the core logic for deciding whether to connect or disconnect.
func checkAndManageConnection(client *f1tvclient.F1TVClient, loader *season.SeasonLoader, valkey *valkeyclient.ValkeyClient) {
	const bufferDuration = 14 * time.Minute
	now := time.Now()
	schedule := loader.GetSeasonSchedule()

	activeEvent := findActiveEvent(&schedule, now, bufferDuration)

	if activeEvent != nil {
		if !client.IsRunning() {
			fmt.Printf("Session '%s' is active. Connecting to F1TV client...\n", activeEvent.Summary)
			resetGlobalState()
			client.Start()
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
		} else if valkey != nil {
			// No active event and client is not running, try to load from Valkey
			if globalState == nil || globalState.R.SessionInfo == nil {
				fmt.Println("No active session, loading latest state from Valkey...")
				loadedState, err := valkey.LoadLatestState(context.Background())
				if err != nil {
					fmt.Printf("Error loading state from Valkey: %v\n", err)
				} else if loadedState != nil {
					globalState = loadedState
					globalState.LapBroadcaster = lapHistoryBroadcaster
					fmt.Println("Successfully loaded latest state from Valkey.")
				} else {
					fmt.Println("No previous state found in Valkey.")
				}
			}
		}
	}
}

func manageF1TVConnection(client *f1tvclient.F1TVClient, loader *season.SeasonLoader, valkey *valkeyclient.ValkeyClient) {
	const checkInterval = 1 * time.Minute

	// Run once immediately on start to avoid initial delay
	checkAndManageConnection(client, loader, valkey)

	ticker := time.NewTicker(checkInterval)
	defer ticker.Stop()

	for range ticker.C {
		checkAndManageConnection(client, loader, valkey)
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
