BINARY ?= api-gateway
CONFIG ?= /etc/proxy/config.yaml

.PHONY: build run test lint clean docker-build coverage

build:
	go build -ldflags="-w -s" -trimpath -o $(BINARY) ./cmd/

run: build
	./$(BINARY) -config $(CONFIG)

test:
	go test -v -race -count=1 ./...

lint:
	golangci-lint run ./...

coverage:
	go test -race -count=1 -coverprofile=coverage.out ./...
	go tool cover -html=coverage.out -o coverage.html

clean:
	rm -f $(BINARY) coverage.out coverage.html

docker-build:
	docker build -t $(BINARY) .
