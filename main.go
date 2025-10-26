package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"net/http"
	"os"
	"time"

	"f1sockets/api"
	"f1sockets/auth"
	"f1sockets/broadcaster"
	"f1sockets/f1tvclient"
	"f1sockets/handlers"
	"f1sockets/metrics"
	"f1sockets/model"
	"f1sockets/ratelimiter"
	"f1sockets/recorder"
	"f1sockets/sessionmanager"
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
	browserBroadcaster *broadcaster.Broadcaster
	sessionRecorder    *recorder.Recorder
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

	sessionRecorder = recorder.NewRecorder(2*time.Second, func() *model.GlobalState {
		return globalState
	}, RECORD_LOGS)
	sessionRecorder.Start()
	defer sessionRecorder.Stop()

	browserBroadcaster = broadcaster.NewBroadcaster(connectionLimiter, sessionRecorder)
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

	messageHandlers := handlers.NewMessageHandlers(&globalState, customEventBroadcaster, browserBroadcaster, sessionRecorder, f1tvClient, &skippedFeedUpdates)
	f1tvClient = f1tvclient.NewF1TVClient(messageHandlers.HandleNewStreamMessage, messageHandlers.HandleLegacyStreamMessage)

	if autoConnect {
		manager := sessionmanager.NewManager(f1tvClient, seasonLoader, valkey, &globalState, customEventBroadcaster)
		manager.Start()
	} else {
		fmt.Println("Auto-connect mode disabled. F1TV client starting immediately.")
		f1tvClient.Start()
		defer f1tvClient.Stop()
	}

	http.HandleFunc("/ws", func(w http.ResponseWriter, r *http.Request) {
		if globalState == nil {
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
		w.Header().Set("Access-Control-Allow-Origin", "*")

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

	limiter := ratelimiter.NewIPRateLimiter(rate.Every(time.Minute), 15)
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
