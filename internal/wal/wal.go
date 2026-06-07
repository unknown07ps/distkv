// Package wal implements a write-ahead log for crash recovery.
//
// Why this exists: if a node crashes mid-write, in-memory data is lost.
// The WAL persists every write to disk BEFORE applying it to memory.
// On restart, the node replays the log to reconstruct in-memory state.
//
// Format: each entry is a length-prefixed JSON line.
//   [4-byte big-endian length][JSON payload\n]
//
// This is deliberately simple — production systems use binary encoding
// (e.g. protobuf) for efficiency. The structure is what matters.
package wal

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
)

// OpType identifies the operation recorded in a log entry.
type OpType string

const (
	OpPut    OpType = "PUT"
	OpDelete OpType = "DELETE"
)

// Entry is one record in the WAL.
type Entry struct {
	Op    OpType `json:"op"`
	Key   string `json:"key"`
	Value string `json:"value,omitempty"` // empty for DELETE
}

// WAL is a write-ahead log backed by a file.
type WAL struct {
	mu   sync.Mutex
	file *os.File
	buf  *bufio.Writer
}

// Open opens (or creates) the WAL file at the given path.
func Open(path string) (*WAL, error) {
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR|os.O_APPEND, 0644)
	if err != nil {
		return nil, fmt.Errorf("wal open %s: %w", path, err)
	}
	return &WAL{
		file: f,
		buf:  bufio.NewWriterSize(f, 64*1024),
	}, nil
}

// Append writes an entry to the log and syncs to disk before returning.
// This guarantees durability: if Append returns nil, the entry survived a crash.
func (w *WAL) Append(e Entry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	data, err := json.Marshal(e)
	if err != nil {
		return fmt.Errorf("wal marshal: %w", err)
	}
	data = append(data, '\n')

	// Write length prefix so we can detect truncated entries on replay.
	var lenBuf [4]byte
	binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
	if _, err := w.buf.Write(lenBuf[:]); err != nil {
		return fmt.Errorf("wal write len: %w", err)
	}
	if _, err := w.buf.Write(data); err != nil {
		return fmt.Errorf("wal write data: %w", err)
	}

	// Flush buffer then fsync — both steps required for durability.
	if err := w.buf.Flush(); err != nil {
		return fmt.Errorf("wal flush: %w", err)
	}
	if err := w.file.Sync(); err != nil {
		return fmt.Errorf("wal sync: %w", err)
	}
	return nil
}

// Replay reads all entries from the beginning of the WAL and calls fn for each.
// Stops at the first truncated or corrupt entry (crash mid-write).
// Must be called before any Append calls — i.e. at startup.
func (w *WAL) Replay(fn func(Entry) error) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("wal seek: %w", err)
	}

	r := bufio.NewReader(w.file)
	for {
		var lenBuf [4]byte
		if _, err := io.ReadFull(r, lenBuf[:]); err != nil {
			if err == io.EOF || err == io.ErrUnexpectedEOF {
				break // clean end or truncated length prefix — stop
			}
			return fmt.Errorf("wal read len: %w", err)
		}
		size := binary.BigEndian.Uint32(lenBuf[:])

		data := make([]byte, size)
		if _, err := io.ReadFull(r, data); err != nil {
			// Truncated entry — crash happened mid-write, discard it.
			break
		}

		var e Entry
		if err := json.Unmarshal(data[:len(data)-1], &e); err != nil {
			// Corrupt entry — stop here.
			break
		}

		if err := fn(e); err != nil {
			return err
		}
	}
	return nil
}

// Close flushes and closes the underlying file.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if err := w.buf.Flush(); err != nil {
		return err
	}
	return w.file.Close()
}

// Truncate rewrites the WAL with only the provided entries.
// Used after a snapshot to keep the log from growing unbounded.
func (w *WAL) Truncate(entries []Entry) error {
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.file.Truncate(0); err != nil {
		return fmt.Errorf("wal truncate: %w", err)
	}
	if _, err := w.file.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("wal seek after truncate: %w", err)
	}
	w.buf.Reset(w.file)

	for _, e := range entries {
		data, _ := json.Marshal(e)
		data = append(data, '\n')
		var lenBuf [4]byte
		binary.BigEndian.PutUint32(lenBuf[:], uint32(len(data)))
		w.buf.Write(lenBuf[:])
		w.buf.Write(data)
	}
	w.buf.Flush()
	return w.file.Sync()
}
