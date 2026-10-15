# Building from source

Go 1.22 or later.

```sh
git clone https://github.com/ZahakJ/astrolabe-cli && cd astrolabe-cli
make build          # ./astrolabe, static, stripped
make test           # go vet + go test ./...
make install        # into ~/.local/bin (PREFIX=... to change)
make dist           # all four release binaries and checksums.txt in dist/
```

`scripts/build.sh VERSION` is the release build. It runs `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=VERSION"` for linux/amd64, linux/arm64, darwin/amd64 and darwin/arm64, then writes `dist/checksums.txt`.
