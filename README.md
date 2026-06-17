# Madonna

A production-grade distributed key-value store built from scratch in Go. No external consensus libraries: every component is hand-rolled to demonstrate the internals of a real distributed system.

## What's Inside

| Component | Implementation |
|---|---|
| Storage engine | WAL (write-ahead log) + in-memory hash map |
| Key routing | Consistent hash ring with 150 virtual nodes (SHA-256) |
| Failure detection | Gossip-based heartbeat monitor |
| Replication | Async fan-out to all alive peers |
| Transport | HTTP/1.1 — any node can serve any key via transparent proxy |
| Deployment | Docker Compose 3-node cluster |
| Observability | Live browser-based cluster monitor UI |

## Architecture

```
Client
  │
  ▼
┌─────────────────────────────────────┐
│  Layer 4 · HTTP Server              │  ← routes, proxies, replicates
├─────────────────────────────────────┤
│  Layer 3 · Replicator               │  ← async write fan-out
├─────────────────────────────────────┤
│  Layer 2 · Cluster                  │  ← consistent hash ring + gossip
├─────────────────────────────────────┤
│  Layer 1 · Store                    │  ← WAL + in-memory map
└─────────────────────────────────────┘
```

Each node is identical. There is no primary — any node accepts any request and routes it to the correct owner via the hash ring.

## Quick Start

**Requirements:** Docker Desktop (running)

```bash
# Start the 3-node cluster
docker compose up --build -d

# Verify all nodes are up
docker compose ps
```

Open the monitor dashboard:
```
http://localhost:8080/monitor
```

## Usage

### curl

```bash
# Write a key (routes to the correct node automatically)
curl -X PUT localhost:8080/key/hello -d world

# Read from any node — all three will return the same value
curl localhost:8080/key/hello
curl localhost:8081/key/hello
curl localhost:8082/key/hello

# Delete a key
curl -X DELETE localhost:8080/key/hello
```

### CLI

```bash
# Build
go build -o bin/cli ./cmd/cli        # Linux/Mac
go build -o bin/cli.exe ./cmd/cli    # Windows

# Use
./bin/cli -node localhost:8080 put mykey myvalue
./bin/cli -node localhost:8081 get mykey       # route through any node
./bin/cli -node localhost:8082 del mykey
./bin/cli -node localhost:8080 status
```

### Make targets

```bash
make cluster-up    # build images and start cluster
make demo-put      # seed 3 keys
make demo-get      # read the same key from all 3 nodes
make cluster-down  # stop cluster
make test          # run integration tests
make bench         # run benchmarks
```

## API Reference

### Public (client-facing)

| Method | Path | Description |
|---|---|---|
| `GET` | `/key/{key}` | Get value. Returns `404` if not found. |
| `PUT` | `/key/{key}` | Set value (request body = raw value). Returns `204`. |
| `DELETE` | `/key/{key}` | Delete key. Returns `204`. |

If a request arrives at a node that does not own the key, it is transparently proxied to the correct owner. The response includes an `X-Served-By` header indicating which node handled it.

### Internal (node-to-node)

| Method | Path | Description |
|---|---|---|
| `GET` | `/internal/ping` | Gossip heartbeat liveness check |
| `POST` | `/internal/replicate` | Receive a replicated write |
| `GET` | `/internal/status` | Cluster liveness state (JSON) |
| `GET` | `/monitor` | Live cluster monitor UI |

## How It Works

### Consistent Hashing

Keys are assigned to nodes using a consistent hash ring. Each real node occupies 150 virtual positions on the ring (SHA-256, first 4 bytes as uint32). When a key arrives, its hash is computed and the first virtual node clockwise on the ring becomes the owner.

This means adding or removing a node remaps only `1/N` of keys on average, versus `~100%` with naive modulo hashing.

### Gossip Failure Detection

Every node pings every peer every 500ms. If a peer misses 3 consecutive heartbeats, it is declared dead and removed from the ring. Its keys are automatically rehashed to the next alive node. When the node recovers, it is re-added to the ring.

### Write Path

```
Client PUT /key/hello
  → Node receives request
  → Hash ring: who owns "hello"?
    → Self: write to WAL → update memory → enqueue replication → 204
    → Other: proxy request to owner node → relay response
  → Background: fan-out POST /internal/replicate to all alive peers
```

Replication is **async** — the client gets an acknowledgement as soon as the WAL write is durable on the primary. Peers catch up in the background. This is a deliberate AP tradeoff: low write latency at the cost of brief replica lag.

### WAL Format

```
┌──────────────────┬─────────────────────────────────────────┐
│  4 bytes (BE)    │  JSON payload + newline                 │
│  Length prefix   │  {"op":"PUT","key":"k1","value":"v1"}\n │
└──────────────────┴─────────────────────────────────────────┘
```

On restart, the WAL is replayed sequentially to rebuild in-memory state. Truncated entries (crash mid-write) are silently discarded.

## Configuration

All configuration is via environment variables — no config files, no rebuilds needed.

| Variable | Description | Example |
|---|---|---|
| `MADONNA_ADDR` | This node's address | `node1:8080` |
| `MADONNA_PEERS` | Comma-separated peer addresses | `node2:8080,node3:8080` |
| `MADONNA_WAL` | Path to WAL file | `/data/wal.log` |

## Project Structure

```
madonna/
├── cmd/
│   ├── node/          # Node entrypoint
│   └── cli/           # CLI client
├── internal/
│   ├── cluster/       # Hash ring + gossip wired together
│   ├── gossip/        # Heartbeat failure detection
│   ├── hash/          # Consistent hash ring
│   ├── monitor/       # Browser-based cluster UI
│   ├── replication/   # Async write fan-out
│   ├── server/        # HTTP layer
│   ├── store/         # In-memory KV + WAL integration
│   └── wal/           # Write-ahead log
├── tests/
│   ├── integration_test.go
│   └── benchmark_test.go
├── Dockerfile
├── docker-compose.yml
└── Makefile
```

## Testing

```bash
# Full integration test suite (spins up real in-process nodes)
make test

# Benchmarks — WAL throughput + in-memory read throughput
make bench
```

Integration tests cover:

- Consistent hash routing (key written to node1 readable via node2)
- WAL crash recovery (state survives process restart)
- Async replication (write appears on peer within 500ms)
- Hash ring determinism (all nodes agree on key ownership)

## Design Decisions & Tradeoffs

**Async replication over synchronous quorum writes**
Writes return after a single WAL flush on the primary. If the primary crashes before replication completes, that write is lost. A CP system would wait for a quorum (e.g. 2 of 3 nodes) before acknowledging — at the cost of higher write latency. This project documents the tradeoff rather than hiding it.

**Direct pings over gossip broadcast**
Each node pings every peer directly — O(N) messages per interval. A true gossip protocol (e.g. SWIM) is O(log N). For a 3-node cluster the difference is irrelevant; the detection logic is identical and the structure makes failure detection easy to follow.

**HTTP over a binary protocol**
Using HTTP means `curl` just works, the monitor UI is a single HTML file with no backend changes, and every internal operation is observable with standard tools. A production system would use a binary protocol (gRPC, custom TCP) for throughput.

## License

MIT
