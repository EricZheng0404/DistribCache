/*
This file contains the implementation of the Store struct and its methods for 
managing file storage with optional TTL (Time To Live) functionality. 
The Store uses a path transformation function to determine how keys are mapped 
to file paths, and it handles metadata for expiry of keys.
*/
package main

import (
	"bytes"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"strings"
	"sync"
	"time"
)

// ============================================================================
// Type Definitions
// ============================================================================

// MetaData holds the expiry information for a key.
type MetaData struct {
	Expiry int64 `json:"expiry"`
}

// KeyExpiry represents a key and its expiry time.
type KeyExpiry struct {
	Key    string
	Expiry int64
}

// PathKey represents the transformed path and file name.
type PathKey struct {
	PathName string
	FileName string
}

// PathTransformFunc defines a function type for transforming keys into PathKeys.
// This allows for flexible key-to-path mapping strategies.
type PathTransformFunc func(key string) PathKey

// StoreOpts contains options for creating a Store.
type StoreOpts struct {
	Root              string // Root directory for storing files.
	PathTransformFunc PathTransformFunc
}

// Store is responsible for managing file storage.
type Store struct {
	StoreOpts
	keyIndex map[string]string // maps user-supplied key → SHA1 content hash
	keyMu    sync.RWMutex
}

// ============================================================================
// PathKey Methods
// ============================================================================

// FirstPathName returns the first segment of the path name.
func (p PathKey) FirstPathName() string {
	paths := strings.Split(p.PathName, "/")
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

// FullPath constructs the full path for the file.
func (p PathKey) FullPath() string {
	return fmt.Sprintf("%s/%s", p.PathName, p.FileName)
}

// MetaPath constructs the full path for the metadata file.
func (p PathKey) MetaPath() string {
	return fmt.Sprintf("%s/%s.meta", p.PathName, p.FileName)
}

// ============================================================================
// MetaData Methods
// ============================================================================

// isExpired checks if the metadata has expired.
func (m *MetaData) isExpired() bool {
	return m.Expiry > 0 && time.Now().Unix() > m.Expiry
}

// ============================================================================
// Path Transform Functions
// ============================================================================

// CASPathTransformFunc splits an already-computed SHA1 hex digest into a
// hierarchical directory PathKey (e.g. "abcde/fghij/..."). The SHA1 hash
// itself is computed from file *content* in writeStream/WriteDecrypt,
// enabling true content-addressable storage where identical content always
// maps to the same address regardless of the user-supplied key.
func CASPathTransformFunc(hashString string) PathKey {
	blockSize := 5
	sliceLength := len(hashString) / blockSize

	paths := make([]string, sliceLength)
	for i := 0; i < sliceLength; i++ {
		from, to := i*blockSize, (i+1)*blockSize
		paths[i] = hashString[from:to]
	}

	return PathKey{
		PathName: strings.Join(paths, "/"),
		FileName: hashString,
	}
}

// DefaultPathTransformFunc is the default transformation function.
var DefaultPathTransformFunc = func(key string) PathKey {
	return PathKey{
		PathName: key,
		FileName: key,
	}
}

// ============================================================================
// Store Constructor
// ============================================================================

// NewStore creates a new Store instance with the provided options.
func NewStore(storeOpts StoreOpts) *Store {
	if storeOpts.PathTransformFunc == nil {
		storeOpts.PathTransformFunc = DefaultPathTransformFunc
	}
	if len(storeOpts.Root) == 0 {
		var err error
		storeOpts.Root, err = getDefaultRootFolder()
		if err != nil {
			return nil // Handle error appropriately
		}
	}
	s := &Store{
		StoreOpts: storeOpts,
		keyIndex:  make(map[string]string),
	}
	s.loadKeyIndex() // restore key→contentHash mappings from a previous run
	return s
}

// ============================================================================
// Public Store Methods
// ============================================================================

// HasKey checks if a key exists in the store and is not expired.
func (s *Store) HasKey(key string) bool {
	contentHash, ok := s.resolveContentHash(key)
	if !ok {
		return false
	}
	pathKey := s.PathTransformFunc(contentHash)
	fullPathWithRoot := fmt.Sprintf("%s/%s", s.Root, pathKey.FullPath())

	// Check if the file exists
	_, err := os.Stat(fullPathWithRoot)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}

	// Check for expiry
	meta, err := s.readMetaData(key)
	if err != nil {
		return true // No meta file, treat as non-expiring
	}

	// If the meta file exists, check if it's expired
	// If expired, delete the file and meta, and return false
	if meta.isExpired() {
		s.DeleteLocal(key)
		return false
	}

	return true
}

// GetContentHash returns the SHA1 content hash for a given key (used by the HTTP API).
func (s *Store) GetContentHash(key string) (string, bool) {
	return s.resolveContentHash(key)
}

// Read retrieves the file associated with the given key.
func (s *Store) Read(key string) (int64, io.Reader, error) {
	return s.readStream(key)
}

// Write stores the content from the provided reader associated with the key.
func (s *Store) Write(key string, r io.Reader, ttl int64) (int64, error) {
	return s.writeStream(key, r, ttl)
}

