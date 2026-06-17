# Madonna — System Architecture & Workflow Diagrams

## 1. High-Level System Architecture

```mermaid
graph TB
    subgraph "Client Layer"
        CLI["CLI Client<br/>(cmd/cli)"]
        CURL["curl / HTTP Client"]
    end

    subgraph "Docker Network: madonna"
        subgraph "Node 1 (localhost:8080)"
            S1["HTTP Server"]
            CL1["Cluster Manager"]
            R1["Replicator"]
            ST1["Store (in-memory)"]
            W1["WAL (disk)"]
            G1["Gossip Monitor"]
            H1["Hash Ring"]
        end

        subgraph "Node 2 (localhost:8081)"
            S2["HTTP Server"]
            CL2["Cluster Manager"]
            R2["Replicator"]
            ST2["Store (in-memory)"]
            W2["WAL (disk)"]
            G2["Gossip Monitor"]
            H2["Hash Ring"]
        end

        subgraph "Node 3 (localhost:8082)"
            S3["HTTP Server"]
            CL3["Cluster Manager"]
            R3["Replicator"]
            ST3["Store (in-memory)"]
            W3["WAL (disk)"]
            G3["Gossip Monitor"]
            H3["Hash Ring"]
        end
    end

    CLI --> S1
    CLI --> S2
    CLI --> S3
    CURL --> S1

    G1 <--> G2
    G1 <--> G3
    G2 <--> G3

    R1 --> S2
    R1 --> S3
    R2 --> S1
    R2 --> S3
    R3 --> S1
    R3 --> S2
```

---

## 2. Node Internal Layer Architecture

Each node is built as a **4-layer stack**. Dependencies only flow downward.

```mermaid
graph TB
    subgraph "Single Node Architecture"
        L4["Layer 4: HTTP Server<br/>(internal/server)"]
        L3["Layer 3: Replication<br/>(internal/replication)"]
        L2["Layer 2: Cluster<br/>(internal/cluster)"]
        L2a["Consistent Hash Ring<br/>(internal/hash)"]
        L2b["Gossip Monitor<br/>(internal/gossip)"]
        L1["Layer 1: Durable Store<br/>(internal/store)"]
        L0["WAL (Write-Ahead Log)<br/>(internal/wal)"]
    end

    L4 --> L3
    L4 --> L2
    L4 --> L1
    L2 --> L2a
    L2 --> L2b
    L1 --> L0

    style L4 fill:#4A90D9,stroke:#2C5F8A,color:#fff
    style L3 fill:#7B68EE,stroke:#5A4FC0,color:#fff
    style L2 fill:#FF8C42,stroke:#CC6F35,color:#fff
    style L2a fill:#FFB347,stroke:#CC8F39,color:#fff
    style L2b fill:#FFB347,stroke:#CC8F39,color:#fff
    style L1 fill:#50C878,stroke:#3DA05F,color:#fff
    style L0 fill:#2E8B57,stroke:#246D44,color:#fff
```

---

## 3. Write Flow (PUT /key/{key})

```mermaid
sequenceDiagram
    participant C as Client
    participant S1 as Node 1 (Server)
    participant HR as Hash Ring
    participant ST as Store
    participant WAL as WAL (Disk)
    participant REP as Replicator
    participant S2 as Node 2
    participant S3 as Node 3

    C->>S1: PUT /key/hello body="world"
    S1->>HR: OwnerOf("hello")
    
    alt Key belongs to this node
        HR-->>S1: "node1:8080" (self)
        S1->>WAL: Append(PUT, "hello", "world")
        WAL->>WAL: Write length-prefix + JSON
        WAL->>WAL: Flush + fsync
        WAL-->>S1: OK (durable)
        S1->>ST: data["hello"] = "world"
        S1->>REP: EnqueuePut("hello", "world")
        S1-->>C: 204 No Content ✓
        
        Note over REP,S3: Async replication (background)
        REP->>S2: POST /internal/replicate {"op":"PUT","key":"hello","value":"world"}
        REP->>S3: POST /internal/replicate {"op":"PUT","key":"hello","value":"world"}
    
    else Key belongs to another node
        HR-->>S1: "node2:8080" (remote)
        S1->>S2: Proxy PUT /key/hello body="world"
        S2-->>S1: 204 No Content
        S1-->>C: 204 No Content (X-Served-By: node2:8080)
    end
```

