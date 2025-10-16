package main

import (
    "fmt"
    "io/ioutil"
    "bytes"
	"log"
	"time"

	"github.com/ericzheng0404/DistribCache/p2p"
)


type ServerOpts struct {
	DefaultTTLSeconds int64
}

func makeServer(listenAddr string, nodes []string, opts ServerOpts) *FileServer {
	tcpP2pTransportOpts := p2p.TCPTransportOpts{
		ListenAddress: listenAddr,
		HandShakeFunc: p2p.NOPHandShakeFunc,
		Decoder:       p2p.DefaultDecoder{},
	}
	tcpTransport := p2p.NewTCPTransport(tcpP2pTransportOpts)

	fileServerOpts := FileServerOpts{
		EncKey:            newEncryptionKey(),
		StorageRoot:       listenAddr + "_network",
		PathTransformFunc: CASPathTransformFunc,
		Transport:         tcpTransport,
		BootstrapNodes:    nodes,
		DefaultTTLSeconds: opts.DefaultTTLSeconds,
	}

	s := NewFileServer(fileServerOpts)
	tcpTransport.OnPeer = s.OnPeer
	return s
}

func main() {
	opts1 := ServerOpts{}
	s1 := makeServer(":3000", []string{}, opts1)

	go func() {
		log.Fatal(s1.Start())
	}()

	time.Sleep(1 * time.Second)

	opts2 := ServerOpts{DefaultTTLSeconds: 5}
	s2 := makeServer(":4000", []string{":3000"}, opts2)

	go func() {
		log.Fatal(s2.Start())
	}()

	time.Sleep(1 * time.Second)

	// Store a file with a 2-second TTL
	key := "private.txt"
	data := bytes.NewReader([]byte("this is a private file"))
	if err := s2.Store(key, data, 2); err != nil {
		log.Fatalf("Failed to store file: %v", err)
	}

	fmt.Printf("Stored %s with 2s TTL\n", key)

	// Wait for TTL to expire
	time.Sleep(3 * time.Second)

	// Try to get the file, should fail
	_, err := s2.Get(key)
	if err == nil {
		log.Fatalf("Expected file to be expired, but Get succeeded")
	}
	fmt.Printf("Get failed as expected for expired key %s\n", key)

	// Store a file with infinite TTL
	keyInfinite := "permanent.txt"
	dataInfinite := bytes.NewReader([]byte("this file should not expire"))
	if err := s2.Store(keyInfinite, dataInfinite, 0); err != nil {
		log.Fatalf("Failed to store file with infinite TTL: %v", err)
	}
	fmt.Printf("Stored %s with infinite TTL\n", keyInfinite)

	time.Sleep(2 * time.Second)

	// Get should succeed
	r, err := s2.Get(keyInfinite)
	if err != nil {
		log.Fatalf("Failed to get file with infinite TTL: %v", err)
	}
	b, _ := ioutil.ReadAll(r)
	fmt.Printf("Got file with infinite TTL: %s -> %s\n", keyInfinite, string(b))
}