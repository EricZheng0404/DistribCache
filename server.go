package main

import (
	"bytes"
	"encoding/binary"
	"encoding/gob"
	"fmt"
	"io"
	"log"
	"sync"
	"time"

	"github.com/ericzheng0404/DistribCache/p2p"
)

type FileServerOpts struct {
	ListenAddress     string
	EncKey            []byte
	StorageRoot       string
	PathTransformFunc PathTransformFunc
	Transport         p2p.Transport
	BootstrapNodes    []string
	DefaultTTLSeconds int64
}

type FileServer struct {
	FileServerOpts
	peerLock     sync.Mutex
	peers        map[string]p2p.Peer
	store        *Store
	quitchan     chan struct{}
	expiryIndex  map[string]int64
	expiryLock   sync.RWMutex
}

func NewFileServer(opts FileServerOpts) *FileServer {
	storeOpts := StoreOpts{
		Root:              opts.StorageRoot,
		PathTransformFunc: opts.PathTransformFunc,
	}
	return &FileServer{
		FileServerOpts: opts,
		store:          NewStore(storeOpts),
		quitchan:       make(chan struct{}),
		peers:          make(map[string]p2p.Peer),
		expiryIndex:    make(map[string]int64),
	}
}

func (s *FileServer) HandleMessage(from string, msg *Message) error {
	switch v := msg.Payload.(type) {
	case MessageStoreFile:
		return s.handleMessageStoreFile(from, v)
	case MessageGetFile:
		return s.handleMessageGetFile(from, v)
	}
	return nil
}

func (s *FileServer) handleMessageGetFile(from string, msg MessageGetFile) error {
	fmt.Printf("[%s] Serving file over the network: %s\n", s.Transport.Addr(), msg.Key)
	if s.store.HasKey(msg.Key) {
		fileSize, reader, err := s.store.Read(msg.Key)
		if err != nil {
			return fmt.Errorf("error reading file %s: %w", msg.Key, err)
		}

		if rc, ok := reader.(io.ReadCloser); ok {
			defer func() {
				if err := rc.Close(); err != nil {
					log.Printf("error closing reader: %v", err)
				}
			}()
		}

		peer, ok := s.peers[from]
		if !ok {
			return fmt.Errorf("peer %s not found", from)
		}

		peer.Send([]byte{p2p.IncomingStream})
		if err := binary.Write(peer, binary.LittleEndian, fileSize); err != nil {
			return fmt.Errorf("error writing file size to peer %s: %w", from, err)
		}

		n, err := io.Copy(peer, reader)
		if err != nil {
			return fmt.Errorf("error sending file to peer %s: %w", from, err)
		}
		fmt.Printf("[%s] Written %d bytes over the network to %s\n", s.Transport.Addr(), n, from)
		return nil
	} else {
		return fmt.Errorf("[%s] File not found for key: %s", s.Transport.Addr(), msg.Key)
	}
}

func (s *FileServer) handleMessageStoreFile(from string, msg MessageStoreFile) error {
	peer, ok := s.peers[from]
	if !ok {
		return fmt.Errorf("peer not found: %s", from)
	}

	n, err := s.store.Write(msg.Key, io.LimitReader(peer, int64(msg.Size)), msg.TTL)
	if err != nil {
		return fmt.Errorf("error writing file %s to disk: %w", msg.Key, err)
	}

	s.expiryLock.Lock()
	s.expiryIndex[msg.Key] = msg.Expiry
	s.expiryLock.Unlock()

	fmt.Printf("Wrote %d bytes to disk at address: [%s]\n", n, s.Transport.Addr())
	peer.CloseStream()
	return nil
}

func (s *FileServer) loop() {
	defer func() {
		log.Println("File server stopped due to error or user quit action")
		s.Transport.Close()
	}()

	for {
		select {
		case rpc := <-s.Transport.Consume():
			var msg Message
			if err := gob.NewDecoder(bytes.NewReader(rpc.Payload)).Decode(&msg); err != nil {
				log.Printf("Decoding error: %v", err)
				return
			}
			if err := s.HandleMessage(rpc.From, &msg); err != nil {
				log.Printf("Error handling message from %s: %v", rpc.From, err)
			}

		case <-s.quitchan:
			return
		}
	}
}

func (s *FileServer) stream(msg *Message) error {
	peers := []io.Writer{}
	for _, peer := range s.peers {
		peers = append(peers, peer)
	}
	mw := io.MultiWriter(peers...)
	return gob.NewEncoder(mw).Encode(msg)
}

func (s *FileServer) broadcast(msg *Message) error {
	msgBuffer := new(bytes.Buffer)
	if err := gob.NewEncoder(msgBuffer).Encode(msg); err != nil {
		return fmt.Errorf("error encoding message for broadcast: %w", err)
	}
	for _, peer := range s.peers {
		if err := peer.Send([]byte{p2p.IncomingMessage}); err != nil {
			return fmt.Errorf("error sending incoming message byte to peer: %w", err)
		}
		if err := peer.Send(msgBuffer.Bytes()); err != nil {
			return fmt.Errorf("error sending broadcast message to peer: %w", err)
		}
	}
	return nil
}

type Message struct {
	Payload any
}

type MessageStoreFile struct {
	Key    string
	Size   int64
	TTL    int64 // seconds supplied by sender
	Expiry int64 // unix seconds, authoritative
}

type MessageGetFile struct {
	Key string
}

