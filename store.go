package main

import (
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// MetaData holds the expiry information for a key.
type MetaData struct {
	Expiry int64 `json:"expiry"`
}

// KeyExpiry represents a key and its expiry time.
type KeyExpiry struct {
	Key    string
	Expiry int64
}

// getDefaultRootFolder returns the default storage directory.
func getDefaultRootFolder() (string, error) {
	defaultRootFolderName := "Storage"
	dir, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("failed to get current working directory: %w", err)
	}
	return fmt.Sprintf("%s/%s", dir, defaultRootFolderName), nil
}

// CASPathTransformFunc transforms a key into a PathKey using SHA1 hashing.
func CASPathTransformFunc(key string) PathKey {
	// Implementing key transformation using SHA1.
	hash := sha1.Sum([]byte(key))
	hashString := hex.EncodeToString(hash[:]) // Convert to slice [:]
	blockSize := 5                            // Depth for block
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

// PathKey represents the transformed path and file name.
type PathKey struct {
	PathName string
	FileName string
}

// FirstPathName returns the first segment of the path name.
func (p PathKey) FirstPathName() string {
	paths := strings.Split(p.PathName, "/")
	if len(paths) == 0 {
		return ""
	}
	return paths[0]
}

// PathTransformFunc defines a function type for transforming keys into PathKeys.
type PathTransformFunc func(key string) PathKey

// FullPath constructs the full path for the file.
func (p PathKey) FullPath() string {
	return fmt.Sprintf("%s/%s", p.PathName, p.FileName)
}

// MetaPath constructs the full path for the metadata file.
func (p PathKey) MetaPath() string {
	return fmt.Sprintf("%s/%s.meta", p.PathName, p.FileName)
}

// StoreOpts contains options for creating a Store.
type StoreOpts struct {
	Root              string // Root directory for storing files.
	PathTransformFunc PathTransformFunc
}

// DefaultPathTransformFunc is the default transformation function.
var DefaultPathTransformFunc = func(key string) PathKey {
	return PathKey{
		PathName: key,
		FileName: key,
	}
}

// Store is responsible for managing file storage.
type Store struct {
	StoreOpts
}

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
	return &Store{
		StoreOpts: storeOpts,
	}
}

// HasKey checks if a key exists in the store and is not expired.
func (s *Store) HasKey(key string) bool {
	pathKey := s.PathTransformFunc(key)
	fullPath := pathKey.FullPath()
	fullPathWithRoot := fmt.Sprintf("%s/%s", s.Root, fullPath)

	_, err := os.Stat(fullPathWithRoot)
	if errors.Is(err, os.ErrNotExist) {
		return false
	}

	// Check for expiry
	meta, err := s.readMetaData(key)
	if err != nil {
		return true // No meta file, treat as non-expiring
	}

	if meta.isExpired() {
		s.DeleteLocal(key) // Delete if expired
		return false
	}

	return true
}

// Clear removes all files in the store's root directory.
func (s *Store) Clear() error {
	return os.RemoveAll(s.Root)
}

// Delete removes the file associated with the given key.
func (s *Store) Delete(key string) error {
	return s.DeleteLocal(key)
}

// DeleteLocal removes the file and its metadata associated with the given key.
func (s *Store) DeleteLocal(key string) error {
	pathKey := s.PathTransformFunc(key)

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

	log.Printf("TTL expired, key=%s", pathKey.FileName)
	return nil
}

// Read retrieves the file associated with the given key.
func (s *Store) Read(key string) (int64, io.Reader, error) {
	return s.readStream(key)
}

