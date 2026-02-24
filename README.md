# DistribCache

A high-performance, peer-to-peer distributed storage system built from scratch in Go. Features **true content-addressable storage** where files are addressed by the SHA1 hash of their *content*, AES encryption, automatic TTL-based expiry, and a live web dashboard.

## 🚀 Features

### Core Functionality
- **True Content-Addressable Storage**: Files are addressed by the SHA1 hash of their *content* — identical content always resolves to the same address regardless of key, enabling automatic deduplication
- **Peer-to-Peer Networking**: Custom TCP transport layer with handshake protocol for reliable node communication
- **End-to-End Encryption**: AES-CTR encryption for secure file storage and network transmission
- **Automatic TTL Expiry**: Per-key time-to-live with scheduled cleanup and metadata persistence
- **Fault Tolerance**: Automatic file replication across network nodes with offline recovery support
- **Web Dashboard**: Live single-page UI with file upload, download, delete, TTL badges, CAS dedup highlighting, and runtime peer management

### Technical Highlights
- Built using **Go standard library only** (zero runtime dependencies)
- Custom TCP transport with message encoding/decoding
- Broadcast messaging for network-wide synchronization
- Concurrent operations with proper mutex locking
- Persistent `_keyindex.json` maps human-readable keys to content hashes, surviving server restarts
- REST HTTP API for all storage and peer operations
- Comprehensive test coverage (8+ distributed scenarios)

## 📋 Architecture

```
┌─────────────┐         ┌─────────────┐         ┌─────────────┐
│   Node 1    │◄───────►│   Node 2    │◄───────►│   Node 3    │
│             │   TCP   │             │   TCP   │             │
│ FileServer  │         │ FileServer  │         │ FileServer  │
│   Store     │         │   Store     │         │   Store     │
│ Encryption  │         │ Encryption  │         │ Encryption  │
│  HTTP API   │         │             │         │             │
│  Dashboard  │         │             │         │             │
└─────────────┘         └─────────────┘         └─────────────┘
       ▲
       │ HTTP :8080
  Browser / curl
```

### Key Components

1. **FileServer** (`server.go`)
   - Manages peer connections and file distribution
   - Handles message broadcasting and routing
   - Implements TTL scheduler for automatic cleanup

2. **Store** (`store.go`)
   - True content-addressable storage: SHA1 is computed from file *content* via a buffer, not from the key
   - `keyIndex` (`_keyindex.json`) maps human-readable keys → SHA1 content hashes, persisted across restarts
   - `CASPathTransformFunc` is a pure hash splitter — it takes an already-computed digest and builds the hierarchical path
   - Metadata management for TTL tracking
   - Read/write operations with encryption support

3. **HTTP API & Dashboard** (`http_api.go` + `frontend/`)
   - REST API serving all storage and peer operations
   - Single-page dashboard with live polling, drag-and-drop upload, CAS dedup visualisation
   - Runtime peer connection via dashboard UI or `POST /api/peers`

4. **Transport Layer** (`p2p/`)
   - Custom TCP transport implementation
   - Handshake protocol for secure connections
   - Message encoding with Go's `gob` format

5. **Encryption** (`encryption.go`)
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

## 🖥️ Dashboard

### Starting
```bash
go run .
```
Then open **http://localhost:8080** in your browser.

The dashboard auto-starts alongside the TCP node — no extra steps needed.

### Features
| Panel | What it does |
|---|---|
| **Stats bar** | Live file count, peer count, deduplicated blob count |
| **Connected Peers** | Lists active TCP peers; connect new peers at runtime |
| **Upload File** | Drag-and-drop or browse; set key name and optional TTL |
| **Stored Files** | Table with SHA1 hash (click to copy), size, TTL countdown, download & delete |

### Adding a peer at runtime
In the **Connected Peers** panel, type a TCP address in the "Connect new peer" field and press **+ Connect** (or Enter):
```
:4001
```
The node dials immediately; the peer appears in the list within 2 seconds once the handshake completes.

## 🌐 REST API

