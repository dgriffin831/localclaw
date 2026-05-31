# Testing

Primary validation:

```bash
go test ./...
```

Focused packages:

```bash
go test ./cmd/localclaw
go test ./internal/config
go test ./internal/runtime
go test ./internal/mcp
go test ./internal/mcp/tools
go test ./internal/memory
go test ./internal/workspace
go test ./internal/cli
```

Smoke commands:

```bash
go run ./cmd/localclaw doctor
go run ./cmd/localclaw memory status
go run ./cmd/localclaw mcp serve
go run ./cmd/localclaw setup opencode --dry-run
```

When Go files change:

```bash
go fmt ./...
```

Before delivery, run `go test ./...` and report any skipped validation.