// WriteDecrypt decrypts the incoming stream, hashes the plaintext content,
// and stores it at the content-addressed path.
func (s *Store) WriteDecrypt(encKey []byte, key string, r io.Reader, ttl int64) (int64, error) {
	// Decrypt into a buffer so we can hash the plaintext before writing.
	var decBuf bytes.Buffer
	n, err := copyDecrypt(encKey, r, &decBuf)
	if err != nil {
		return 0, fmt.Errorf("error decrypting content for key %s: %w", key, err)
	}

	// Hash the decrypted (canonical) content.
	hasher := sha1.New()
	hasher.Write(decBuf.Bytes())
	contentHash := hex.EncodeToString(hasher.Sum(nil))

	// Derive storage path from content hash.
	pathKey := s.PathTransformFunc(contentHash)
	pathNameWithRoot := fmt.Sprintf("%s/%s", s.Root, pathKey.PathName)
	if err := os.MkdirAll(pathNameWithRoot, os.ModePerm); err != nil {
		return 0, fmt.Errorf("error creating directory %s: %w", pathNameWithRoot, err)
	}

	fullPath := fmt.Sprintf("%s/%s", s.Root, pathKey.FullPath())
	f, err := os.Create(fullPath)
	if err != nil {
		return 0, fmt.Errorf("error creating file %s: %w", fullPath, err)
	}
	defer f.Close()

	if _, err := io.Copy(f, &decBuf); err != nil {
		return 0, fmt.Errorf("error writing decrypted content to %s: %w", fullPath, err)
	}

	// Register key → content hash.
	s.keyMu.Lock()
	s.keyIndex[key] = contentHash
	s.keyMu.Unlock()
	if err := s.saveKeyIndex(); err != nil {
		log.Printf("warning: failed to persist key index: %v", err)
	}

	if ttl > 0 {
		if err := s.writeMetaData(key, time.Now().Unix()+ttl); err != nil {
			return 0, fmt.Errorf("failed to write metadata for key %s: %w", key, err)
		}
	}

	fmt.Printf("Written (%d) bytes to disk: %s\n", n, fullPath)
	return int64(n), nil
}

// Delete removes the file associated with the given key.
func (s *Store) Delete(key string) error {
	return s.DeleteLocal(key)
}

// DeleteLocal removes the file and its metadata associated with the given key.
func (s *Store) DeleteLocal(key string) error {
	contentHash, ok := s.resolveContentHash(key)
	if !ok {
		return nil // already gone
	}
	pathKey := s.PathTransformFunc(contentHash)

	// Delete data file
	dataPath := fmt.Sprintf("%s/%s", s.Root, pathKey.FullPath())
	if err := os.Remove(dataPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete data file %s: %w", dataPath, err)
	}

	// Delete meta file
	metaPath := fmt.Sprintf("%s/%s", s.Root, pathKey.MetaPath())
	if err := os.Remove(metaPath); err != nil && !os.IsNotExist(err) {
		return fmt.Errorf("failed to delete meta file %s: %w", metaPath, err)
	}

	// Remove from key index
	s.keyMu.Lock()
	delete(s.keyIndex, key)
	s.keyMu.Unlock()
	if err := s.saveKeyIndex(); err != nil {
		log.Printf("warning: failed to persist key index after deletion: %v", err)
	}

	log.Printf("deleted key=%s (hash=%s)", key, pathKey.FileName)
	return nil
}

// Clear removes all files in the store's root directory.
func (s *Store) Clear() error {
	return os.RemoveAll(s.Root)
}

// ListKeys returns all keys with their expiry by iterating the key index.
func (s *Store) ListKeys() ([]KeyExpiry, error) {
	// Snapshot the keys first to avoid holding the lock during readMetaData calls.
	s.keyMu.RLock()
	keys := make([]string, 0, len(s.keyIndex))
	for k := range s.keyIndex {
		keys = append(keys, k)
	}
	s.keyMu.RUnlock()

	var result []KeyExpiry
	for _, key := range keys {
		meta, err := s.readMetaData(key)
		if err != nil {
			// No meta file → non-expiring
			result = append(result, KeyExpiry{Key: key, Expiry: 0})
		} else {
			result = append(result, KeyExpiry{Key: key, Expiry: meta.Expiry})
		}
	}
	return result, nil
}

// ============================================================================
// Private Helper Methods
// ============================================================================

// getDefaultRootFolder returns the default storage directory.
func getDefaultRootFolder() (string, error) {
	defaultRootFolderName := "Storage"
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}
	return fmt.Sprintf("%s/%s", dir, defaultRootFolderName), nil
}

// keyIndexPath returns the path to the persistent key-index file.
func (s *Store) keyIndexPath() string {
	return fmt.Sprintf("%s/_keyindex.json", s.Root)
}

// loadKeyIndex loads the key→contentHash index from disk (called on startup).
func (s *Store) loadKeyIndex() {
	data, err := os.ReadFile(s.keyIndexPath())
	if err != nil {
		return // file doesn't exist yet; start fresh
	}
	s.keyMu.Lock()
	defer s.keyMu.Unlock()
	if err := json.Unmarshal(data, &s.keyIndex); err != nil {
		s.keyIndex = make(map[string]string)
	}
}

