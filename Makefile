.PHONY: build run test lint clean docker-build docker-run download-model ngrok help

# ── Variables ─────────────────────────────────────────────────────────────────
BINARY      := reviewer
BUILD_DIR   := .
CMD         := ./cmd/server
DOCKER_TAG  := ai-code-reviewer:latest
PORT        := 3000

# ── Development ───────────────────────────────────────────────────────────────

## build: Compile the Go binary
build:
	go build -o $(BUILD_DIR)/$(BINARY) $(CMD)

## run: Run the server locally (requires .env to be set up)
run:
	go run $(CMD)

## test: Run all unit tests
test:
	go test ./... -v -count=1

## test-short: Run tests without verbose output
test-short:
	go test ./...

## lint: Run go vet and staticcheck
lint:
	go vet ./...
	@which staticcheck > /dev/null 2>&1 && staticcheck ./... || echo "staticcheck not installed — run: go install honnef.co/go/tools/cmd/staticcheck@latest"

## clean: Remove build artefacts
clean:
	rm -f $(BINARY)
	go clean -testcache

## tidy: Tidy and vendor modules
tidy:
	go mod tidy
	go mod vendor

# ── Local LLM ─────────────────────────────────────────────────────────────────

## download-model: Download the Qwen2.5-Coder-7B GGUF model (~5.2 GB)
download-model:
	bash scripts/download-model.sh

## llm: Start the llama.cpp server natively (brew install llama.cpp first)
llm:
	@if [ ! -f models/qwen2.5-coder-7b-instruct-q5_k_m.gguf ]; then \
		echo "Model not found. Run: make download-model"; exit 1; \
	fi
	@which llama-server > /dev/null 2>&1 || (echo "llama.cpp not found. Run: brew install llama.cpp" && exit 1)
	llama-server \
		-m models/qwen2.5-coder-7b-instruct-q5_k_m.gguf \
		--host 0.0.0.0 \
		--port 8080 \
		--ctx-size 8192 \
		--threads $(shell sysctl -n hw.ncpu 2>/dev/null || echo 4) \
		--parallel 2

# ── Docker ────────────────────────────────────────────────────────────────────

## docker-build: Build the Docker image
docker-build:
	docker build -t $(DOCKER_TAG) .

## docker-run: Run just the app container (assumes external LLM or API provider)
docker-run:
	docker run --rm --env-file .env -p $(PORT):$(PORT) $(DOCKER_TAG)

## up: Start the full local stack (app + llama.cpp) via docker compose
up:
	docker compose up --build

## down: Stop the local stack
down:
	docker compose down

## logs: Follow logs from the app container
logs:
	docker compose logs -f app

# ── Tunnel ────────────────────────────────────────────────────────────────────

## ngrok: Expose local port to GitHub webhooks (requires ngrok installed)
ngrok:
	ngrok http $(PORT)

# ── Setup ─────────────────────────────────────────────────────────────────────

## env: Copy .env.example to .env if .env doesn't exist
env:
	@if [ ! -f .env ]; then cp .env.example .env && echo "Created .env — fill in your credentials"; \
	else echo ".env already exists"; fi

## help: Show this help message
help:
	@grep -E '^## ' Makefile | sed 's/^## //' | column -t -s ':'
