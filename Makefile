BIN     := docsli
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test lint vuln run docker clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/$(BIN) ./cmd/docsli

test:
	go test -race ./...

lint:
	golangci-lint run

vuln:
	govulncheck ./...

run: build
	./bin/$(BIN) -config config.yml

docker:
	docker build -t docsli .

clean:
	rm -rf bin
