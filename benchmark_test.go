package main

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/ericzheng0404/DistribCache/p2p"
)

func newFileServer(listenAddr string, nodes []string) *FileServer {
	tcpTransportOpts := p2p.TCPTransportOpts{
		ListenAddress: listenAddr,
		HandShakeFunc: p2p.NOPHandShakeFunc,
		Decoder:       p2p.DefaultDecoder{},
	}
	tcpTransport := p2p.NewTCPTransport(tcpTransportOpts)

	fsOpts := FileServerOpts{
		StorageRoot:       listenAddr + "_network",
		PathTransformFunc: CASPathTransformFunc,
		Transport:         tcpTransport,
		BootstrapNodes:    nodes,
	}

	s := NewFileServer(fsOpts)
	tcpTransport.OnPeer = s.OnPeer
	return s
}

func setupTestNetwork(t testing.TB, numNodes int) ([]*FileServer, func()) {
	servers := make([]*FileServer, numNodes)
	bootstrapNode := ":5000"

	for i := 0; i < numNodes; i++ {
		listenAddr := fmt.Sprintf(":500%d", i)
		var nodes []string
		if i != 0 {
			nodes = []string{bootstrapNode}
		}

		server := newFileServer(listenAddr, nodes)
		servers[i] = server
		go func(s *FileServer) {
			if err := s.Start(); err != nil {
				t.Errorf("server failed to start: %v", err)
			}
		}(server)
	}

	sleepDuration := time.Duration(numNodes/2) * time.Second
	if sleepDuration < 2*time.Second {
		sleepDuration = 2 * time.Second
	}
	time.Sleep(sleepDuration)

	cleanup := func() {
		for _, server := range servers {
			server.Stop()
			if err := server.store.Clear(); err != nil {
				t.Errorf("failed to clear storage: %v", err)
			}
		}
	}

	return servers, cleanup
}

func generateRandomData(size int) []byte {
	data := make([]byte, size)
	// Not truly random, but good enough for benchmarks
	for i := 0; i < size; i++ {
		data[i] = byte(i)
	}
	return data
}

func generateKey(i int) string {
	return fmt.Sprintf("testfile_%d", i)
}

func benchmarkStore(b *testing.B, numNodes, fileSize int) {
	servers, cleanup := setupTestNetwork(b, numNodes)
	defer cleanup()

	b.SetBytes(int64(fileSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		key := generateKey(i)
		data := generateRandomData(fileSize)
		if err := servers[0].Store(key, bytes.NewReader(data), 0); err != nil {
			b.Fatalf("store failed: %v", err)
		}
	}
}

func benchmarkGet(b *testing.B, numNodes, fileSize int) {
	servers, cleanup := setupTestNetwork(b, numNodes)
	defer cleanup()

	// Store a single file to be fetched in the benchmark
	key := "static_file"
	data := generateRandomData(fileSize)
	if err := servers[0].Store(key, bytes.NewReader(data), 0); err != nil {
		b.Fatalf("store for get benchmark failed: %v", err)
	}

	sleepDuration := time.Duration(numNodes/2) * time.Second
	if sleepDuration < 1*time.Second {
		sleepDuration = 1 * time.Second
	}
	time.Sleep(sleepDuration) // allow time for propagation

	b.SetBytes(int64(fileSize))
	b.ResetTimer()

	for i := 0; i < b.N; i++ {
		// Alternate between getting from the source node and another node
		serverIndex := i % numNodes
		if _, err := servers[serverIndex].Get(key); err != nil {
			b.Fatalf("get failed from server %d: %v", serverIndex, err)
		}
	}
}

func BenchmarkStore_1Node_1KB(b *testing.B)    { benchmarkStore(b, 1, 1024) }
func BenchmarkStore_5Nodes_1KB(b *testing.B)    { benchmarkStore(b, 5, 1024) }
func BenchmarkStore_10Nodes_1KB(b *testing.B)   { benchmarkStore(b, 10, 1024) }
func BenchmarkStore_20Nodes_1KB(b *testing.B)   { benchmarkStore(b, 20, 1024) }

func BenchmarkStore_1Node_10KB(b *testing.B)   { benchmarkStore(b, 1, 10240) }
func BenchmarkStore_5Nodes_10KB(b *testing.B)   { benchmarkStore(b, 5, 10240) }
func BenchmarkStore_10Nodes_10KB(b *testing.B)  { benchmarkStore(b, 10, 10240) }
func BenchmarkStore_20Nodes_10KB(b *testing.B)  { benchmarkStore(b, 20, 10240) }

func BenchmarkStore_1Node_100KB(b *testing.B)  { benchmarkStore(b, 1, 102400) }
func BenchmarkStore_5Nodes_100KB(b *testing.B)  { benchmarkStore(b, 5, 102400) }
func BenchmarkStore_10Nodes_100KB(b *testing.B) { benchmarkStore(b, 10, 102400) }
func BenchmarkStore_20Nodes_100KB(b *testing.B) { benchmarkStore(b, 20, 102400) }

func BenchmarkStore_1Node_1MB(b *testing.B)     { benchmarkStore(b, 1, 1024*1024) }
func BenchmarkStore_5Nodes_1MB(b *testing.B)    { benchmarkStore(b, 5, 1024*1024) }
func BenchmarkStore_10Nodes_1MB(b *testing.B)   { benchmarkStore(b, 10, 1024*1024) }
func BenchmarkStore_20Nodes_1MB(b *testing.B)   { benchmarkStore(b, 20, 1024*1024) }

func BenchmarkGet_1Node_1KB(b *testing.B)       { benchmarkGet(b, 1, 1024) }
func BenchmarkGet_5Nodes_1KB(b *testing.B)       { benchmarkGet(b, 5, 1024) }
func BenchmarkGet_10Nodes_1KB(b *testing.B)      { benchmarkGet(b, 10, 1024) }
func BenchmarkGet_20Nodes_1KB(b *testing.B)      { benchmarkGet(b, 20, 1024) }

func BenchmarkGet_1Node_10KB(b *testing.B)      { benchmarkGet(b, 1, 10240) }
func BenchmarkGet_5Nodes_10KB(b *testing.B)      { benchmarkGet(b, 5, 10240) }
func BenchmarkGet_10Nodes_10KB(b *testing.B)     { benchmarkGet(b, 10, 10240) }
func BenchmarkGet_20Nodes_10KB(b *testing.B)     { benchmarkGet(b, 20, 10240) }

func BenchmarkGet_1Node_100KB(b *testing.B)     { benchmarkGet(b, 1, 102400) }
func BenchmarkGet_5Nodes_100KB(b *testing.B)     { benchmarkGet(b, 5, 102400) }
func BenchmarkGet_10Nodes_100KB(b *testing.B)    { benchmarkGet(b, 10, 102400) }
func BenchmarkGet_20Nodes_100KB(b *testing.B)    { benchmarkGet(b, 20, 102400) }

func BenchmarkGet_1Node_1MB(b *testing.B)        { benchmarkGet(b, 1, 1024*1024) }
func BenchmarkGet_5Nodes_1MB(b *testing.B)       { benchmarkGet(b, 5, 1024*1024) }
func BenchmarkGet_10Nodes_1MB(b *testing.B)      { benchmarkGet(b, 10, 1024*1024) }
func BenchmarkGet_20Nodes_1MB(b *testing.B)      { benchmarkGet(b, 20, 1024*1024) }
