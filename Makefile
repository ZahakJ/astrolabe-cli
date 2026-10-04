# astrolabe — common tasks. The release build is scripts/build.sh.

VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
PREFIX  ?= $(HOME)/.local
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build test dist install uninstall clean

build:
	CGO_ENABLED=0 go build -trimpath -buildvcs=false -ldflags "$(LDFLAGS)" -o astrolabe ./cmd/astrolabe

test:
	go vet ./...
	go test ./...

dist:
	sh scripts/build.sh $(VERSION)

install: build
	mkdir -p $(PREFIX)/bin
	cp astrolabe $(PREFIX)/bin/.astrolabe.new && mv -f $(PREFIX)/bin/.astrolabe.new $(PREFIX)/bin/astrolabe
	@echo "installed $(PREFIX)/bin/astrolabe"

uninstall:
	rm -f $(PREFIX)/bin/astrolabe

clean:
	rm -rf dist astrolabe
