package main

import (
	"log"
	"os"
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
	// HTTP dashboard port: read from environment so PaaS hosts (Fly, Railway,
	// Render) can inject their own port; fall back to :8080 locally.
	httpPort := os.Getenv("PORT")
	if httpPort == "" {
		httpPort = "8080"
	}

	opts1 := ServerOpts{}
	s1 := makeServer(":3000", []string{}, opts1)
	go func() { log.Fatal(s1.Start()) }()

	time.Sleep(1 * time.Second)

	// Start the HTTP dashboard / REST API.
	go func() { log.Fatal(s1.StartHTTPServer(":" + httpPort)) }()

	// Second node — peers with s1 to demonstrate replication in the dashboard.
	opts2 := ServerOpts{DefaultTTLSeconds: 0}
	s2 := makeServer(":4000", []string{":3000"}, opts2)
	go func() { log.Fatal(s2.Start()) }()

	// Block forever; both nodes and the dashboard run in goroutines.
	select {}
}
