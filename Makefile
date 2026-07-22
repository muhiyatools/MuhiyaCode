.PHONY: fmt check-fmt vet test test-race build verify snapshot clean

VERSION ?= 1.3.0
LDFLAGS := -s -w -X github.com/muhiya/muhiyacode/internal/buildinfo.Version=$(VERSION)

fmt:
	go fmt ./...

check-fmt:
	@test -z "$$(gofmt -l .)" || (gofmt -l . && exit 1)

vet:
	go vet ./...

test:
	go test ./... -count=1

test-race:
	CGO_ENABLED=1 go test -race ./... -count=1

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/muhiyacode ./cmd/muhiyacode

verify: check-fmt vet test build

snapshot:
	goreleaser release --snapshot --clean

clean:
	go clean -testcache

