// Package hash implements a consistent hash ring with virtual nodes.
//
// Why this exists: with naive hash(key) % N, adding or removing a node
// remaps ~all keys. Consistent hashing remaps only 1/N keys on average.
// Virtual nodes fix uneven arc distribution on the ring.
package hash

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"sort"
	"sync"
)

const defaultVirtualNodes = 150

// Ring is a thread-safe consistent hash ring.
type Ring struct {
	mu           sync.RWMutex
	virtualNodes int
	ring         []uint32          // sorted hash positions
	nodeByPos    map[uint32]string // position -> node address
}

// New creates a Ring with the default number of virtual nodes per real node.
func New() *Ring {
	return &Ring{
		virtualNodes: defaultVirtualNodes,
		nodeByPos:    make(map[uint32]string),
	}
}

// Add places a node onto the ring via virtualNodes virtual positions.
// addr is the node's HTTP address, e.g. "node1:8080".
func (r *Ring) Add(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	for i := 0; i < r.virtualNodes; i++ {
		pos := r.hashKey(fmt.Sprintf("%s#%d", addr, i))
		r.ring = append(r.ring, pos)
		r.nodeByPos[pos] = addr
	}

	sort.Slice(r.ring, func(i, j int) bool { return r.ring[i] < r.ring[j] })
}

// Remove takes a node off the ring, deleting all its virtual positions.
func (r *Ring) Remove(addr string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	// Collect positions belonging to this node.
	toDelete := make(map[uint32]struct{})
	for i := 0; i < r.virtualNodes; i++ {
		pos := r.hashKey(fmt.Sprintf("%s#%d", addr, i))
		toDelete[pos] = struct{}{}
		delete(r.nodeByPos, pos)
	}

	// Rebuild the sorted ring without those positions.
	filtered := r.ring[:0]
	for _, pos := range r.ring {
		if _, bad := toDelete[pos]; !bad {
			filtered = append(filtered, pos)
		}
	}
	r.ring = filtered
}

// Get returns the node responsible for the given key.
// It finds the first virtual node clockwise from hash(key) on the ring.
// Returns "" if the ring is empty.
func (r *Ring) Get(key string) string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	if len(r.ring) == 0 {
		return ""
	}

	pos := r.hashKey(key)

	// Binary search for the first position >= pos.
	idx := sort.Search(len(r.ring), func(i int) bool {
		return r.ring[i] >= pos
	})

	// Wrap around the ring if key hashes past the last position.
	if idx == len(r.ring) {
		idx = 0
	}

	return r.nodeByPos[r.ring[idx]]
}

// Nodes returns all unique real nodes currently on the ring.
func (r *Ring) Nodes() []string {
	r.mu.RLock()
	defer r.mu.RUnlock()

	seen := make(map[string]struct{})
	for _, addr := range r.nodeByPos {
		seen[addr] = struct{}{}
	}

	nodes := make([]string, 0, len(seen))
	for addr := range seen {
		nodes = append(nodes, addr)
	}
	sort.Strings(nodes)
	return nodes
}

// hashKey hashes a string to a uint32 position on the ring.
// We take the first 4 bytes of SHA-256 — good distribution, no crypto needed.
func (r *Ring) hashKey(key string) uint32 {
	h := sha256.Sum256([]byte(key))
	return binary.BigEndian.Uint32(h[:4])
}
