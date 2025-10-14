package auth

import (
	"fmt"
	"os"
)

// Authenticate will handle the F1TV authentication process.
func Authenticate() (string, error) {
	if token := os.Getenv("F1_TV_TOKEN"); token != "" {
		fmt.Println("Using F1_TV_TOKEN from environment variable.")
		return token, nil
	}

	return "", fmt.Errorf("F1_TV_TOKEN environment variable not set")
}
