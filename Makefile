BINARY   ?= api-gateway
BIN_DIR  ?= bin
CONFIG   ?= /etc/proxy/config.yaml

GO       ?= go
GOLANGCI ?= golangci-lint

.PHONY: all build run test lint coverage clean docker-build fmt vet

all: fmt vet lint build test

build:
	$(GO) build -ldflags="-w -s" -trimpath -o $(BIN_DIR)/$(BINARY) ./cmd/

run: build
	./$(BIN_DIR)/$(BINARY) -config $(CONFIG)

test:
	$(GO) test -v -race -count=1 -coverprofile=$(BIN_DIR)/coverage.out ./...
	$(GO) tool cover -func=$(BIN_DIR)/coverage.out

lint:
	$(GOLANGCI) run ./...

coverage: test
	$(GO) tool cover -html=$(BIN_DIR)/coverage.out -o $(BIN_DIR)/coverage.html

vet:
	$(GO) vet ./...

fmt:
	$(GO) fmt ./...

clean:
	rm -rf $(BIN_DIR)

docker-build:
	docker build -t $(BINARY) .

docker-buildx:
	docker buildx build --platform linux/amd64 -t $(BINARY) .

lint-ci:
	golangci-lint run ./... --output.json.path=lint-report.json
	scripts/lint-to-issues.sh
	test ! -s lint-report.json

install-hooks:
	cp .githooks/pre-push .git/hooks/pre-push
	chmod +x .git/hooks/pre-push
	@echo "✅ Pre-push hook installed"
