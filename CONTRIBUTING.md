# Contributing

Open an issue before changing a public command, configuration field, generated file contract, profile rule, or authority boundary. Bug fixes should include a fixture that fails before the fix and proves why the behavior matters.

Run these checks before submitting a pull request:

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/sam-harness
python3 scripts/validate-skill.py skills/sam-harness
```

Keep generated output backward compatible within a schema version. Preserve existing repository content and avoid adding a dependency when the standard library can implement the behavior clearly.
