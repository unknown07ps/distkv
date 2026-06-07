// Package replication propagates writes from a primary node to its replicas.
//
// Write path:
//   Client -> primary node (PUT /key)
//       -> store.Put (WAL + memory)
//       -> Replicator.Enqueue (non-blocking)
//   Background goroutine -> POST /internal/replicate on each alive peer
//
// This is async replication: the client gets an acknowledgement as soon as
// the primary has written to its WAL. Replicas catch up in the background.
//
// Tradeoff: if the primary crashes before replicas receive the write, that
// write is lost. For a CP system you'd wait for a quorum of replicas to
// confirm before returning to the client. We document this tradeoff in README.
package replication

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

// Op is the payload sent to replica nodes.
type Op struct {
	Op    string `json:"op"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

// Replicator fans out write operations to peer nodes.
type Replicator struct {
	queue    chan Op
	getPeers func() []string // returns currently-alive peer addresses
}

// New creates a Replicator. getPeers is called on each write to discover alive peers.
func New(getPeers func() []string) *Replicator {
	r := &Replicator{
		queue:    make(chan Op, 4096), // buffered: primary doesn't block on slow replicas
		getPeers: getPeers,
	}
	go r.worker()
	return r
}

// EnqueuePut schedules a PUT for replication. Non-blocking.
func (r *Replicator) EnqueuePut(key, value string) {
	r.enqueue(Op{Op: "PUT", Key: key, Value: value})
}

// EnqueueDelete schedules a DELETE for replication. Non-blocking.
func (r *Replicator) EnqueueDelete(key string) {
	r.enqueue(Op{Op: "DELETE", Key: key})
}

func (r *Replicator) enqueue(op Op) {
	select {
	case r.queue <- op:
	default:
		// Queue full — replica will catch up via snapshot on rejoin.
		log.Printf("replicator: queue full, dropping op for key %s", op.Key)
	}
}

// worker drains the queue and sends each op to all alive peers.
func (r *Replicator) worker() {
	client := &http.Client{Timeout: 2 * time.Second}

	for op := range r.queue {
		for _, peer := range r.getPeers() {
			if err := sendOp(client, peer, op); err != nil {
				log.Printf("replicator: failed to replicate to %s: %v", peer, err)
			}
		}
	}
}

// sendOp sends one operation to one peer.
func sendOp(client *http.Client, peer string, op Op) error {
	body, err := json.Marshal(op)
	if err != nil {
		return fmt.Errorf("marshal: %w", err)
	}

	url := fmt.Sprintf("http://%s/internal/replicate", peer)
	resp, err := client.Post(url, "application/json", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("post: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("peer returned %d", resp.StatusCode)
	}
	return nil
}
