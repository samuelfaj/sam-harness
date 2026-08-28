# sam-harness

[Português](docs/README.pt-BR.md) | [Español](docs/README.es.md)

Sam Harness turns a repository's actual architecture, commands, delivery path, and risk into durable instructions for AI coding agents. It does not paste a large prompt into every conversation. A portable skill guides the adoption, a Go CLI produces deterministic plans and checks, and the installed repository files keep the rules in force after the chat ends.

The method comes from Samuel Fajreldines's book [Development Harness](https://samuelfaj.com/books/development-harness-couse/).

## Install the skill

```bash
npx skills add samuelfaj/sam-harness --skill sam-harness -g
```

Then open a repository and ask your agent:

```text
Apply sam-harness here.
```

Explicit invocation is more reliable when the agent supports it:

```text
Use $sam-harness and apply the harness to this repository.
```

```text
/sam-harness apply
```

The skill asks before downloading the CLI. Its bootstrap scripts require Cosign and verify the signed release bundle and checksum before installing the binary in the user cache.

## What happens

1. `scan` reads manifests, commands, workspaces, CI files, Git state, UI hints, persistence hints, and deployment files. It does not edit the repository.
2. The agent asks about business facts that source code cannot prove, including criticality, data sensitivity, production use, authority, design ownership, rollback, approvals, ambiguous commands, and an undetected CI provider.
3. `plan` recommends `baseline`, `production`, or `regulated`, then records the exact file operations under a cryptographic plan ID that expires after 30 minutes.
4. The user reviews and approves that ID.
5. `apply` rejects stale repository state and writes only the approved operations.
6. `doctor` validates the installed structure. `check` runs the configured commands and writes a local evidence receipt.

Sam Harness preserves existing `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, Copilot instructions, `.gitignore`, and GitLab CI content through bounded managed blocks. It never grants commit, push, release, or deploy authority on its own.

## CLI

```text
sam-harness scan [path] [--format human|json]
sam-harness plan [path] --profile auto|baseline|production|regulated [--answers file]
sam-harness apply --plan <file> --accept <plan-id>
sam-harness check [path] [--format human|json]
sam-harness doctor [path]
sam-harness upgrade [path] --to <version>
```

Plans go to the operating system's temporary directory unless `--output` names a new file outside the repository. Existing files and repository paths are refused. `scan` and `plan` do not write tracked repository files.

## Profiles

`baseline` installs repository instructions, authority boundaries, deterministic local gates, evidence rules, and user-facing quality controls.

`production` also installs CI integration, release and rollback runbooks, immutable artifact requirements, SBOM and provenance controls, migration reconciliation, and production observation.

Those controls are requirements, not proof by themselves. Promotion still requires receipts for the actual CI run, artifact digest, SBOM, provenance, approvals, and live observation.

`regulated` adds threat modeling, data governance, separated approvals, audit evidence, recovery exercises, and retirement controls. It does not claim regulatory certification.

## Supported repositories

The first release detects TypeScript and JavaScript, Python, Go, and Rust, including mixed monorepos. It integrates with GitHub Actions and GitLab CI only when the user approves CI changes.

## Development

The project uses Go 1.27.

```bash
go test ./...
go test -race ./...
go vet ./...
go build ./cmd/sam-harness
python3 scripts/validate-skill.py skills/sam-harness
```

The [book traceability matrix](docs/book-traceability.md) maps all 20 chapters to executable controls, questions, templates, or tests. The CI test rejects a missing chapter.

## Security

Read [SECURITY.md](SECURITY.md) before reporting a vulnerability. Release archives include checksums, a keyless Cosign bundle for the checksum file, CycloneDX SBOMs, and GitHub build provenance.

## License

MIT. Copyright 2026 Samuel Fajreldines.
