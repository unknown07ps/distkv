// Node entrypoint. Starts a single distkv node.
//
// Usage:
//   DISTKV_ADDR=node1:8080 DISTKV_PEERS=node2:8081,node3:8082 \
//   DISTKV_WAL=/data/wal.log ./node
//
// All configuration is via environment variables so Docker Compose
// can inject values without rebuilding the binary.
package main

import (
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/you/distkv/internal/cluster"
	"github.com/you/distkv/internal/replication"
	"github.com/you/distkv/internal/server"
	"github.com/you/distkv/internal/store"
)

func main() {
	// Configuration from environment.
	addr := mustEnv("DISTKV_ADDR")     // this node's address, e.g. node1:8080
	walPath := getEnv("DISTKV_WAL", "/tmp/distkv-wal.log")
	peersRaw := os.Getenv("DISTKV_PEERS") // comma-separated peer addresses

	var peers []string
	if peersRaw != "" {
		for _, p := range strings.Split(peersRaw, ",") {
			p = strings.TrimSpace(p)
			if p != "" && p != addr {
				peers = append(peers, p)
			}
		}
	}

	log.Printf("starting node addr=%s peers=%v wal=%s", addr, peers, walPath)

	// Layer 1: durable store (WAL + in-memory map).
	st, err := store.New(walPath)
	if err != nil {
		log.Fatalf("store init: %v", err)
	}
	defer st.Close()

	// Layer 2: cluster (consistent hash ring + gossip failure detection).
	cl := cluster.New(addr, peers)

	// Layer 3: replication (async write propagation to alive peers).
	repl := replication.New(cl.Peers)

	// Layer 4: HTTP server.
	srv := server.New(st, cl, repl)

	log.Printf("listening on %s", addr)
	if err := http.ListenAndServe(addr, srv.Handler()); err != nil {
		log.Fatalf("http server: %v", err)
	}
}

func mustEnv(key string) string {
	v := os.Getenv(key)
	if v == "" {
		log.Fatalf("required env var %s not set", key)
	}
	return v
}

func getEnv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
