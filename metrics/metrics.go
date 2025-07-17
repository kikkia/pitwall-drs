package metrics

import (
	"fmt"
	"log"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/DataDog/datadog-go/v5/statsd"
)

var (
	Client  *statsd.Client
	Enabled bool
)

func Init() {
	if os.Getenv("DD_ENABLED") != "true" {
		Enabled = false
		return
	}
	Enabled = true

	agentHost := os.Getenv("DD_AGENT_HOST")
	if agentHost == "" {
		agentHost = "localhost"
	}
	agentPortStr := os.Getenv("DD_DOGSTATSD_PORT")
	if agentPortStr == "" {
		agentPortStr = "8125"
	}

	addr := fmt.Sprintf("%s:%s", agentHost, agentPortStr)

	var err error
	Client, err = statsd.New(addr, statsd.WithTags([]string{"service:f1-socket-proxy"}))
	if err != nil {
		log.Fatalf("Failed to create StatsD client: %v", err)
	}

	fmt.Println("Datadog client initialized")
}

func OpenConnection() {
	if Client == nil {
		return
	}
	Client.Incr("f1_socket_proxy.websockets.connection_open", nil, 1)
}

func CloseConnection() {
	if Client == nil {
		return
	}
	Client.Incr("f1_socket_proxy.websockets.connection_close", nil, 1)
}

func TotalConnections(connectionCount int) {
	if Client == nil {
		return
	}
	Client.Gauge("f1_socket_proxy.websockets.total_connections", float64(connectionCount), nil, 0)
}

// Middleware to track API requests.
func InstrumentHandler(handler http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !Enabled {
			handler.ServeHTTP(w, r)
			return
		}
		start := time.Now()
		handler.ServeHTTP(w, r)
		duration := time.Since(start)

		if Client == nil {
			return
		}

		tags := []string{
			"route:" + r.URL.Path,
			"method:" + r.Method,
			"status:" + strconv.Itoa(200),
		}

		Client.Timing("f1_socket_proxy.api.request_duration", duration, tags, 1)
		Client.Incr("f1_socket_proxy.api.requests_total", tags, 1)
	})
}
