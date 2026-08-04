GO ?= go
BIN := bin/browser

.PHONY: all build test vet clean

all: build

build:
	cd cmd/browser && $(GO) build -o ../../$(BIN) .

test: vet
	$(GO) test ./...

vet:
	$(GO) vet ./...

clean:
	rm -f $(BIN)
