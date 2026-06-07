// Package gossip implements heartbeat-based failure detection.
//
// Why this exists: when a node dies, it doesn't announce itself.
// Every other node must independently detect the silence and mark it dead.
//
// Design: each node sends a heartbeat ping to every peer every interval.
// If a node misses maxMissed consecutive pings, it is marked dead.
// Dead nodes are removed from the ring by the cluster layer.
//
// In a real cluster you'd use a gossip protocol (O(log N) messages).
// This implementation uses direct pings (O(N)) which is fine for 3-10 nodes
// and keeps the code readable. The structure is identical — swap the
// broadcast mechanism and the detection logic stays the same.
package gossip

import (
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

const (
	heartbeatInterval = 500 * time.Millisecond
	maxMissed         = 3 // mark dead after 3 consecutive missed heartbeats
)

// NodeState tracks liveness for one peer.
type NodeState struct {
	Addr    string
	Alive   bool
	Missed  int
}

// Monitor manages liveness state for all peers and calls OnDead/OnRevive callbacks.
type Monitor struct {
	mu       sync.RWMutex
	selfAddr string
	peers    map[string]*NodeState

	OnDead   func(addr string) // called when a node transitions alive->dead
	OnRevive func(addr string) // called when a node transitions dead->alive
}

// New creates a Monitor for the node at selfAddr.
func New(selfAddr string) *Monitor {
	return &Monitor{
		selfAddr: selfAddr,
		peers:    make(map[string]*NodeState),
	}
}

// AddPeer registers a peer to monitor.
func (m *Monitor) AddPeer(addr string) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.peers[addr] = &NodeState{Addr: addr, Alive: true}
}

// Start begins background heartbeat loops. Call once at startup.
func (m *Monitor) Start() {
	go m.pingLoop()
}

// pingLoop runs forever, pinging all peers on each tick.
func (m *Monitor) pingLoop() {
	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()

	for range ticker.C {
		m.mu.RLock()
		addrs := make([]string, 0, len(m.peers))
		for addr := range m.peers {
			addrs = append(addrs, addr)
		}
		m.mu.RUnlock()

		for _, addr := range addrs {
			go m.ping(addr)
		}
	}
}

// ping sends one heartbeat to addr and updates its liveness state.
func (m *Monitor) ping(addr string) {
	url := fmt.Sprintf("http://%s/internal/ping", addr)
	client := &http.Client{Timeout: 200 * time.Millisecond}
	resp, err := client.Get(url)
	if err == nil {
		resp.Body.Close()
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	state, ok := m.peers[addr]
	if !ok {
		return
	}

	if err != nil {
		state.Missed++
		if state.Alive && state.Missed >= maxMissed {
			state.Alive = false
			log.Printf("gossip: node %s declared dead (missed %d heartbeats)", addr, state.Missed)
			if m.OnDead != nil {
				go m.OnDead(addr)
			}
		}
	} else {
		if !state.Alive {
			log.Printf("gossip: node %s is alive again", addr)
			if m.OnRevive != nil {
				go m.OnRevive(addr)
			}
		}
		state.Alive = true
		state.Missed = 0
	}
}

// AliveNodes returns the addresses of all currently-alive peers (excludes self).
func (m *Monitor) AliveNodes() []string {
	m.mu.RLock()
	defer m.mu.RUnlock()

	alive := make([]string, 0)
	for addr, state := range m.peers {
		if state.Alive {
			alive = append(alive, addr)
		}
	}
	return alive
}

// StatusHandler returns an HTTP handler that exposes liveness state as JSON.
// Mount at /internal/status.
func (m *Monitor) StatusHandler() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		m.mu.RLock()
		defer m.mu.RUnlock()

		states := make([]NodeState, 0, len(m.peers))
		for _, s := range m.peers {
			states = append(states, *s)
		}

		w.Header().Set("Content-Type", "application/json")
		json.NewEncoder(w).Encode(map[string]interface{}{
			"self":  m.selfAddr,
			"peers": states,
		})
	}
}
