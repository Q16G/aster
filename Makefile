VERSION ?= dev
COMMIT  := $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
TIME    := $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS := -s -w -X main.Version=$(VERSION) -X main.Commit=$(COMMIT) -X main.BuildTime=$(TIME)

.PHONY: build test vet release-dry clean

build:
	go build -ldflags "$(LDFLAGS)" -o aster ./cmd/aster

test:
	go test ./... -race -timeout 300s

vet:
	go vet ./...

release-dry:
	goreleaser release --snapshot --clean

clean:
	rm -f aster
