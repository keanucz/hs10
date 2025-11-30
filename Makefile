.PHONY: build run dev clean test docker-build docker-up docker-down llama-bindings

build:
	@echo "Building replychat..."
	@go build -o replychat ./src
	@echo "Build complete: ./replychat"

run: llama-bindings build
	@echo "Starting replychat..."
	@./replychat

dev: llama-bindings
	@echo "Running in development mode..."
	@go run ./src

clean:
	@echo "Cleaning build artifacts..."
	@rm -f replychat
	@rm -f data/*.db data/*.db-shm data/*.db-wal
	@echo "Clean complete"

test:
	@echo "Running tests..."
	@go test ./...

deps:
	@echo "Downloading dependencies..."
	@go mod download
	@go mod tidy

docker-build:
	@echo "Building Docker image..."
	@docker-compose build

docker-up:
	@echo "Starting Docker containers..."
	@docker-compose up

docker-down:
	@echo "Stopping Docker containers..."
	@docker-compose down

llama-bindings: go-llama.cpp/libbinding.a

go-llama.cpp/libbinding.a:
	@echo "Building llama.cpp bindings..."
	@$(MAKE) -C go-llama.cpp libbinding.a >/dev/null

help:
	@echo "Available commands:"
	@echo "  make build        - Build the binary"
	@echo "  make run          - Build and run the application"
	@echo "  make dev          - Run in development mode (no build)"
	@echo "  make clean        - Remove build artifacts and database"
	@echo "  make test         - Run tests"
	@echo "  make deps         - Download and tidy dependencies"
	@echo "  make docker-build - Build Docker image"
	@echo "  make docker-up    - Start Docker containers"
	@echo "  make docker-down  - Stop Docker containers"
	@echo "  make help         - Show this help message"
