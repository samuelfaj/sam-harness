# Stack detection and gates

Sam Harness supports mixed repositories. Treat every detected workspace as its own command and ownership boundary when its manifest declares different checks.

## TypeScript and JavaScript

Use the declared package manager and existing scripts. Prefer non-writing gates such as `format:check`, `lint`, `typecheck`, `test`, and `build`. Do not replace a missing script with a guessed command. Detect UI frameworks and persistence libraries only as technical hints; confirm their business role through the adoption questions.

## Python

Read `pyproject.toml` before selecting commands. Use configured pytest, Ruff, and mypy entry points. Respect the repository's environment manager. Do not install a new formatter, test runner, or type checker merely to satisfy a profile.

## Go

Use module boundaries from `go.mod`. Standard gates are `go test ./...`, `go vet ./...`, and `go test -run=^$ ./...` as a compile-only check that does not leave a single-main binary in the repository. Add repository-specific integration, race, fuzz, or migration checks only when the codebase already defines them or the user approves their introduction.

## Rust

Use Cargo workspace boundaries. Standard gates are `cargo fmt --all -- --check`, Clippy with warnings denied, `cargo test --all`, and `cargo build --all`. Preserve feature and target conventions declared by the project.

## Ambiguity

If multiple commands appear to serve the same gate, ask which one is authoritative. A convenient command is not evidence that it matches CI or release behavior.
