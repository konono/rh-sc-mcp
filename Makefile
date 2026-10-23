BINARY := rh-sc-mcp

.PHONY: build test fmt vet lint install clean

build:
	go build -o bin/$(BINARY) ./cmd/$(BINARY)

test:
	go test -race ./...

fmt:
	gofmt -l -w ./cmd ./internal

vet:
	go vet ./...

lint:
	golangci-lint run

install:
	go install ./cmd/$(BINARY)

clean:
	rm -rf bin
