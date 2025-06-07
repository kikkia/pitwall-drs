package main

import (
	"encoding/json"
	"fmt"
	"io"
	"io/ioutil"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"f1sockets/model"

	ics "github.com/arran4/golang-ical"
	"github.com/gorilla/websocket"
)

const (
	f1tvBaseURL    = "https://livetiming.formula1.com/signalr"
	clientProtocol = "1.5"
	hubName        = "Streaming"
	// Address to listen on for browser WebSocket connections
	listenAddr = "localhost:8080"
)

var (
	globalState       = model.NewEmptyGlobalState()
	seasonSchedule    model.SeasonSchedule
	lastScheduleFetch int64
)

type NegotiateResponse struct {
	ConnectionToken string `json:"ConnectionToken"`
	// Other fields exist but are not needed for connection
}

// Manager for connected browser clients
type ClientManager struct {
	clients map[*websocket.Conn]bool
	sync.RWMutex
}

var manager = ClientManager{
	clients: make(map[*websocket.Conn]bool),
}

var (
	logBuffer      = make([]string, 0, 100)
	logBufferMutex sync.Mutex
	logFlushTicker = time.NewTicker(2 * time.Second)
	logFilePath    = "recordings/f1tv_events_spain_race.txt"
	DEBUG          = true
)

var upgrader = websocket.Upgrader{
	CheckOrigin: func(r *http.Request) bool {
		// Allow connections from any origin during development.
		// In production, disable this.
		return true
	},
}

