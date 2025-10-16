package p2p

import (
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
)

// TCPPeer represents a remote node over an established TCP connection.
type TCPPeer struct {
	net.Conn      // The underlying TCP connection.
	outbound     bool          // True if this peer was initiated by us (outbound), false if accepted (inbound).
	wg           *sync.WaitGroup // WaitGroup to synchronize goroutines.
}

// Addr returns the address the transport is accepting connections from.
func (t *TCPTransport) Addr() string {
	return t.ListenAddress
}

// TCPTransportOpts holds options for TCPTransport.
type TCPTransportOpts struct {
	ListenAddress string        // Address to listen for incoming connections.
	HandShakeFunc HandShakeFunc // Function to handle handshake with peers.
	Decoder       Decoder       // Decoder for incoming messages.
	OnPeer        func(Peer) error // Callback for new peers.
}

// NewTCPPeer creates a new TCPPeer instance.
func NewTCPPeer(conn net.Conn, outbound bool) *TCPPeer {
	return &TCPPeer{
		Conn:     conn,
		outbound: outbound,
		wg:      &sync.WaitGroup{},
	}
}

// CloseStream signals that the stream is closing.
func (p *TCPPeer) CloseStream() {
	p.wg.Done()
}

// Send sends a message to the peer.
func (p *TCPPeer) Send(msg []byte) error {
	_, err := p.Conn.Write(msg)
	return err
}

// Consume returns a read-only channel for incoming messages.
func (t *TCPTransport) Consume() <-chan RPC {
	return t.rpcch
}

// TCPTransport manages TCP connections for peer-to-peer communication.
type TCPTransport struct {
	TCPTransportOpts
	listener net.Listener // TCP listener for incoming connections.
	rpcch    chan RPC     // Channel for incoming RPC messages.
}

// NewTCPTransport creates a new TCPTransport instance with provided options.
func NewTCPTransport(opts TCPTransportOpts) *TCPTransport {
	return &TCPTransport{
		TCPTransportOpts: opts,
		rpcch:           make(chan RPC, 1024),
	}
}

// Close closes the TCP listener.
func (t *TCPTransport) Close() error {
	return t.listener.Close()
}

// Dial connects to a remote address.
func (t *TCPTransport) Dial(addr string) error {
	conn, err := net.Dial("tcp", addr)
	if err != nil {
		return fmt.Errorf("failed to dial %s: %w", addr, err)
	}
	go t.handleConn(conn, true)
	return nil
}

// ListenAndAccept starts listening for incoming connections.
func (t *TCPTransport) ListenAndAccept() error {
	var err error
	t.listener, err = net.Listen("tcp", t.ListenAddress)
	if err != nil {
		return fmt.Errorf("failed to listen on %s: %w", t.ListenAddress, err)
	}
	go t.startAcceptLoop()
	log.Printf("TCP transport listening on: %s\n", t.ListenAddress)
	return nil
}

// startAcceptLoop continuously accepts incoming connections.
func (t *TCPTransport) startAcceptLoop() {
	for {
		conn, err := t.listener.Accept()
		if errors.Is(err, net.ErrClosed) {
			return // Listener is closed, exit the loop.
		}
		if err != nil {
			log.Printf("TCP accept error: %s\n", err)
			continue
		}
		log.Printf("New incoming connection: %s\n", conn.RemoteAddr())
		go t.handleConn(conn, false)
	}
}

// handleConn manages the connection with a peer.
func (t *TCPTransport) handleConn(conn net.Conn, outbound bool) {
	var err error
	defer func() {
		if err != nil {
			log.Printf("Dropping peer connection: %s\n", err)
		}
		conn.Close()
	}()

	peer := NewTCPPeer(conn, outbound)

	if err = t.HandShakeFunc(peer); err != nil {
		log.Printf("Failed handshake with peer: %s\n", err)
		conn.Close()
		return
	}

	if t.OnPeer != nil {
		if err := t.OnPeer(peer); err != nil {
			log.Printf("Error in OnPeer callback: %s\n", err)
			return
		}
	}

	// Read Loop
	for {
		rpc := RPC{}
		err := t.Decoder.Decode(conn, &rpc)
		if errors.Is(err, net.ErrClosed) {
			log.Printf("Peer closed connection: %s\n", err)
			return
		}
		rpc.From = conn.RemoteAddr().String()
		if rpc.Stream {
			log.Printf("Received stream message from %s\n", rpc.From)
			peer.wg.Add(1)
			log.Printf("[%s] Incoming stream...\n", conn.RemoteAddr().String())
			peer.wg.Wait()
			log.Println("Stream closed. Resuming read loop.")
			continue
		}
		t.rpcch <- rpc
		log.Println("Continuing to process incoming messages...")
	}
}