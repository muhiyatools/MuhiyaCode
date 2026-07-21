.PHONY: fmt check-fmt vet test test-race build verify snapshot clean

VERSION ?= 1.0.0-dev
LDFLAGS := -s -w -X github.com/muhiya/muhiyacode/internal/buildinfo.Version=$(VERSION)

fmt:
	go fmt ./...

check-fmt:
	@test -z "$$(gofmt -l cmd internal benchmarks)" || (gofmt -l cmd internal benchmarks && exit 1)

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
