BINARY_NODE = ./bin/node
BINARY_CLI  = ./bin/cli

.PHONY: all build test bench cluster-up cluster-down clean

all: build

build:
	go build -o $(BINARY_NODE) ./cmd/node
	go build -o $(BINARY_CLI)  ./cmd/cli

test:
	go test ./... -v -timeout 30s

bench:
	go test ./tests/ -run='^$$' -bench=. -benchmem -benchtime=5s

cluster-up:
	docker compose up --build -d
	@echo "cluster running:"
	@echo "  node1 -> localhost:8080"
	@echo "  node2 -> localhost:8081"
	@echo "  node3 -> localhost:8082"

cluster-down:
	docker compose down

# Example usage targets — demonstrate the cluster works.
demo-put:
	curl -s -X PUT localhost:8080/key/hello -d world && echo
	curl -s -X PUT localhost:8080/key/foo   -d bar   && echo
	curl -s -X PUT localhost:8080/key/baz   -d qux   && echo

demo-get:
	@echo "reading from node1:"; curl -s localhost:8080/key/hello && echo
	@echo "reading from node2:"; curl -s localhost:8081/key/hello && echo
	@echo "reading from node3:"; curl -s localhost:8082/key/hello && echo

clean:
	rm -rf ./bin
	docker compose down -v 2>/dev/null || true