---

## 4. Read Flow (GET /key/{key})

```mermaid
sequenceDiagram
    participant C as Client
    participant S1 as Node 1 (Server)
    participant HR as Hash Ring
    participant ST as Store
    participant S2 as Node 2

    C->>S1: GET /key/hello
    S1->>HR: OwnerOf("hello")
    
    alt Key owned locally
        HR-->>S1: "node1:8080" (self)
        S1->>ST: Get("hello")
        ST-->>S1: "world", found=true
        S1-->>C: 200 OK "world"
    
    else Key owned remotely
        HR-->>S1: "node2:8080" (remote)
        S1->>S2: Proxy GET /key/hello
        S2-->>S1: 200 OK "world"
        S1-->>C: 200 OK "world" (X-Served-By: node2:8080)
    end
```

---

## 5. Gossip Failure Detection

```mermaid
sequenceDiagram
    participant G1 as Node 1 Gossip
    participant G2 as Node 2 Gossip
    participant G3 as Node 3 Gossip
    participant Ring as Hash Ring

    loop Every 500ms
        G1->>G2: GET /internal/ping
        G2-->>G1: 200 OK
        G1->>G3: GET /internal/ping
        G3-->>G1: 200 OK
    end

    Note over G3: Node 3 crashes!

    G1->>G3: GET /internal/ping
    G3--xG1: Timeout (missed=1)
    G1->>G3: GET /internal/ping
    G3--xG1: Timeout (missed=2)
    G1->>G3: GET /internal/ping
    G3--xG1: Timeout (missed=3)

    Note over G1: missed >= maxMissed (3)
    G1->>Ring: Remove("node3:8080")
    Note over Ring: Node 3's keys reassigned<br/>to next clockwise node

    Note over G3: Node 3 recovers!
    G1->>G3: GET /internal/ping
    G3-->>G1: 200 OK
    G1->>Ring: Add("node3:8080")
    Note over Ring: Node 3 back in ring
```

---

## 6. Consistent Hashing Ring

```mermaid
graph LR
    subgraph "Consistent Hash Ring (SHA-256, 150 vnodes/node)"
        direction LR
        A["0x00000000"]
        B["Node1 vnode #42"]
        C["Node3 vnode #7"]
        D["Node2 vnode #91"]
        E["Node1 vnode #118"]
        F["Node2 vnode #3"]
        G["Node3 vnode #55"]
        H["0xFFFFFFFF"]
    end

    A --> B --> C --> D --> E --> F --> G --> H
    H -.->|wrap around| A

    K1["key: 'hello'<br/>hash → 0x3A..."]
    K1 -.->|lands on| D

    style K1 fill:#FFD700,stroke:#B8860B,color:#333
    style D fill:#7B68EE,stroke:#5A4FC0,color:#fff
```

> **Key Insight**: Each real node gets **150 virtual positions** on the ring. When looking up a key, the system hashes the key with SHA-256, takes the first 4 bytes as a uint32, and finds the **first virtual node clockwise** from that position. This ensures even distribution and minimal key remapping when nodes join/leave.

---

## 7. Docker Deployment Topology

