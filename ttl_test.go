package main

import (
	"bytes"
	"fmt"
	"io/ioutil"
	"log"
	"os"
	"strings"
	"testing"
	"time"
)

func TestStoreWithTTL(t *testing.T) {
	opts := ServerOpts{}
	s := makeServer(":3000", []string{}, opts)
	go s.Start()
	defer s.Stop()

	key := "testfile"
	data := []byte("test data")
	ttl := 2 * time.Second

	err := s.Store(key, bytes.NewReader(data), int64(ttl.Seconds()))
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// 1. Local expiry
	time.Sleep(ttl + 1*time.Second)

	if s.store.HasKey(key) {
		t.Errorf("key %q should have been expired and deleted", key)
	}
}

func TestRemoteExpiry(t *testing.T) {
	// Setup server 1
	opts1 := ServerOpts{}
	s1 := makeServer(":3001", []string{}, opts1)
	go s1.Start()
	defer s1.Stop()

	// Setup server 2
	opts2 := ServerOpts{}
	s2 := makeServer(":4001", []string{":3001"}, opts2)
	go s2.Start()
	defer s2.Stop()

	time.Sleep(2 * time.Second) // allow time for connection

	key := "remotefile"
	data := []byte("remote data")
	ttl := 2 * time.Second

	// Store from s2, which will propagate to s1
	err := s2.Store(key, bytes.NewReader(data), int64(ttl.Seconds()))
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	// Both should have the key initially
	if !s1.store.HasKey(key) {
		t.Errorf("s1 should have key %q", key)
	}
	if !s2.store.HasKey(key) {
		t.Errorf("s2 should have key %q", key)
	}

	// Wait for expiry
	time.Sleep(ttl + 1*time.Second)

	// Both should not have the key
	if s1.store.HasKey(key) {
		t.Errorf("s1 should not have key %q after expiry", key)
	}
	if s2.store.HasKey(key) {
		t.Errorf("s2 should not have key %q after expiry", key)
	}
}

func TestNoPrematureEviction(t *testing.T) {
	opts := ServerOpts{}
	s := makeServer(":3002", []string{}, opts)
	go s.Start()
	defer s.Stop()

	key := "testfile"
	data := []byte("test data")
	ttl := 3 * time.Second

	err := s.Store(key, bytes.NewReader(data), int64(ttl.Seconds()))
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	time.Sleep(1 * time.Second)

	if !s.store.HasKey(key) {
		t.Errorf("key %q should not have been expired yet", key)
	}
}

func TestInfiniteTTL(t *testing.T) {
	opts := ServerOpts{}
	s := makeServer(":3003", []string{}, opts)
	go s.Start()
	defer s.Stop()

	key := "testfile"
	data := []byte("test data")

	err := s.Store(key, bytes.NewReader(data), 0) // 0 for infinite
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	time.Sleep(2 * time.Second)

	if !s.store.HasKey(key) {
		t.Errorf("key %q should not have been expired", key)
	}
}

func TestNegativeTTL(t *testing.T) {
	opts := ServerOpts{}
	s := makeServer(":3004", []string{}, opts)
	go s.Start()
	defer s.Stop()

	key := "testfile"
	data := []byte("test data")

	err := s.Store(key, bytes.NewReader(data), -1)
	if err == nil {
		t.Error("expected error for negative TTL, but got nil")
	}
}

func TestOfflineDeletion(t *testing.T) {
	tmpDir, err := ioutil.TempDir("", "ttl-test")
	if err != nil {
		t.Fatalf("failed to create temp dir: %v", err)
	}
	defer os.RemoveAll(tmpDir)

	opts := ServerOpts{}
	s := makeServer(":3005", []string{}, opts)
	s.StorageRoot = tmpDir
	go s.Start()

	key := "offline-key"
	data := []byte("some data")
	ttl := 1 * time.Second

	// Store the key and stop the server
	if err := s.Store(key, bytes.NewReader(data), int64(ttl.Seconds())); err != nil {
		t.Fatalf("failed to store key: %v", err)
	}
	s.Stop()

	// Wait for TTL to expire
	time.Sleep(ttl + 1*time.Second)

	// Create a new server instance with the same storage root
	s2 := makeServer(":3005", []string{}, opts)
	s2.StorageRoot = tmpDir

	// Capture log output
	var logBuf bytes.Buffer
	log.SetOutput(&logBuf)
	defer log.SetOutput(os.Stderr)

	go s2.Start()
	defer s2.Stop()
	time.Sleep(1 * time.Second) // Give it a moment to start and run the startup eviction

	if s2.store.HasKey(key) {
		t.Errorf("key %q should have been evicted on startup", key)
	}

	logOutput := logBuf.String()
	expectedLog := fmt.Sprintf("TTL expired, key=%s", CASPathTransformFunc(key).FileName)
	if !strings.Contains(logOutput, expectedLog) {
		t.Errorf("expected log output to contain %q, but it was %q", expectedLog, logOutput)
	}
}

func TestTTLWithOneSecond(t *testing.T) {
	opts := ServerOpts{}
	s := makeServer(":3006", []string{}, opts)
	go s.Start()
	defer s.Stop()

	key := "one-second-ttl"
	data := []byte("test data")
	ttl := 1 * time.Second

	err := s.Store(key, bytes.NewReader(data), int64(ttl.Seconds()))
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	time.Sleep(ttl + 1*time.Second)

	if s.store.HasKey(key) {
		t.Errorf("key %q should have been expired and deleted", key)
	}
}

func TestMultipleKeysWithDifferentTTLs(t *testing.T) {
	opts := ServerOpts{}
	s := makeServer(":3007", []string{}, opts)
	go s.Start()
	defer s.Stop()

	keys := []string{"key1", "key2", "key3"}
	ttls := []time.Duration{2 * time.Second, 4 * time.Second, 6 * time.Second}
	data := []byte("test data")

	for i, key := range keys {
		err := s.Store(key, bytes.NewReader(data), int64(ttls[i].Seconds()))
		if err != nil {
			t.Fatalf("Store() error = %v", err)
		}
	}

	time.Sleep(3 * time.Second)
	if !s.store.HasKey(keys[1]) {
		t.Errorf("key %q should not have been expired yet", keys[1])
	}
	if !s.store.HasKey(keys[2]) {
		t.Errorf("key %q should not have been expired yet", keys[2])
	}

	time.Sleep(4 * time.Second)
	if s.store.HasKey(keys[1]) {
		t.Errorf("key %q should have been expired and deleted", keys[1])
	}
	if s.store.HasKey(keys[2]) {
		t.Errorf("key %q should have been expired and deleted", keys[2])
	}
}

func TestUpdateTTL(t *testing.T) {
	opts := ServerOpts{}
	s := makeServer(":3008", []string{}, opts)
	go s.Start()
	defer s.Stop()

	key := "update-ttl"
	data := []byte("test data")
	ttl1 := 2 * time.Second
	ttl2 := 5 * time.Second

	err := s.Store(key, bytes.NewReader(data), int64(ttl1.Seconds()))
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	err = s.Store(key, bytes.NewReader(data), int64(ttl2.Seconds()))
	if err != nil {
		t.Fatalf("Store() error = %v", err)
	}

	time.Sleep(3 * time.Second)

	if !s.store.HasKey(key) {
		t.Errorf("key %q should not have been expired yet", key)
	}
}
