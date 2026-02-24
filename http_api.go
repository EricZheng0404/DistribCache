/*
This file adds a REST HTTP API and static frontend server to FileServer.

Endpoints:
  GET    /api/status        → node address, peer list, file count
  GET    /api/files         → list all files with hash, size, TTL
  POST   /api/files         → upload a file (multipart: key, ttl, file)
  GET    /api/files/{key}   → download a file
  DELETE /api/files/{key}   → delete a file
  GET    /                  → serves frontend/index.html
*/
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// ============================================================================
// JSON response types
// ============================================================================

// FileEntry is the API representation of a stored file.
type FileEntry struct {
	Key          string `json:"key"`
	ContentHash  string `json:"content_hash"`
	Size         int64  `json:"size"`
	Expiry       int64  `json:"expiry"`        // unix seconds; 0 = never
	TTLRemaining int64  `json:"ttl_remaining"` // seconds left; -1 = infinite
}

// StatusResponse is returned by GET /api/status.
type StatusResponse struct {
	Addr      string   `json:"addr"`
	Peers     []string `json:"peers"`
	FileCount int      `json:"file_count"`
}

// ============================================================================
// Handlers
// ============================================================================

func (s *FileServer) apiStatus(w http.ResponseWriter, r *http.Request) {
	s.peerLock.Lock()
	peers := make([]string, 0, len(s.peers))
	for addr := range s.peers {
		peers = append(peers, addr)
	}
	s.peerLock.Unlock()

	keys, _ := s.store.ListKeys()
	writeJSON(w, http.StatusOK, StatusResponse{
		Addr:      s.Transport.Addr(),
		Peers:     peers,
		FileCount: len(keys),
	})
}

func (s *FileServer) apiListFiles(w http.ResponseWriter, r *http.Request) {
	keys, err := s.store.ListKeys()
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	now := time.Now().Unix()
	entries := make([]FileEntry, 0, len(keys))
	for _, ke := range keys {
		hash, _ := s.store.GetContentHash(ke.Key)

		// Read just to get the file size, then close immediately.
		size, reader, err := s.store.Read(ke.Key)
		if err != nil {
			size = 0
		} else if rc, ok := reader.(io.Closer); ok {
			rc.Close()
		}

		ttlRemaining := int64(-1)
		if ke.Expiry > 0 {
			ttlRemaining = ke.Expiry - now
			if ttlRemaining < 0 {
				ttlRemaining = 0
			}
		}

		entries = append(entries, FileEntry{
			Key:          ke.Key,
			ContentHash:  hash,
			Size:         size,
			Expiry:       ke.Expiry,
			TTLRemaining: ttlRemaining,
		})
	}

	writeJSON(w, http.StatusOK, entries)
}

func (s *FileServer) apiUploadFile(w http.ResponseWriter, r *http.Request) {
	if err := r.ParseMultipartForm(64 << 20); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "invalid multipart form"})
		return
	}

	key := r.FormValue("key")
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required"})
		return
	}

	ttl := int64(0)
	if ttlStr := r.FormValue("ttl"); ttlStr != "" {
		v, err := strconv.ParseInt(ttlStr, 10, 64)
		if err != nil || v < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "ttl must be a non-negative integer (0 = infinite)"})
			return
		}
		ttl = v
	}

	file, _, err := r.FormFile("file")
	if err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "file field is required"})
		return
	}
	defer file.Close()

	if err := s.Store(key, file, ttl); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	hash, _ := s.store.GetContentHash(key)
	writeJSON(w, http.StatusOK, map[string]string{
		"key":          key,
		"content_hash": hash,
		"status":       "stored",
	})
}

func (s *FileServer) apiGetFile(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/api/files/")
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required"})
		return
	}

	reader, err := s.Get(key)
	if err != nil {
		writeJSON(w, http.StatusNotFound, map[string]string{"error": err.Error()})
		return
	}
	if rc, ok := reader.(io.ReadCloser); ok {
		defer rc.Close()
	}

	w.Header().Set("Content-Disposition", fmt.Sprintf("attachment; filename=%q", key))
	w.Header().Set("Content-Type", "application/octet-stream")
	io.Copy(w, reader)
}

func (s *FileServer) apiDeleteFile(w http.ResponseWriter, r *http.Request) {
	key := strings.TrimPrefix(r.URL.Path, "/api/files/")
	if key == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "key is required"})
		return
	}

	if err := s.store.Delete(key); err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	writeJSON(w, http.StatusOK, map[string]string{"status": "deleted", "key": key})
}

// apiConnectPeer dials a new peer at the given TCP address at runtime.
func (s *FileServer) apiConnectPeer(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Addr string `json:"addr"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil || body.Addr == "" {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "addr is required, e.g. \":4000\""})
		return
	}

	// Check if already connected
	s.peerLock.Lock()
	for addr := range s.peers {
		if strings.HasSuffix(addr, body.Addr) {
			s.peerLock.Unlock()
			writeJSON(w, http.StatusConflict, map[string]string{"error": "already connected to " + body.Addr})
			return
		}
	}
	s.peerLock.Unlock()

	if err := s.Transport.Dial(body.Addr); err != nil {
		writeJSON(w, http.StatusBadGateway, map[string]string{"error": fmt.Sprintf("dial %s: %v", body.Addr, err)})
		return
	}

	log.Printf("[API] Dialed new peer %s", body.Addr)
	writeJSON(w, http.StatusOK, map[string]string{"status": "connected", "addr": body.Addr})
}

// ============================================================================
// Server startup
// ============================================================================

// StartHTTPServer starts the REST API and static frontend on the given address.
func (s *FileServer) StartHTTPServer(addr string) error {
	mux := http.NewServeMux()

	// Static frontend (frontend/index.html)
	mux.Handle("/", http.FileServer(http.Dir("frontend")))

	// Peers
	mux.HandleFunc("/api/peers", cors(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			s.apiConnectPeer(w, r)
		} else {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	// Status
	mux.HandleFunc("/api/status", cors(s.apiStatus))

	// File collection: list + upload
	mux.HandleFunc("/api/files", cors(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.apiListFiles(w, r)
		case http.MethodPost:
			s.apiUploadFile(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	// File resource: download + delete
	mux.HandleFunc("/api/files/", cors(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet:
			s.apiGetFile(w, r)
		case http.MethodDelete:
			s.apiDeleteFile(w, r)
		default:
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}
	}))

	log.Printf("HTTP dashboard listening on http://localhost%s\n", addr)
	return http.ListenAndServe(addr, mux)
}

// ============================================================================
// Helpers
// ============================================================================

func cors(h http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Access-Control-Allow-Origin", "*")
		w.Header().Set("Access-Control-Allow-Methods", "GET, POST, DELETE, OPTIONS")
		w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
		if r.Method == http.MethodOptions {
			w.WriteHeader(http.StatusNoContent)
			return
		}
		h(w, r)
	}
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		log.Printf("error encoding JSON response: %v", err)
	}
}
