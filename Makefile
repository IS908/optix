SHELL := /bin/bash
.PHONY: build build-cli build-server tidy test test-integration clean proto py-server

PYTHON := python/.venv/bin/python

# Build
build: build-cli build-server

build-cli:
	go build -o bin/optix ./cmd/optix-cli

build-server:
	go build -o bin/optix-server ./cmd/optix-server

# Dependencies
tidy:
	go mod tidy

# Unit tests (no external services required)
test:
	go test ./...
	$(PYTHON) -m pytest python/tests/ -v

# Integration tests: starts Python gRPC server, runs Go tests, stops server
test-integration:
	@echo "Starting Python analysis server..."
	@$(PYTHON) -m optix_engine.grpc_server.server --addr=localhost:50052 & \
	PYPID=$$! ; \
	for i in $$(seq 1 120); do \
		nc -z localhost 50052 2>/dev/null && break ; \
		if ! kill -0 $$PYPID 2>/dev/null; then echo "Python server exited unexpectedly"; exit 1; fi ; \
		sleep 1 ; \
	done ; \
	if ! nc -z localhost 50052 2>/dev/null; then echo "Python server failed to start within 120s"; kill $$PYPID 2>/dev/null; exit 1; fi ; \
	echo "Python analysis server ready." ; \
	go test -tags=integration -v -timeout=60s ./internal/analysis/ ; \
	STATUS=$$? ; kill $$PYPID 2>/dev/null ; exit $$STATUS

# Clean
clean:
	rm -rf bin/
	rm -rf data/optix.db

# Proto codegen (requires: go install buf + protoc-gen-go + protoc-gen-go-grpc)
proto:
	./scripts/proto-gen.sh

# Run CLI
run-cli:
	go run ./cmd/optix-cli $(ARGS)

# Run server
run-server:
	go run ./cmd/optix-server

# Start Python analysis gRPC server (foreground)
py-server:
	$(PYTHON) -m optix_engine.grpc_server.server --addr=localhost:50052
