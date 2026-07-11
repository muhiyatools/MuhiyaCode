# Contributing

MuhiyaCode is a Go 1.25 module. Keep package dependencies pointing inward toward `internal/contract`; concrete lifetimes belong in `internal/command`.

Before proposing a change:

```sh
go mod verify
go fmt ./...
go vet ./...
go test ./... -count=1
go build -trimpath ./cmd/muhiyacode
```

Run `CGO_ENABLED=1 go test -race ./... -count=1` on a supported toolchain. Add focused tests for failure, cancellation, persistence, migration, permission, and narrow-terminal behavior—not only the success path.

Do not add a TypeScript/Node/Bun runtime, store secrets in settings or fixtures, weaken real-path containment, make tool budgets hard blockers, or change the v1 state contract without an explicit migration and rollback plan. Prompt changes must preserve byte stability and remain within the tested size ceilings.

