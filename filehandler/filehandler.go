package filehandler

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Gzip the file if not yet zipped and send it
func ServeFileWithCompression(w http.ResponseWriter, r *http.Request, safePath string) {
	gzipPath := safePath + ".gz"

	if strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		// Try to serve pre-compressed
		if _, err := os.Stat(gzipPath); err == nil {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Vary", "Accept-Encoding")
			setCacheHeaders(w, r, safePath)
			http.ServeFile(w, r, gzipPath)
			return
		}

		// Create compressed version on first request
		if err := createCompressedFile(safePath, gzipPath); err == nil {
			w.Header().Set("Content-Encoding", "gzip")
			w.Header().Set("Content-Type", "text/plain; charset=utf-8")
			w.Header().Set("Vary", "Accept-Encoding")
			setCacheHeaders(w, r, safePath)
			http.ServeFile(w, r, gzipPath)
			return
		}
	}

	// Serve uncompressed
	setCacheHeaders(w, r, safePath)
	http.ServeFile(w, r, safePath)
}

func createCompressedFile(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dst.Close()

	gzipWriter := gzip.NewWriter(dst)
	defer gzipWriter.Close()

	_, err = io.Copy(gzipWriter, src)
	return err
}

func setCacheHeaders(w http.ResponseWriter, r *http.Request, path string) {
	fileInfo, err := os.Stat(path)
	if err != nil {
		return
	}
	etag := fmt.Sprintf(`"%x-%x"`, fileInfo.ModTime().Unix(), fileInfo.Size())
	w.Header().Set("Cache-Control", "public, max-age=31536000, immutable")
	w.Header().Set("ETag", etag)
	w.Header().Set("Last-Modified", fileInfo.ModTime().UTC().Format(http.TimeFormat))
	if match := r.Header.Get("If-None-Match"); match != "" {
		if strings.Contains(match, etag) {
			w.WriteHeader(http.StatusNotModified)
			return
		}
	}
}

// http handler for the actual HTTP endpoint
func RecordingsFileHandler() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasSuffix(strings.ToLower(r.URL.Path), ".txt") {
			http.NotFound(w, r)
			return
		}

		cleanPath := filepath.Clean(r.URL.Path)
		if strings.Contains(cleanPath, "..") {
			http.NotFound(w, r)
			return
		}

		safePath := filepath.Join("recordings", cleanPath)

		absRecordings, _ := filepath.Abs("recordings")
		absSafePath, _ := filepath.Abs(safePath)
		if !strings.HasPrefix(absSafePath, absRecordings) {
			http.NotFound(w, r)
			return
		}

		if _, err := os.Stat(safePath); os.IsNotExist(err) {
			http.NotFound(w, r)
			return
		}

		ServeFileWithCompression(w, r, safePath)
	})
}

func HandleRecordings(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	recordings := make(map[string][]string)
	recordingsRoot := "recordings"

	err := filepath.Walk(recordingsRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		if !info.IsDir() && strings.HasSuffix(info.Name(), ".txt") {
			dir := filepath.Dir(path)
			// Use ToSlash for consistent path separators in the JSON output
			dir = filepath.ToSlash(dir)
			recordings[dir] = append(recordings[dir], filepath.ToSlash(path))
		}
		return nil
	})

	if err != nil {
		http.Error(w, "Failed to read recordings directory", http.StatusInternalServerError)
		fmt.Printf("Error walking recordings directory: %v\n", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Access-Control-Allow-Origin", "*")

	if err := json.NewEncoder(w).Encode(recordings); err != nil {
		http.Error(w, "Failed to encode recordings to JSON", http.StatusInternalServerError)
		fmt.Printf("Error encoding recordings JSON: %v\n", err)
	}
}