// readStream opens a stream for reading the file associated with the key.
func (s *Store) readStream(key string) (int64, io.ReadCloser, error) {
	pathKey := s.PathTransformFunc(key)
	pathAndFilename := pathKey.FullPath()
	fullPathWithRoot := fmt.Sprintf("%s/%s", s.Root, pathAndFilename)

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

// Write stores the content from the provided reader associated with the key.
func (s *Store) Write(key string, r io.Reader, ttl int64) (int64, error) {
	return s.writeStream(key, r, ttl)
}

// WriteDecrypt stores the content from the provided reader associated with the key after decryption.
func (s *Store) WriteDecrypt(encKey []byte, key string, r io.Reader, ttl int64) (int64, error) {
	pathKey := s.PathTransformFunc(key)
	pathNameWithRoot := fmt.Sprintf("%s/%s", s.Root, pathKey.PathName)

	if err := os.MkdirAll(pathNameWithRoot, os.ModePerm); err != nil {
		return 0, fmt.Errorf("error creating directory %s: %w", pathNameWithRoot, err)
	}

	pathAndFilename := pathKey.FullPath()
	fullPathAndFilenameWithRoot := fmt.Sprintf("%s/%s", s.Root, pathAndFilename)
	f, err := os.Create(fullPathAndFilenameWithRoot)
	if err != nil {
		return 0, fmt.Errorf("error creating file %s: %w", fullPathAndFilenameWithRoot, err)
	}
	defer f.Close()

	n, err := copyDecrypt(encKey, r, f)
	if err != nil {
		return 0, fmt.Errorf("error copying decrypted content to file: %w", err)
	}

	if ttl > 0 {
		if err := s.writeMetaData(key, time.Now().Unix()+ttl); err != nil {
			return 0, fmt.Errorf("failed to write metadata for key %s: %w", key, err)
		}
	}

	fmt.Printf("Written (%d) bytes to disk: %s\n", n, fullPathAndFilenameWithRoot)
	return int64(n), nil
}

// writeStream writes the content from the provided reader associated with the key.
func (s *Store) writeStream(key string, r io.Reader, ttl int64) (int64, error) {
	pathKey := s.PathTransformFunc(key)
	pathNameWithRoot := fmt.Sprintf("%s/%s", s.Root, pathKey.PathName)

	if err := os.MkdirAll(pathNameWithRoot, os.ModePerm); err != nil {
		return 0, fmt.Errorf("error creating directory %s: %w", pathNameWithRoot, err)
	}

	pathAndFilename := pathKey.FullPath()
	fullPathAndFilenameWithRoot := fmt.Sprintf("%s/%s", s.Root, pathAndFilename)
	f, err := os.Create(fullPathAndFilenameWithRoot)
	if err != nil {
		return 0, fmt.Errorf("error creating file %s: %w", fullPathAndFilenameWithRoot, err)
	}
	defer f.Close()

	n, err := io.Copy(f, r)
	if err != nil {
		return 0, fmt.Errorf("error writing to file %s: %w", fullPathAndFilenameWithRoot, err)
	}

	if ttl > 0 {
		if err := s.writeMetaData(key, time.Now().Unix()+ttl); err != nil {
			return 0, fmt.Errorf("failed to write metadata for key %s: %w", key, err)
		}
	}

	return n, nil
}

// ListKeys scans the directory tree and returns keys with their expiry.
func (s *Store) ListKeys() ([]KeyExpiry, error) {
	var keys []KeyExpiry
	err := filepath.Walk(s.Root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if !info.IsDir() && !strings.HasSuffix(info.Name(), ".meta") {
			// Assumes the filename is the key. This might need adjustment
			// based on the actual key-to-filename mapping.
			key := info.Name()
			meta, err := s.readMetaData(key)
			if err != nil {
				// No meta file, treat as non-expiring
				keys = append(keys, KeyExpiry{Key: key, Expiry: 0})
			} else {
				keys = append(keys, KeyExpiry{Key: key, Expiry: meta.Expiry})
			}
		}
		return nil
	})
	return keys, err
}

func (s *Store) writeMetaData(key string, expiry int64) error {
	pathKey := s.PathTransformFunc(key)
	metaPath := fmt.Sprintf("%s/%s", s.Root, pathKey.MetaPath())

	meta := MetaData{Expiry: expiry}
	data, err := json.Marshal(meta)
	if err != nil {
		return fmt.Errorf("failed to marshal metadata: %w", err)
	}

	return os.WriteFile(metaPath, data, 0644)
}

func (s *Store) readMetaData(key string) (*MetaData, error) {
	pathKey := s.PathTransformFunc(key)
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

func (m *MetaData) isExpired() bool {
	return m.Expiry > 0 && time.Now().Unix() > m.Expiry
}