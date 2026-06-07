// Package store is the core key-value engine.
//
// Every write goes to the WAL first, then applies to the in-memory map.
// On startup, the WAL is replayed to rebuild state after a crash.
// Reads are served entirely from memory — O(1), no disk access.
package store

import (
	"fmt"
	"sync"

	"github.com/you/distkv/internal/wal"
)

// Store is a durable in-memory key-value store.
type Store struct {
	mu   sync.RWMutex
	data map[string]string
	log  *wal.WAL
}

// New opens the WAL at walPath and replays it to restore state.
// Returns a Store ready to serve reads and writes.
func New(walPath string) (*Store, error) {
	w, err := wal.Open(walPath)
	if err != nil {
		return nil, fmt.Errorf("store: open wal: %w", err)
	}

	s := &Store{
		data: make(map[string]string),
		log:  w,
	}

	// Replay applies all prior writes so in-memory state matches disk.
	if err := w.Replay(func(e wal.Entry) error {
		switch e.Op {
		case wal.OpPut:
			s.data[e.Key] = e.Value
		case wal.OpDelete:
			delete(s.data, e.Key)
		}
		return nil
	}); err != nil {
		return nil, fmt.Errorf("store: wal replay: %w", err)
	}

	return s, nil
}

// Put sets key to value. Durable on return.
func (s *Store) Put(key, value string) error {
	// Write to WAL before touching memory — the order is load-bearing.
	if err := s.log.Append(wal.Entry{Op: wal.OpPut, Key: key, Value: value}); err != nil {
		return fmt.Errorf("store put wal: %w", err)
	}
	s.mu.Lock()
	s.data[key] = value
	s.mu.Unlock()
	return nil
}

// Get returns the value for key and whether it exists.
func (s *Store) Get(key string) (string, bool) {
	s.mu.RLock()
	v, ok := s.data[key]
	s.mu.RUnlock()
	return v, ok
}

// Delete removes key. Durable on return.
func (s *Store) Delete(key string) error {
	if err := s.log.Append(wal.Entry{Op: wal.OpDelete, Key: key}); err != nil {
		return fmt.Errorf("store delete wal: %w", err)
	}
	s.mu.Lock()
	delete(s.data, key)
	s.mu.Unlock()
	return nil
}

// Snapshot returns a copy of all current key-value pairs.
// Used by replication to send full state to a new or rejoining node.
func (s *Store) Snapshot() map[string]string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	snap := make(map[string]string, len(s.data))
	for k, v := range s.data {
		snap[k] = v
	}
	return snap
}

// ApplySnapshot replaces in-memory state with the provided snapshot.
// Called on a node that is catching up after rejoining the cluster.
// The WAL is NOT updated here — that is the caller's responsibility.
func (s *Store) ApplySnapshot(snap map[string]string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data = make(map[string]string, len(snap))
	for k, v := range snap {
		s.data[k] = v
	}
}

// Close flushes and closes the WAL.
func (s *Store) Close() error {
	return s.log.Close()
}
