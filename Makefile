.PHONY: fmt check-fmt vet staticcheck vuln test test-race fuzz chaos benchmark-check docs-check build verify snapshot clean

VERSION ?= 1.3.2
LDFLAGS := -s -w -X github.com/muhiya/muhiyacode/internal/buildinfo.Version=$(VERSION)

fmt:
	go fmt ./...

check-fmt:
	@test -z "$$(gofmt -l cmd internal)" || (gofmt -l cmd internal && exit 1)

vet:
	go vet ./...

staticcheck:
	staticcheck ./...

vuln:
	govulncheck ./...

test:
	go test ./... -count=1

test-race:
	CGO_ENABLED=1 go test -race ./... -count=1

fuzz:
	go test ./internal/orchestrator -run=^$$ -fuzz=FuzzValidateCallArgsNeverPanics -fuzztime=20s
	go test ./internal/tui -run=^$$ -fuzz=FuzzPasteRoundTrip -fuzztime=20s
	go test ./internal/tui -run=^$$ -fuzz=FuzzRenderForDisplay -fuzztime=20s

chaos:
	go test ./internal/orchestrator -run=TestFaultInjectionCatalog -count=10
	go test ./internal/workspace -run='Test(PatchJournal|Sandbox|ShellRunner)' -count=5

benchmark-check:
	go run ./cmd/muhiyacode benchmark --manifest benchmarks/manifest.example.json --validate-only

docs-check:
	go run ./cmd/muhiyacode docs check

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o bin/muhiyacode ./cmd/muhiyacode

verify: check-fmt vet staticcheck test benchmark-check docs-check build

snapshot:
	goreleaser release --snapshot --clean

clean:
	go clean -testcache