All endpoints are served on `:8080` alongside the dashboard.

| Method | Endpoint | Description |
|---|---|---|
| `GET` | `/api/status` | Node address, connected peers, file count |
| `GET` | `/api/files` | List all files (key, content hash, size, TTL) |
| `POST` | `/api/files` | Upload a file (`multipart/form-data`: `key`, `ttl`, `file`) |
| `GET` | `/api/files/{key}` | Download a file |
| `DELETE` | `/api/files/{key}` | Delete a file |
| `POST` | `/api/peers` | Connect to a new peer at runtime (`{"addr": ":4001"}`) |

### Examples
```bash
# Upload
curl -F 'key=hello.txt' -F 'ttl=60' -F 'file=@hello.txt' http://localhost:8080/api/files

# Download
curl http://localhost:8080/api/files/hello.txt -o hello.txt

# Delete
curl -X DELETE http://localhost:8080/api/files/hello.txt

# Connect peer
curl -X POST http://localhost:8080/api/peers \
  -H 'Content-Type: application/json' \
  -d '{"addr": ":4001"}'

# Status
curl http://localhost:8080/api/status
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

- **Lookup**: O(1) — key index resolve + SHA1-derived path
- **Storage**: Hierarchical directory structure (8-level depth, 5 hex chars per level)
- **Deduplication**: Identical content stored once; multiple keys can point to the same hash
- **Encryption**: Stream-based processing with 32KB buffer
- **TTL Cleanup**: Scheduled every 1 second with minimal overhead

## 🏗️ Project Structure

```
DistribCache/
├── main.go              # Entry point and server setup
├── server.go            # FileServer implementation
├── store.go             # True content-addressable storage + key index
├── encryption.go        # AES encryption/decryption
├── http_api.go          # REST API + dashboard HTTP server
├── frontend/
│   └── index.html       # Single-page web dashboard
├── p2p/
│   ├── transport.go     # Transport interface
│   ├── tcp_transport.go # TCP implementation
│   ├── handshake.go     # Connection handshake
│   ├── message.go       # Message types
│   └── encoding.go      # Message encoding
├── *_test.go            # Test files
└── README.md
```

At runtime each storage root also contains:
```
<root>/
├── _keyindex.json       # Persistent key → SHA1 content hash mapping
├── <h1>/<h2>/.../<sha1> # Content file at hash-derived path
└── <h1>/<h2>/.../<sha1>.meta  # TTL metadata (optional)
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
- Dashboard only attaches to the first node (`s1`)

### Planned Features
- [ ] WebSocket transport support
- [ ] DHT-based peer discovery
- [ ] Authentication and access control
- [ ] Compression support
- [ ] Per-node dashboard switcher
- [ ] Metrics and monitoring
- [x] Web dashboard with REST API
- [x] Runtime peer connection via UI/API

## 📝 Technical Details

### Content Addressing
Files are addressed by the SHA1 hash of their **content**, not their key. The key is only a human-readable alias stored in `_keyindex.json`.

```
Write("myfile.txt", []byte("Hello, World!"))

1. Buffer content → compute SHA1("Hello, World!")
   SHA1: 943a702d06f34599aee1f8da8ef9f7296031d699

2. Derive hierarchical path from hash:
   Path: 943a7/02d06/f3459/9aee1/f8da8/ef9f7/29603/1d699/
   File: <root>/943a7/02d06/f3459/9aee1/f8da8/ef9f7/29603/1d699/943a702d06f34599aee1f8da8ef9f7296031d699

3. Register in key index:
   _keyindex.json: { "myfile.txt": "943a702d06f34599aee1f8da8ef9f7296031d699" }
```

**Deduplication example** — two different keys with identical content map to the same file:
```
Write("foo", []byte("same content"))  →  SHA1: 94e66df8cd09...  (stored once)
Write("bar", []byte("same content"))  →  SHA1: 94e66df8cd09...  (no duplicate write)
```

Lookup by key resolves through the index: `key → contentHash → path → file`.

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
