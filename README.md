# DistribCache

A high-performance, peer-to-peer distributed storage system built from scratch in Go. Features content-addressable storage with SHA1 hashing, AES encryption, and automatic TTL-based expiry.

## 🚀 Features

### Core Functionality
- **Content-Addressable Storage**: SHA1-based file addressing with hierarchical directory structure
- **Peer-to-Peer Networking**: Custom TCP transport layer with handshake protocol for reliable node communication
- **End-to-End Encryption**: AES-CTR encryption for secure file storage and network transmission
- **Automatic TTL Expiry**: Per-key time-to-live with scheduled cleanup and metadata persistence
- **Fault Tolerance**: Automatic file replication across network nodes with offline recovery support

### Technical Highlights
- Built using **Go standard library only** (zero runtime dependencies)
- Custom TCP transport with message encoding/decoding
- Broadcast messaging for network-wide synchronization
- Concurrent operations with proper mutex locking
- Comprehensive test coverage (8+ distributed scenarios)

## 📋 Architecture

```
┌─────────────┐         ┌─────────────┐         ┌─────────────┐
│   Node 1    │◄───────►│   Node 2    │◄───────►│   Node 3    │
│             │   TCP   │             │   TCP   │             │
│ FileServer  │         │ FileServer  │         │ FileServer  │
│   Store     │         │   Store     │         │   Store     │
│ Encryption  │         │ Encryption  │         │ Encryption  │
└─────────────┘         └─────────────┘         └─────────────┘
```

### Key Components

1. **FileServer** (`server.go`)
   - Manages peer connections and file distribution
   - Handles message broadcasting and routing
   - Implements TTL scheduler for automatic cleanup

2. **Store** (`store.go`)
   - Content-addressable storage with SHA1 hashing
   - Metadata management for TTL tracking
   - Read/write operations with encryption support

3. **Transport Layer** (`p2p/`)
   - Custom TCP transport implementation
   - Handshake protocol for secure connections
   - Message encoding with Go's `gob` format

4. **Encryption** (`encryption.go`)
   - AES-256 encryption/decryption
   - CTR mode with random IV generation
   - Stream-based processing for large files

## 🛠️ Installation

### Prerequisites
- Go 1.23.2 or higher

### Clone and Build
```bash
git clone https://github.com/ericzheng0404/DistribCache.git
cd DistribCache
go mod download
go build
```

## 🎯 Usage

### Basic Example

```go
package main

import (
    "bytes"
    "log"
)

func main() {
    // Create a server on port 3000
    opts := ServerOpts{DefaultTTLSeconds: 0} // 0 = infinite TTL
    s1 := makeServer(":3000", []string{}, opts)
    go s1.Start()

    // Create a second server that connects to the first
    s2 := makeServer(":4000", []string{":3000"}, opts)
    go s2.Start()

    // Store a file with 60-second TTL
    key := "myfile.txt"
    data := bytes.NewReader([]byte("Hello, Distributed World!"))
    if err := s2.Store(key, data, 60); err != nil {
        log.Fatal(err)
    }

    // Retrieve the file
    reader, err := s2.Get(key)
    if err != nil {
        log.Fatal(err)
    }
    // File is automatically replicated to s1
}
```

### TTL Examples

```go
// Store with 5-second TTL
s.Store("temp-file", data, 5)

// Store with infinite TTL (never expires)
s.Store("permanent-file", data, 0)

// Use server's default TTL
s.Store("default-ttl-file", data, 0)
```

## 🧪 Testing

Run all tests:
```bash
go test -v ./...
```

Run specific test suites:
```bash
# TTL functionality tests
go test -v -run TestTTL

# Storage tests
go test -v -run TestStore

# Encryption tests
go test -v -run TestEncryption
```

### Test Coverage
- ✅ Local TTL expiry
- ✅ Remote node synchronization
- ✅ Offline recovery and startup eviction
- ✅ Multiple keys with different TTLs
- ✅ TTL updates and edge cases
- ✅ Encryption/decryption validation
- ✅ Network partition handling

## 📊 Performance

- **Lookup**: O(1) with SHA1 content addressing
- **Storage**: Hierarchical directory structure (5-level depth)
- **Encryption**: Stream-based processing with 32KB buffer
- **TTL Cleanup**: Scheduled every 1 second with minimal overhead

## 🏗️ Project Structure

```
Distributed-CAS/
├── main.go              # Entry point and server setup
├── server.go            # FileServer implementation
├── store.go             # Content-addressable storage
├── encryption.go        # AES encryption/decryption
├── p2p/
│   ├── transport.go     # Transport interface
│   ├── tcp_transport.go # TCP implementation
│   ├── handshake.go     # Connection handshake
│   ├── message.go       # Message types
│   └── encoding.go      # Message encoding
├── *_test.go           # Test files
└── README.md
```

## 🔒 Security

- **AES-256 Encryption**: All files encrypted at rest and in transit
- **Random IV Generation**: Unique initialization vector per file
- **Secure Key Management**: 256-bit cryptographic keys
- **No Plaintext Storage**: Files always encrypted on disk

## 🚧 Limitations & Future Work

### Current Limitations
- No distributed hash table (DHT) for peer discovery
- Single transport protocol (TCP only)
- No authentication/authorization layer

### Planned Features
- [ ] WebSocket transport support
- [ ] DHT-based peer discovery
- [ ] Authentication and access control
- [ ] Compression support
- [ ] Performance benchmarking suite
- [ ] Metrics and monitoring

## 📝 Technical Details

### Content Addressing
Files are stored using SHA1 hashing with hierarchical paths:
```
key: "myfile.txt"
SHA1: a22417f34710113bc04970687667eeaefffeabb3
Path: a2241/7f347/10113/bc049/70687/667ee/aefff/eabb3/
```

### TTL Implementation
- **Metadata Files**: JSON files (`.meta`) store expiry timestamps
- **Scheduler**: Background goroutine checks expiry every second
- **Startup Recovery**: Expired files deleted on server start
- **Network Sync**: Expiry timestamps propagated to all nodes

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## 📄 License

This project is open source and available under the [MIT License](LICENSE).


## 🙏 Acknowledgments

- Built using Go's excellent standard library
- Inspired by Git's content-addressable storage model
- Testing powered by [testify](https://github.com/stretchr/testify)
