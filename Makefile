# folio — common tasks. The release build is scripts/build.sh.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PREFIX  ?= $(HOME)/.local
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test dist install uninstall clean

build:
	CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "$(LDFLAGS)" -o folio ./cmd/folio

test:
	go vet ./...
	go test ./...

dist:
	sh scripts/build.sh $(VERSION)

install: build
	mkdir -p $(PREFIX)/bin
	cp folio $(PREFIX)/bin/.folio.new && mv -f $(PREFIX)/bin/.folio.new $(PREFIX)/bin/folio
	@echo "installed $(PREFIX)/bin/folio"

uninstall:
	rm -f $(PREFIX)/bin/folio

clean:
	rm -rf dist folio
