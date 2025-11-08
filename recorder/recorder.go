package recorder

import (
	"encoding/json"
	"f1sockets/model"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

// GlobalStateProvider is a function type that returns the current global state.
// It's used by the recorder to avoid a direct dependency or circular imports.
type GlobalStateProvider func() *model.GlobalState

// Recorder handles writing data stream to recording files.
type Recorder struct {
	buffer              []string
	mutex               sync.Mutex
	flushTicker         *time.Ticker
	stopChan            chan struct{}
	globalStateProvider GlobalStateProvider
	recordingEnabled    bool
}

func NewRecorder(flushInterval time.Duration, gsp GlobalStateProvider, recordingEnabled bool) *Recorder {
	return &Recorder{
		buffer:              make([]string, 0, 1000),
		flushTicker:         time.NewTicker(flushInterval),
		stopChan:            make(chan struct{}),
		globalStateProvider: gsp,
		recordingEnabled:    recordingEnabled,
	}
}

func (r *Recorder) Start() {
	if !r.recordingEnabled {
		return
	}
	go r.flushRoutine()
}

func (r *Recorder) Stop() {
	if !r.recordingEnabled {
		return
	}
	// a single select statement to ensure that the stopChan signal is not missed
	select {
	case <-r.stopChan:
		return
	default:
		close(r.stopChan)
		r.flushTicker.Stop()
		r.flush()
		r.generateAndSaveMetadata()
	}
}

// Record adds a message payload to the buffer to be written to a file.
func (r *Recorder) Record(payload []byte) {
	if !r.recordingEnabled {
		return
	}

	globalState := r.globalStateProvider()
	if globalState == nil || !globalState.IsSessionFinished() {
		logEntry := fmt.Sprintf("[%s] %s", time.Now().Format(time.RFC3339), payload)
		r.mutex.Lock()
		r.buffer = append(r.buffer, logEntry)
		r.mutex.Unlock()
	}
}

func (r *Recorder) flushRoutine() {
	for {
		select {
		case <-r.flushTicker.C:
			r.flush()
		case <-r.stopChan:
			return
		}
	}
}

func (r *Recorder) flush() {
	filePath := r.getRecordingFilePath()
	if filePath == "" {
		return
	}

	r.mutex.Lock()
	if len(r.buffer) == 0 {
		r.mutex.Unlock()
		return
	}

	toWrite := r.buffer
	r.buffer = make([]string, 0, 1000)
	r.mutex.Unlock()

	f, err := os.OpenFile(filePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		fmt.Printf("Recorder: Failed to open log file for appending: %v\n", err)
		return
	}
	defer f.Close()

	if _, err := f.WriteString(strings.Join(toWrite, "\n") + "\n"); err != nil {
		fmt.Printf("Recorder: Failed to write log batch: %v\n", err)
	}
}

func (r *Recorder) generateAndSaveMetadata() {
	if !r.recordingEnabled {
		return
	}

	globalState := r.globalStateProvider()
	if globalState == nil {
		return
	}

	recordingPath := r.getRecordingFilePath()
	if recordingPath == "" {
		return
	}

	metadata, err := GenerateMetadataFromState(globalState)
	if err != nil {
		fmt.Printf("Recorder: Failed to generate metadata for %s: %v\n", recordingPath, err)
		return
	}

	metadataPath := strings.TrimSuffix(recordingPath, ".txt") + ".meta.json"
	metadataBytes, err := json.MarshalIndent(metadata, "", "  ")
	if err != nil {
		fmt.Printf("Recorder: Failed to marshal metadata for %s: %v\n", recordingPath, err)
		return
	}

	if err := os.WriteFile(metadataPath, metadataBytes, 0644); err != nil {
		fmt.Printf("Recorder: Failed to write metadata file %s: %v\n", metadataPath, err)
	} else {
		fmt.Printf("Recorder: Successfully generated and saved metadata to %s\n", metadataPath)
	}
}

func (r *Recorder) getRecordingFilePath() string {
	globalState := r.globalStateProvider()
	if globalState == nil || globalState.R.SessionInfo == nil || globalState.R.SessionInfo.Path == "" {
		return ""
	}

	formattedPath := formatSessionPath(globalState.R.SessionInfo.Path)
	fullPath := "recordings/" + formattedPath + ".txt"

	dir := filepath.Dir(fullPath)
	if err := os.MkdirAll(dir, 0755); err != nil {
		fmt.Printf("Recorder: Error creating directory %s: %v\n", dir, err)
		return ""
	}

	return fullPath
}

// takes a session path like "2025/2025-06-15_Canadian_Grand_Prix/2025-06-13_Practice_1/"
// and transforms it to "2025/Canadian_Grand_Prix/Practice_1" for a cleaner file path.
func formatSessionPath(sessionPath string) string {
	parts := strings.Split(sessionPath, "/")
	if len(parts) == 0 {
		return ""
	}

	cleanedParts := []string{parts[0]}

	for i := 1; i < len(parts); i++ {
		part := parts[i]
		if part == "" {
			continue
		}

		if len(part) >= 11 && part[4] == '-' && part[7] == '-' && part[10] == '_' {
			cleanedParts = append(cleanedParts, part[11:])
		} else {
			cleanedParts = append(cleanedParts, part)
		}
	}

	return strings.Join(cleanedParts, "/")
}
