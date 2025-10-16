package auth

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

// authRequestPayload defines the structure for the authentication request.
type authRequestPayload struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

// authResponsePayload defines the structure for the authentication response.
type authResponsePayload struct {
	LoginSession struct {
		Expires           int64  `json:"expires"`
		SubscriptionToken string `json:"subscriptionToken"`
	} `json:"loginSession"`
}

var (
	// tokenCache stores the authentication token and its expiration time.
	tokenCache struct {
		sync.RWMutex
		token   string
		expires time.Time
	}
	// httpClient is a shared HTTP client for authentication requests.
	httpClient = &http.Client{Timeout: 90 * time.Second}
)

// Authenticate handles the F1TV authentication process.
// It first checks for a valid cached token. If the token is expired or not present,
// it attempts to fetch a new one from the authentication service using credentials
// from environment variables.
func Authenticate() (string, error) {
	// Check for a valid, non-expired token in the cache first.
	tokenCache.RLock()
	// Check if token expires in more than an hour
	if tokenCache.token != "" && time.Now().Before(tokenCache.expires.Add(-1*time.Hour)) {
		fmt.Println("Using cached F1TV token.")
		token := tokenCache.token
		tokenCache.RUnlock()
		return token, nil
	}
	tokenCache.RUnlock()

	// If cache is invalid, acquire a lock to fetch a new token.
	tokenCache.Lock()
	defer tokenCache.Unlock()

	// Re-check the cache after acquiring the lock, in case another goroutine
	// just refreshed it.
	if tokenCache.token != "" && time.Now().Before(tokenCache.expires.Add(-1*time.Hour)) {
		fmt.Println("Using cached F1TV token (refreshed by another process).")
		return tokenCache.token, nil
	}

	fmt.Println("No valid cached token found. Fetching new token from auth service.")

	email := os.Getenv("F1_EMAIL")
	password := os.Getenv("F1_PASSWORD")
	authURL := os.Getenv("F1_AUTH_URL")

	if email == "" || password == "" || authURL == "" {
		return "", fmt.Errorf("F1_EMAIL, F1_PASSWORD, and F1_AUTH_URL environment variables must be set")
	}

	reqBody, err := json.Marshal(authRequestPayload{Email: email, Password: password})
	if err != nil {
		return "", fmt.Errorf("failed to marshal auth request body: %w", err)
	}

	req, err := http.NewRequest("POST", authURL, bytes.NewBuffer(reqBody))
	if err != nil {
		return "", fmt.Errorf("failed to create auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("failed to execute auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("auth service returned non-OK status %d: %s", resp.StatusCode, string(bodyBytes))
	}

	var respPayload authResponsePayload
	if err := json.NewDecoder(resp.Body).Decode(&respPayload); err != nil {
		return "", fmt.Errorf("failed to decode auth response: %w", err)
	}

	if respPayload.LoginSession.SubscriptionToken == "" {
		return "", fmt.Errorf("auth service response did not contain a subscription token")
	}

	// Update the cache
	tokenCache.token = respPayload.LoginSession.SubscriptionToken
	tokenCache.expires = time.Unix(respPayload.LoginSession.Expires, 0)

	fmt.Printf("Successfully fetched and cached new F1TV token. Expires at: %s\n", tokenCache.expires.Format(time.RFC3339))

	return tokenCache.token, nil
}

// TokenExpiresAt returns the expiration time of the cached token.
// It returns a zero time.Time if no token is cached.
func TokenExpiresAt() time.Time {
	tokenCache.RLock()
	defer tokenCache.RUnlock()
	return tokenCache.expires
}
