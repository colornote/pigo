# PiGo — pi in Go
#
# Usage:
#   make build          Build ./pigo binary (default)
#   make install        Build and install to $(PREFIX)/bin (default /usr/local/bin)
#   make install-local  Install to GOPATH/bin (~/go/bin) — no sudo needed
#   make test           Run tests
#   make clean          Remove build artifacts

BINARY  := pigo
PACKAGE := ./...
PREFIX  ?= /usr/local

.PHONY: build install install-local test clean

build:
	go build -o $(BINARY) .

install: build
	install -d $(DESTDIR)$(PREFIX)/bin
	install -m 0755 $(BINARY) $(DESTDIR)$(PREFIX)/bin/$(BINARY)
	@echo "✓ installed $(BINARY) to $(PREFIX)/bin/$(BINARY)"

install-local: build
	install -d "$$(go env GOPATH)/bin"
	install -m 0755 $(BINARY) "$$(go env GOPATH)/bin/$(BINARY)"
	@echo "✓ installed $(BINARY) to $$(go env GOPATH)/bin/$(BINARY)"

test:
	go test $(PACKAGE)

clean:
	rm -f $(BINARY)
