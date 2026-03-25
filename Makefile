BINARY     := arcline-portal
MODULE     := arclineit/arcline-portal
GO         := go
GOFLAGS    := -trimpath -ldflags="-s -w"

.PHONY: build run linux-amd64 linux-arm64 all test clean

build:
	$(GO) build $(GOFLAGS) -o $(BINARY) .

run:
	$(GO) run .

linux-amd64:
	GOOS=linux GOARCH=amd64 $(GO) build $(GOFLAGS) -o $(BINARY)-linux-amd64 .

linux-arm64:
	GOOS=linux GOARCH=arm64 $(GO) build $(GOFLAGS) -o $(BINARY)-linux-arm64 .

all: linux-amd64 linux-arm64

test:
	$(GO) test ./...

clean:
	rm -f $(BINARY) $(BINARY)-linux-amd64 $(BINARY)-linux-arm64
