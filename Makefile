SHELL := /bin/bash
.PHONY: build build-cli build-server tidy test lint-python test-integration clean proto py-server release release-all release-smoke release-clean web web-deps build-full

PYTHON := python/.venv/bin/python

# Build
build: build-cli build-server

build-cli:
	go build -o bin/optix ./cmd/optix-cli

build-server:
	go build -o bin/optix-server ./cmd/optix-server

# ─── Web SPA（Market Intel /intel/）────────────────────────────────────────
# dist 产物经 go:embed 进二进制；占位 web/dist/index.html 保证纯 Go 构建可用。
web-deps:
	@if [ ! -d web/node_modules ]; then cd web && npm ci; fi

web: web-deps
	cd web && npm run build
	@echo "✓ web/dist 已构建（占位 index.html 被覆盖 —— 勿提交；还原: git checkout web/dist/index.html）"

# 完整产物：SPA + 双二进制（发布与本地验收用；纯 Go 开发用 build 即可）
build-full: web build

# Dependencies
tidy:
	go mod tidy

# Unit tests (no external services required)
test:
	go test ./...
	$(PYTHON) -m pytest python/tests/ -v

lint-python:
	$(PYTHON) -m ruff check python/src python/tests

# Integration tests: starts Python gRPC server, runs Go tests, stops server
test-integration:
	@echo "Starting Python analysis server..."
	@READY_FILE=$$(mktemp -t optix-ready.XXXXXX) && rm -f "$$READY_FILE" ; \
	$(PYTHON) -m optix_engine.grpc_server.server --addr=localhost:50052 --ready-file="$$READY_FILE" & \
	PYPID=$$! ; \
	for i in $$(seq 1 600); do \
		[ -f "$$READY_FILE" ] && break ; \
		if ! kill -0 $$PYPID 2>/dev/null; then echo "Python server exited unexpectedly"; rm -f "$$READY_FILE"; exit 1; fi ; \
		sleep 0.2 ; \
	done ; \
	if [ ! -f "$$READY_FILE" ]; then echo "Python server failed to start within 120s"; kill $$PYPID 2>/dev/null; rm -f "$$READY_FILE"; exit 1; fi ; \
	echo "Python analysis server ready." ; \
	go test -tags=integration -v -timeout=60s ./internal/analysis/ ; \
	STATUS=$$? ; kill $$PYPID 2>/dev/null ; rm -f "$$READY_FILE" ; exit $$STATUS

# Clean
clean:
	rm -rf bin/
	rm -rf data/optix.db

# Proto codegen (requires: go install buf + protoc-gen-go + protoc-gen-go-grpc)
proto:
	./scripts/proto-gen.sh

# Sync the embedded sectors fallback after editing configs/sectors.json.
# A drift-detection test (internal/portfolio/embed_test.go) will catch any
# divergence before release.
sync-sectors:
	cp configs/sectors.json internal/portfolio/default_sectors.json
	@echo "✓ internal/portfolio/default_sectors.json synced from configs/sectors.json"

# Run CLI
run-cli:
	go run ./cmd/optix-cli $(ARGS)

# Run server
run-server:
	go run ./cmd/optix-server

# Start Python analysis gRPC server (foreground)
py-server:
	$(PYTHON) -m optix_engine.grpc_server.server --addr=localhost:50052

# ─── Release ─────────────────────────────────────────────────────────────────
# Build a single-platform tarball at dist/optix-skill-$(VERSION)-$(GOOS)-$(GOARCH).tar.gz
#   make release VERSION=v1.2.0                    # uses host GOOS/GOARCH
#   make release VERSION=v1.2.0 GOOS=linux GOARCH=arm64
release:
	@if [ -z "$(VERSION)" ]; then echo "ERROR: VERSION is required (e.g. make release VERSION=v1.2.0)" >&2; exit 2; fi
	VERSION="$(VERSION)" GOOS="$(GOOS)" GOARCH="$(GOARCH)" ./scripts/build-release.sh

# Build all four supported platforms — convenience target for local pre-flight.
release-all:
	@if [ -z "$(VERSION)" ]; then echo "ERROR: VERSION is required" >&2; exit 2; fi
	@for combo in darwin/arm64 darwin/amd64 linux/amd64 linux/arm64 ; do \
		os=$${combo%/*} ; arch=$${combo#*/} ; \
		$(MAKE) --no-print-directory release VERSION=$(VERSION) GOOS=$$os GOARCH=$$arch ; \
	done

release-smoke:
	./scripts/test-release-packaging.sh

release-clean:
	rm -rf dist/
