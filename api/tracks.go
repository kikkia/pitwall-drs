package api

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"sync"

	"f1sockets/model"
)

var (
	tracks     []model.Track
	tracksOnce sync.Once
	tracksErr  error
)

func loadTracks() {
	tracksOnce.Do(func() {
		file, err := os.Open("data/tracks.json")
		if err != nil {
			tracksErr = fmt.Errorf("failed to open tracks file: %w", err)
			return
		}
		defer file.Close()

		bytes, err := io.ReadAll(file)
		if err != nil {
			tracksErr = fmt.Errorf("failed to read tracks file: %w", err)
			return
		}

		if err := json.Unmarshal(bytes, &tracks); err != nil {
			tracksErr = fmt.Errorf("failed to unmarshal tracks data: %w", err)
		}
	})
}

// HandleTrack handles requests for a single track's data by its ID, name, or location.
func HandleTrack(w http.ResponseWriter, r *http.Request) {
	loadTracks()

	w.Header().Set("Access-Control-Allow-Origin", "*")
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "public, max-age=1209600")

	if tracksErr != nil {
		http.Error(w, tracksErr.Error(), http.StatusInternalServerError)
		return
	}

	id := strings.TrimPrefix(r.URL.Path, "/track/")
	for _, track := range tracks {
		if strings.EqualFold(track.ID, id) || strings.EqualFold(track.Name, id) || strings.EqualFold(track.Location, id) {
			if err := json.NewEncoder(w).Encode(track); err != nil {
				fmt.Printf("Error encoding track data for id %s: %v\n", id, err)
			}
			return
		}
	}

	http.NotFound(w, r)
}