```mermaid
graph TB
    subgraph "Host Machine"
        subgraph "Docker Compose Orchestration"
            subgraph "Bridge Network: madonna"
                N1["Container: node1<br/>Image: madonna<br/>Port: 8080→8080<br/>Volume: node1-data:/data"]
                N2["Container: node2<br/>Image: madonna<br/>Port: 8081→8080<br/>Volume: node2-data:/data"]
                N3["Container: node3<br/>Image: madonna<br/>Port: 8082→8080<br/>Volume: node3-data:/data"]
            end
        end

        V1["Volume: node1-data<br/>(WAL persistence)"]
        V2["Volume: node2-data<br/>(WAL persistence)"]
        V3["Volume: node3-data<br/>(WAL persistence)"]
    end

    N1 <-->|"gossip + repl"| N2
    N1 <-->|"gossip + repl"| N3
    N2 <-->|"gossip + repl"| N3

    N1 --- V1
    N2 --- V2
    N3 --- V3

    EXT["External Client<br/>localhost:8080/8081/8082"]
    EXT --> N1
    EXT --> N2
    EXT --> N3

    style N1 fill:#4A90D9,stroke:#2C5F8A,color:#fff
    style N2 fill:#50C878,stroke:#3DA05F,color:#fff
    style N3 fill:#FF8C42,stroke:#CC6F35,color:#fff
```

### Docker Build Process (Multi-stage)

```mermaid
graph LR
    subgraph "Stage 1: Builder (golang:1.22-alpine)"
        SRC["Copy source code"] --> MOD["go mod download"] --> BUILD["go build -o /node ./cmd/node"]
    end

    subgraph "Stage 2: Runtime (alpine:3.19)"
        COPY["COPY --from=builder /node"] --> MKDIR["mkdir -p /data"] --> EP["ENTRYPOINT /node"]
    end

    BUILD --> COPY

    style SRC fill:#4A90D9,stroke:#2C5F8A,color:#fff
    style EP fill:#50C878,stroke:#3DA05F,color:#fff
```

---

## 8. WAL Crash Recovery Flow

```mermaid
sequenceDiagram
    participant Node as Node Process
    participant WAL as WAL File
    participant MEM as In-Memory Map

    Note over Node: Node starts / restarts

    Node->>WAL: Open(walPath)
    Node->>WAL: Replay(callback)
    
    loop For each entry in WAL
        WAL-->>Node: Entry{op: PUT, key: "k1", value: "v1"}
        Node->>MEM: data["k1"] = "v1"
        WAL-->>Node: Entry{op: PUT, key: "k2", value: "v2"}
        Node->>MEM: data["k2"] = "v2"
        WAL-->>Node: Entry{op: DELETE, key: "k1"}
        Node->>MEM: delete(data, "k1")
    end

    Note over MEM: State restored:<br/>{"k2": "v2"}
    Note over Node: Ready to serve requests
```

### WAL Entry Format

```
┌──────────────────┬────────────────────────────────────────┐
│  4 bytes (BE)    │  JSON payload + newline                │
│  Length Prefix   │  {"op":"PUT","key":"k1","value":"v1"}\n│
└──────────────────┴────────────────────────────────────────┘
```

---

## 9. Environment Variables & Configuration

| Variable | Purpose | Example |
|---|---|---|
| `MADONNA_ADDR` | This node's address (host:port) | `node1:8080` |
| `MADONNA_PEERS` | Comma-separated peer addresses | `node2:8080,node3:8080` |
| `MADONNA_WAL` | Path to WAL file | `/data/wal.log` |

---

## 10. API Endpoints Summary

### Public API (Client-facing)
| Method | Path | Description |
|---|---|---|
| `GET` | `/key/{key}` | Get value for key (proxies to owner if needed) |
| `PUT` | `/key/{key}` | Set value for key (body = value) |
| `DELETE` | `/key/{key}` | Delete key |

### Internal API (Node-to-node)
| Method | Path | Description |
|---|---|---|
| `GET` | `/internal/ping` | Gossip heartbeat liveness check |
| `POST` | `/internal/replicate` | Receive replicated write operation |
| `GET` | `/internal/status` | Cluster liveness state (JSON) |