func (s *FileServer) Get(key string) (io.Reader, error) {
	if s.store.HasKey(key) {
		fmt.Printf("[%s] Serving file (%s) locally\n", s.Transport.Addr(), key)
		_, r, err := s.store.Read(key)
		return r, err
	}

	fmt.Printf("[%s] File (%s) not found locally. Serving via network...\n", s.Transport.Addr(), key)
	msg := Message{
		Payload: MessageGetFile{
			Key: key,
		},
	}
	if err := s.broadcast(&msg); err != nil {
		return nil, fmt.Errorf("error broadcasting message for key %s: %w", key, err)
	}

	time.Sleep(500 * time.Millisecond)

	// Open a stream and read from each peer
	for _, peer := range s.peers {
		var fileSize int64
		if err := binary.Read(peer, binary.LittleEndian, &fileSize); err != nil {
			return nil, fmt.Errorf("error reading file size from peer %s: %w", peer.RemoteAddr(), err)
		}

		n, err := s.store.WriteDecrypt(s.EncKey, key, io.LimitReader(peer, fileSize), 0)
		if err != nil {
			return nil, fmt.Errorf("error writing decrypted file %s: %w", key, err)
		}

		fmt.Printf("[%s] Received %d bytes over the network from: %s\n", s.Transport.Addr(), n, peer.RemoteAddr())
		peer.CloseStream()
	}

	_, r, err := s.store.Read(key)
	return r, err
}

func (s *FileServer) Store(key string, r io.Reader, ttlSecs int64) error {
	if ttlSecs < 0 {
		return fmt.Errorf("TTL cannot be negative")
	}

	if ttlSecs == 0 {
		ttlSecs = s.DefaultTTLSeconds
	}

	var (
		fileBuf = new(bytes.Buffer)
		tee     = io.TeeReader(r, fileBuf)
	)

	size, err := s.store.Write(key, tee, ttlSecs)
	if err != nil {
		return fmt.Errorf("error storing file %s: %w", key, err)
	}

	expiry := int64(0)
	if ttlSecs > 0 {
		expiry = time.Now().Unix() + ttlSecs
	}

	s.expiryLock.Lock()
	s.expiryIndex[key] = expiry
	s.expiryLock.Unlock()

	msg := Message{
		Payload: MessageStoreFile{
			Key:    key,
			Size:   size + 16, // 16 bytes for the IV encryption header
			TTL:    ttlSecs,
			Expiry: expiry,
		},
	}

	if err := s.broadcast(&msg); err != nil {
		return fmt.Errorf("error broadcasting store message for key %s: %w", key, err)
	}

	time.Sleep(1 * time.Millisecond)

	for _, peer := range s.peers {
		if err := peer.Send([]byte{p2p.IncomingStream}); err != nil {
			return fmt.Errorf("error sending incoming stream byte to peer: %w", err)
		}

		n, err := copyEncrypt(s.EncKey, fileBuf, peer)
		if err != nil {
			return fmt.Errorf("error copying encrypted file to peer: %w", err)
		}
		fmt.Printf("Received and written bytes to disk: %d\n", n)
	}

	return nil
}

func (s *FileServer) Stop() {
	close(s.quitchan)
}

func (s *FileServer) BootstrapNetwork() error {
	for _, nodeAddress := range s.BootstrapNodes {
		if len(nodeAddress) == 0 {
			continue
		}
		go func(nodeAddress string) {
			fmt.Printf("[%s] Attempting to connect with remote %s\n", s.Transport.Addr(), nodeAddress)
			if err := s.Transport.Dial(nodeAddress); err != nil {
				log.Printf("Error dialing node %s: %v", nodeAddress, err)
			}
		}(nodeAddress)
	}
	return nil
}

func (s *FileServer) OnPeer(p p2p.Peer) error {
	fmt.Printf("Connected to remote peer %s\n", p.RemoteAddr().String())
	s.peerLock.Lock()
	defer s.peerLock.Unlock()
	s.peers[p.RemoteAddr().String()] = p
	return nil
}

func (s *FileServer) scheduler() {
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ticker.C:
			s.expiryLock.Lock()
			for key, expiry := range s.expiryIndex {
				if expiry > 0 && time.Now().Unix() > expiry {
					s.store.DeleteLocal(key)
					delete(s.expiryIndex, key)
				}
			}
			s.expiryLock.Unlock()
		case <-s.quitchan:
			return
		}
	}
}

func (s *FileServer) Start() error {
	if err := s.Transport.ListenAndAccept(); err != nil {
		return fmt.Errorf("error starting transport: %w", err)
	}

	s.populateExpiryIndex()
	go s.scheduler()

	if err := s.BootstrapNetwork(); err != nil {
		return fmt.Errorf("error bootstrapping network: %w", err)
	}

	s.loop()
	return nil
}

func (s *FileServer) populateExpiryIndex() {
	keys, err := s.store.ListKeys()
	if err != nil {
		log.Printf("Failed to list keys for expiry index: %v", err)
		return
	}

	s.expiryLock.Lock()
	defer s.expiryLock.Unlock()
	for _, keyExpiry := range keys {
		if keyExpiry.Expiry > 0 {
			if time.Now().Unix() > keyExpiry.Expiry {
				s.store.DeleteLocal(keyExpiry.Key)
			} else {
				s.expiryIndex[keyExpiry.Key] = keyExpiry.Expiry
			}
		}
	}
}

func init() {
	gob.Register(MessageStoreFile{})
	gob.Register(MessageGetFile{})
}