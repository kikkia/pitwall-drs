package auth

import (
	"bytes"
	"encoding/json"
	"f1sockets/metrics"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"time"
)

type authRequestPayload struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type authResponsePayload struct {
	LoginSession struct {
		Expires           int64  `json:"expires"`
		SubscriptionToken string `json:"subscriptionToken"`
	} `json:"loginSession"`
}

var (
	tokenCache struct {
		sync.RWMutex
		token   string
		expires time.Time
	}
	httpClient = &http.Client{Timeout: 90 * time.Second}
)

func Authenticate() (string, error) {
	tokenCache.RLock()

	if tokenCache.token != "" && time.Now().Before(tokenCache.expires.Add(-1*time.Hour)) {
		fmt.Println("Using cached F1TV token.")
		token := tokenCache.token
		tokenCache.RUnlock()
		return token, nil
	}
	tokenCache.RUnlock()

	tokenCache.Lock()
	defer tokenCache.Unlock()

	if tokenCache.token != "" && time.Now().Before(tokenCache.expires.Add(-1*time.Hour)) {
		fmt.Println("Using cached F1TV token (refreshed by another process).")
		return tokenCache.token, nil
	}

	if token := os.Getenv("F1_TV_TOKEN"); token != "" {
		fmt.Println("Using F1_TV_TOKEN from environment variable for testing and caching it.")
		tokenCache.token = token
		tokenCache.expires = time.Now().AddDate(10, 0, 0)
		return token, nil
	}

	fmt.Println("No valid cached token found. Fetching new token from auth service.")
	metrics.TokenFetch()

	email := os.Getenv("F1_EMAIL")
	password := os.Getenv("F1_PASSWORD")
	authURL := os.Getenv("F1_AUTH_URL")

	if email == "" || password == "" || authURL == "" {
		err := fmt.Errorf("F1_EMAIL, F1_PASSWORD, and F1_AUTH_URL environment variables must be set")
		metrics.TokenFetchFailed()
		return "", err
	}

	reqBody, err := json.Marshal(authRequestPayload{Email: email, Password: password})
	if err != nil {
		metrics.TokenFetchFailed()
		return "", fmt.Errorf("failed to marshal auth request body: %w", err)
	}

	req, err := http.NewRequest("POST", authURL, bytes.NewBuffer(reqBody))
	if err != nil {
		metrics.TokenFetchFailed()
		return "", fmt.Errorf("failed to create auth request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := httpClient.Do(req)
	if err != nil {
		metrics.TokenFetchFailed()
		return "", fmt.Errorf("failed to execute auth request: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		bodyBytes, _ := io.ReadAll(resp.Body)
		err := fmt.Errorf("auth service returned non-OK status %d: %s", resp.StatusCode, string(bodyBytes))
		metrics.TokenFetchFailed()
		return "", err
	}

	var respPayload authResponsePayload
	if err := json.NewDecoder(resp.Body).Decode(&respPayload); err != nil {
		metrics.TokenFetchFailed()
		return "", fmt.Errorf("failed to decode auth response: %w", err)
	}

	if respPayload.LoginSession.SubscriptionToken == "" {
		err := fmt.Errorf("auth service response did not contain a subscription token")
		metrics.TokenFetchFailed()
		return "", err
	}

	// Update the cache
	tokenCache.token = respPayload.LoginSession.SubscriptionToken
	tokenCache.expires = time.Unix(respPayload.LoginSession.Expires, 0)

	fmt.Printf("Successfully fetched and cached new F1TV token. Expires at: %s\n", tokenCache.expires.Format(time.RFC3339))
	metrics.TokenFetchSuccess()

	return tokenCache.token, nil
}

func TokenExpiresAt() time.Time {
	tokenCache.RLock()
	defer tokenCache.RUnlock()
	return tokenCache.expires
}
