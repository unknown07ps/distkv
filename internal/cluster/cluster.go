// Package cluster wires the consistent hash ring and gossip monitor together.
//
// It owns:
//   - The ring: which node owns which key
//   - The node registry: all known nodes in the cluster
//   - Ring updates on gossip events: dead nodes removed, revived nodes re-added
//
// This is where CAP becomes concrete: when a node is removed from the ring
// due to failure, its keys are served by the next node clockwise. If that
// node has a replica, reads continue. If not (replica count < ring size),
// reads fail — we chose CP: refuse to serve potentially stale data.
package cluster

import (
	"sync"

	"github.com/you/madonna/internal/gossip"
	"github.com/you/madonna/internal/hash"
)

// Cluster manages the set of nodes and routes keys to nodes.
type Cluster struct {
	mu       sync.RWMutex
	selfAddr string
	ring     *hash.Ring
	monitor  *gossip.Monitor
	allNodes []string // all configured nodes (alive or dead)
}

// New builds a Cluster for the node at selfAddr.
// peers is the full list of other node addresses in the cluster.
func New(selfAddr string, peers []string) *Cluster {
	ring := hash.New()
	ring.Add(selfAddr)

	monitor := gossip.New(selfAddr)

	c := &Cluster{
		selfAddr: selfAddr,
		ring:     ring,
		monitor:  monitor,
		allNodes: append([]string{selfAddr}, peers...),
	}

	// Register all peers with the ring and gossip monitor.
	for _, peer := range peers {
		ring.Add(peer)
		monitor.AddPeer(peer)
	}

	// Wire gossip callbacks to ring mutations.
	monitor.OnDead = func(addr string) {
		c.ring.Remove(addr)
	}
	monitor.OnRevive = func(addr string) {
		c.ring.Add(addr)
	}

	monitor.Start()
	return c
}

// OwnerOf returns the node address responsible for the given key.
// The returned address may be the local node or a remote node.
func (c *Cluster) OwnerOf(key string) string {
	return c.ring.Get(key)
}

// IsSelf returns true if addr is this node.
func (c *Cluster) IsSelf(addr string) bool {
	return addr == c.selfAddr
}

// Self returns this node's address.
func (c *Cluster) Self() string {
	return c.selfAddr
}

// AliveNodes returns all nodes currently considered alive (including self).
func (c *Cluster) AliveNodes() []string {
	alive := c.monitor.AliveNodes()
	return append(alive, c.selfAddr)
}

// Peers returns all alive nodes that are not self.
func (c *Cluster) Peers() []string {
	return c.monitor.AliveNodes()
}

// Monitor returns the underlying gossip monitor (for HTTP handler mounting).
func (c *Cluster) Monitor() *gossip.Monitor {
	return c.monitor
}