func main() {
	fmt.Printf("Starting F1TV SignalR Proxy on %s\n", listenAddr)

	go loadSeasonData()

	// the http endpoint listening for new connections
	http.HandleFunc("/ws", handleBrowserConnections)

	// New HTTP endpoint to serve the latest DriverList data
	http.HandleFunc("/state", handleState)

	http.HandleFunc("/apply", handleApply)

	http.HandleFunc("/season", handleSeason)

	// routine for connection and proxying it out
	go manageF1TVConnection()

	if DEBUG {
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

func handleBrowserConnections(w http.ResponseWriter, r *http.Request) {
	conn, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		fmt.Printf("Failed to upgrade HTTP to WebSocket: %v\n", err)
		return
	}
	defer conn.Close()

	var beginningState []byte
	beginningState, err = globalState.GetStateAsJSON()
	if err != nil {
		fmt.Printf("Error retrieving global state json for initial message %s: %v\n", conn.RemoteAddr(), err)
		return
	}

	err = conn.WriteMessage(1, beginningState)
	if err != nil {
		fmt.Printf("Error sending global state message to browser client %s: %v\n", conn.RemoteAddr(), err)
		return
	}

	manager.Lock()
	manager.clients[conn] = true
	manager.Unlock()

	fmt.Printf("Browser client connected: %s. Total clients: %d\n", conn.RemoteAddr(), len(manager.clients))

	for {
		// No events from client to handle but we can watch for disconnection here
		_, _, err := conn.ReadMessage()
		if err != nil {
			fmt.Printf("Browser client disconnected: %s. Error: %v\n", conn.RemoteAddr(), err)
			break
		}
	}

	manager.Lock()
	delete(manager.clients, conn)
	manager.Unlock()
	fmt.Printf("Browser client removed: %s. Total clients: %d\n", conn.RemoteAddr(), len(manager.clients))
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

func manageF1TVConnection() {
	for {
		fmt.Println("Attempting to connect to F1TV SignalR...")

		token, cookie, err := negotiate()
		if err != nil {
			fmt.Printf("Negotiation failed: %v. Retrying in 5 seconds...\n", err)
			time.Sleep(5 * time.Second)
			continue
		}
		fmt.Println("Negotiation successful.")

		f1tvConn, err := connectF1TVWebSocket(token, cookie)
		if err != nil {
			fmt.Printf("F1TV WebSocket connection failed: %v. Retrying in 5 seconds...\n", err)
			time.Sleep(5 * time.Second)
			continue
		}
		fmt.Println("Connected to F1TV WebSocket.")
		defer f1tvConn.Close()

		sendSubscribeMessage(f1tvConn)

		for {
			messageType, message, err := f1tvConn.ReadMessage()
			if err != nil {
				fmt.Printf("Error reading from F1TV WebSocket: %v. Reconnecting...\n", err)
				break
			}

			// fmt.Printf("Received from F1TV (%d bytes): %s\n", len(message), message)

			if DEBUG {
				logEntry := fmt.Sprintf("[%s] %s", time.Now().Format(time.RFC3339), message)
				logBufferMutex.Lock()
				logBuffer = append(logBuffer, logEntry)
				logBufferMutex.Unlock()
			}

			var signalRMessage map[string]interface{}
			if err := json.Unmarshal(message, &signalRMessage); err == nil {
				// R at top level denotes a global state update message
				if _, ok := signalRMessage["R"].(map[string]interface{}); ok {
					globalState, err = model.NewGlobalState(message)
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

			// relay message to all connected browser clients
			manager.RLock()
			for client := range manager.clients {
				err := client.WriteMessage(messageType, message)
				if err != nil {
					fmt.Printf("Error sending message to browser client %s: %v\n", client.RemoteAddr(), err)
					// Client likely disconnected, will be removed by handleBrowserConnections
				}
			}
			manager.RUnlock()
		}

		// Only will reach this in an error state, so chill a sec before retrying
		time.Sleep(5 * time.Second)
	}
}

func negotiate() (string, string, error) {
	connectionData := url.QueryEscape(fmt.Sprintf(`[{"name":"%s"}]`, hubName))
	negotiateURL := fmt.Sprintf("%s/negotiate?connectionData=%s&clientProtocol=%s", f1tvBaseURL, connectionData, clientProtocol)

	resp, err := http.Get(negotiateURL)
	if err != nil {
		return "", "", fmt.Errorf("http GET error: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := ioutil.ReadAll(resp.Body)
		return "", "", fmt.Errorf("negotiate failed with status code %d: %s", resp.StatusCode, string(bodyBytes))
	}

	bodyBytes, err := ioutil.ReadAll(resp.Body)
	if err != nil {
		return "", "", fmt.Errorf("failed to read negotiate response body: %w", err)
	}

	var negotiateResp NegotiateResponse
	err = json.Unmarshal(bodyBytes, &negotiateResp)
	if err != nil {
		return "", "", fmt.Errorf("failed to parse negotiate response JSON: %w", err)
	}

	cookie := ""
	setCookieHeaders, ok := resp.Header["Set-Cookie"]
	if ok && len(setCookieHeaders) > 0 {
		// split off options like 'path'
		cookie = setCookieHeaders[0]
		if semiIndex := (len(cookie)); semiIndex > 0 {
			cookie = cookie[:semiIndex]
		}
	}

	if negotiateResp.ConnectionToken == "" {
		return "", "", fmt.Errorf("connection token is empty in negotiate response")
	}

	return negotiateResp.ConnectionToken, cookie, nil
}

func connectF1TVWebSocket(token, cookie string) (*websocket.Conn, error) {
	encodedToken := url.QueryEscape(token)
	connectionData := url.QueryEscape(fmt.Sprintf(`[{"name":"%s"}]`, hubName))
	wsURL := fmt.Sprintf("wss://livetiming.formula1.com/signalr/connect?clientProtocol=%s&transport=webSockets&connectionToken=%s&connectionData=%s", clientProtocol, encodedToken, connectionData)

	headers := http.Header{}
	headers.Add("User-Agent", "f1-testing")
	headers.Add("Accept-Encoding", "gzip,identity")
	if cookie != "" {
		headers.Add("Cookie", cookie)
	}

	conn, _, err := websocket.DefaultDialer.Dial(wsURL, headers)
	if err != nil {
		return nil, fmt.Errorf("websocket dial error: %w", err)
	}

	return conn, nil
}

func sendSubscribeMessage(conn *websocket.Conn) {

	topics := []string{
		"Heartbeat",
		"CarData.z",
		"Position.z",
		"CarData",
		"Position",
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
		"ChampionshipPrediction",
		"TeamRadio",
		"TyreStintSeries",
	}

	subscribeMessage := map[string]interface{}{
		"H": hubName,
		"M": "Subscribe",
		"A": []interface{}{topics},
		"I": 1,
	}

	message, err := json.Marshal(subscribeMessage)
	if err != nil {
		fmt.Printf("Failed to marshal subscribe message: %v\n", err)
		return
	}

	err = conn.WriteMessage(websocket.TextMessage, message)
	if err != nil {
		fmt.Printf("Failed to send subscribe message: %v\n", err)
	} else {
		fmt.Println("Subscribe message sent to F1TV.")
	}
}

func loadSeasonData() {
	fmt.Println("Fetching F1 season data...")
	resp, err := http.Get("https://ics.ecal.com/ecal-sub/660897ca63f9ca0008bcbea6/Formula%201.ics")
	if err != nil {
		fmt.Printf("Error fetching season data: %v\n", err)
		return
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		fmt.Printf("Error fetching season data: status code %d\n", resp.StatusCode)
		return
	}

	cal, err := ics.ParseCalendar(resp.Body)
	if err != nil {
		fmt.Printf("Error parsing ICS data: %v\n", err)
		return
	}

	seasonSchedule.Events = []model.Event{}

	for _, component := range cal.Events() {
		event := model.Event{
			UID:         component.GetProperty(ics.ComponentPropertyUniqueId).Value,
			Location:    component.GetProperty(ics.ComponentPropertyLocation).Value,
			Summary:     component.GetProperty(ics.ComponentPropertySummary).Value,
			Description: component.GetProperty(ics.ComponentPropertyDescription).Value,
		}

		dtStartProp := component.GetProperty(ics.ComponentPropertyDtStart)
		if dtStartProp != nil {
			startTime, err := component.GetStartAt()
			if err == nil {
				event.StartTime = startTime
			} else {
				fmt.Printf("Error parsing start time for event %s: %v\n", event.UID, err)
			}
		}

		dtEndProp := component.GetProperty(ics.ComponentPropertyDtEnd)
		if dtEndProp != nil {
			endTime, err := component.GetEndAt()
			if err == nil {
				event.EndTime = endTime
			} else {
				fmt.Printf("Error parsing end time for event %s: %v\n", event.UID, err)
			}
		}
		seasonSchedule.Events = append(seasonSchedule.Events, event)
	}
	lastScheduleFetch = time.Now().UnixMilli()
	fmt.Printf("Successfully loaded %d F1 events.\n", len(seasonSchedule.Events))
}

func handleSeason(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if len(seasonSchedule.Events) == 0 {
		w.WriteHeader(http.StatusServiceUnavailable)
		json.NewEncoder(w).Encode(map[string]string{"message": "Season data not yet available or failed to load."})
		return
	}

	if time.Now().UnixMilli()-28800000 > lastScheduleFetch { // 8hr refetch
		loadSeasonData()
	}

	err := json.NewEncoder(w).Encode(seasonSchedule)
	if err != nil {
		fmt.Printf("Error encoding season schedule JSON: %v\n", err)
		http.Error(w, "Internal server error", http.StatusInternalServerError)
	}
}

func joinWithNewlines(lines []string) string {
	return fmt.Sprint(strings.Join(lines, "\n"))
}
