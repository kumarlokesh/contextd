BINARY     := contextd
CMD        := ./cmd/contextd
BIN_DIR    := bin

VERSION    ?= dev
COMMIT     := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILD_DATE := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)

LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)"

# sqlite_fts5 enables FTS5 in mattn/go-sqlite3 (required; always set).
TAGS := sqlite_fts5

.PHONY: build test test-cover lint fmt vet tidy clean run

build:
	@mkdir -p $(BIN_DIR)
	go build -tags "$(TAGS)" $(LDFLAGS) -o $(BIN_DIR)/$(BINARY) $(CMD)

test:
	go test -tags "$(TAGS)" -race ./...

test-cover:
	go test -tags "$(TAGS)" -race -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

lint:
	golangci-lint run --build-tags "$(TAGS)" ./...

fmt:
	gofmt -w .

vet:
	go vet -tags "$(TAGS)" ./...

tidy:
	go mod tidy

clean:
	rm -rf $(BIN_DIR)/ coverage.out coverage.html

run: build
	./$(BIN_DIR)/$(BINARY) serve

.DEFAULT_GOAL := build