// saveKeyIndex persists the key→contentHash index to disk.
func (s *Store) saveKeyIndex() error {
	s.keyMu.RLock()
	data, err := json.Marshal(s.keyIndex)
	s.keyMu.RUnlock()
	if err != nil {
		return fmt.Errorf("failed to marshal key index: %w", err)
	}
	if err := os.MkdirAll(s.Root, os.ModePerm); err != nil {
		return err
	}
	return os.WriteFile(s.keyIndexPath(), data, 0644)
}

// resolveContentHash looks up the SHA1 content hash for a user-supplied key.
func (s *Store) resolveContentHash(key string) (string, bool) {
	s.keyMu.RLock()
	defer s.keyMu.RUnlock()
	hash, ok := s.keyIndex[key]
	return hash, ok
}

// readStream opens a stream for reading the file associated with the key.
func (s *Store) readStream(key string) (int64, io.ReadCloser, error) {
	contentHash, ok := s.resolveContentHash(key)
	if !ok {
		return 0, nil, fmt.Errorf("key not found: %s", key)
	}
	pathKey := s.PathTransformFunc(contentHash)
	fullPathWithRoot := fmt.Sprintf("%s/%s", s.Root, pathKey.FullPath())

	fi, err := os.Stat(fullPathWithRoot)
	if err != nil {
		return 0, nil, fmt.Errorf("error stating file %s: %w", fullPathWithRoot, err)
	}

	file, err := os.Open(fullPathWithRoot)
	if err != nil {
		return 0, nil, fmt.Errorf("error opening file %s: %w", fullPathWithRoot, err)
	}
	return fi.Size(), file, nil
}

// writeStream writes the content from the provided reader associated with the key.
// It buffers the full content, computes its SHA1 hash, and stores the file at the
// content-addressed path, registering the key→hash mapping in the key index.
func (s *Store) writeStream(key string, r io.Reader, ttl int64) (int64, error) {
	// Buffer content while simultaneously feeding it to the SHA1 hasher.
	var buf bytes.Buffer
	hasher := sha1.New()
	tee := io.TeeReader(r, &buf) // every byte read from tee is also written to buf
	if _, err := io.Copy(hasher, tee); err != nil {
		return 0, fmt.Errorf("error buffering content for key %s: %w", key, err)
	}
	contentHash := hex.EncodeToString(hasher.Sum(nil))

	// Derive the storage path from the content hash (not the key).
	pathKey := s.PathTransformFunc(contentHash)
	pathNameWithRoot := fmt.Sprintf("%s/%s", s.Root, pathKey.PathName)

	if err := os.MkdirAll(pathNameWithRoot, os.ModePerm); err != nil {
		return 0, fmt.Errorf("error creating directory %s: %w", pathNameWithRoot, err)
	}

	fullPath := fmt.Sprintf("%s/%s", s.Root, pathKey.FullPath())
	f, err := os.Create(fullPath)
	if err != nil {
		return 0, fmt.Errorf("error creating file %s: %w", fullPath, err)
	}
	defer f.Close()

	n, err := io.Copy(f, &buf)
	if err != nil {
		return 0, fmt.Errorf("error writing to file %s: %w", fullPath, err)
	}

	// Register key → content hash so lookups can find the file.
	s.keyMu.Lock()
	s.keyIndex[key] = contentHash
	s.keyMu.Unlock()
	if err := s.saveKeyIndex(); err != nil {
		log.Printf("warning: failed to persist key index: %v", err)
	}

	if ttl > 0 {
		if err := s.writeMetaData(key, time.Now().Unix()+ttl); err != nil {
			return 0, fmt.Errorf("failed to write metadata for key %s: %w", key, err)
		}
	}

	return n, nil
}

// readMetaData reads and returns the metadata for a key.
func (s *Store) readMetaData(key string) (*MetaData, error) {
	contentHash, ok := s.resolveContentHash(key)
	if !ok {
		return nil, fmt.Errorf("key not found in index: %s", key)
	}
	pathKey := s.PathTransformFunc(contentHash)
	metaPath := fmt.Sprintf("%s/%s", s.Root, pathKey.MetaPath())

	data, err := os.ReadFile(metaPath)
	if err != nil {
		return nil, err
	}

	var meta MetaData
	if err := json.Unmarshal(data, &meta); err != nil {
		return nil, fmt.Errorf("failed to unmarshal metadata: %w", err)
	}

	return &meta, nil
}

// writeMetaData writes metadata for a key with an expiry time.
func (s *Store) writeMetaData(key string, expiry int64) error {
	contentHash, ok := s.resolveContentHash(key)
	if !ok {
		return fmt.Errorf("key not found in index: %s", key)
	}
	pathKey := s.PathTransformFunc(contentHash)
	metaPath := fmt.Sprintf("%s/%s", s.Root, pathKey.MetaPath())

	meta := MetaData{Expiry: expiry}
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return os.WriteFile(metaPath, data, 0644)
}