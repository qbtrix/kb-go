# Makefile — Build cross-platform binaries for kb-go
VERSION ?= 0.1.0
BIN = kb
LDFLAGS = -s -w

.PHONY: build test bench clean release

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) .

test:
	go test -v -count=1 ./...

bench:
	go test -bench=. -benchmem ./...

clean:
	rm -f $(BIN) kb-*

release: clean
	GOOS=darwin GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o kb-darwin-arm64 .
	GOOS=darwin GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o kb-darwin-amd64 .
	GOOS=linux GOARCH=arm64 go build -ldflags "$(LDFLAGS)" -o kb-linux-arm64 .
	GOOS=linux GOARCH=amd64 go build -ldflags "$(LDFLAGS)" -o kb-linux-amd64 .
	@echo "Built 4 binaries for v$(VERSION)"
	@ls -lh kb-*
