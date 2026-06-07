// CLI client for the distkv cluster.
//
// Usage:
//   ./cli -node node1:8080 put mykey myvalue
//   ./cli -node node1:8080 get mykey
//   ./cli -node node1:8080 del mykey
//   ./cli -node node1:8080 status
//
// The -node flag can point at ANY node — the cluster routes to the
// correct owner via the consistent hash ring. This demonstrates that
// the distributed routing is transparent to clients.
package main

import (
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strings"
	"time"
)

func main() {
	node := flag.String("node", "localhost:8080", "any cluster node address")
	flag.Parse()

	args := flag.Args()
	if len(args) == 0 {
		log.Fatal("usage: cli [-node addr] <get|put|del|status> [key] [value]")
	}

	client := &http.Client{Timeout: 5 * time.Second}
	cmd := args[0]

	switch cmd {
	case "get":
		if len(args) < 2 {
			log.Fatal("get requires a key")
		}
		doGet(client, *node, args[1])

	case "put":
		if len(args) < 3 {
			log.Fatal("put requires key and value")
		}
		doPut(client, *node, args[1], args[2])

	case "del":
		if len(args) < 2 {
			log.Fatal("del requires a key")
		}
		doDel(client, *node, args[1])

	case "status":
		doStatus(client, *node)

	default:
		log.Fatalf("unknown command: %s", cmd)
	}
}

func doGet(client *http.Client, node, key string) {
	resp, err := client.Get(fmt.Sprintf("http://%s/key/%s", node, key))
	if err != nil {
		log.Fatalf("get error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode == http.StatusNotFound {
		fmt.Printf("(not found)\n")
		return
	}
	if resp.StatusCode != http.StatusOK {
		log.Fatalf("get: unexpected status %d: %s", resp.StatusCode, body)
	}
	fmt.Printf("%s\n", body)
	if servedBy := resp.Header.Get("X-Served-By"); servedBy != "" {
		fmt.Printf("(routed to %s)\n", servedBy)
	}
}

func doPut(client *http.Client, node, key, value string) {
	req, _ := http.NewRequest(http.MethodPut,
		fmt.Sprintf("http://%s/key/%s", node, key),
		strings.NewReader(value),
	)
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("put error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("put: unexpected status %d: %s", resp.StatusCode, body)
	}
	fmt.Printf("OK\n")
}

func doDel(client *http.Client, node, key string) {
	req, _ := http.NewRequest(http.MethodDelete,
		fmt.Sprintf("http://%s/key/%s", node, key),
		nil,
	)
	resp, err := client.Do(req)
	if err != nil {
		log.Fatalf("del error: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusNoContent {
		body, _ := io.ReadAll(resp.Body)
		log.Fatalf("del: unexpected status %d: %s", resp.StatusCode, body)
	}
	fmt.Printf("OK\n")
}

func doStatus(client *http.Client, node string) {
	resp, err := client.Get(fmt.Sprintf("http://%s/internal/status", node))
	if err != nil {
		log.Fatalf("status error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("%s\n", body)
}
