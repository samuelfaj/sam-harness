# sam-harness

[Português](docs/README.pt-BR.md) | [Español](docs/README.es.md)

Sam Harness turns a repository's actual architecture, commands, delivery path, and risk into durable instructions and an executable development lifecycle for AI coding agents. It does not paste a large prompt into every conversation. A portable skill guides adoption and operation, a Go CLI produces deterministic plans and receipts, and installed repository files keep the rules in force after the chat ends.

The method comes from Samuel Fajreldines's book [Development Harness](https://www.samuelfaj.com/books/development-harness-course/development-harness-course.en-US.html).

[![Development Harness workflow: from repository rules to safe production](assets/sam-harness.png)](https://www.samuelfaj.com/books/development-harness-course/development-harness-course.en-US.html)

Read the book in your language: [🇺🇸 English](https://www.samuelfaj.com/books/development-harness-course/development-harness-course.en-US.html) · [🇧🇷 Português](https://www.samuelfaj.com/books/development-harness-course/development-harness-course.html) · [🇪🇸 Español](https://www.samuelfaj.com/books/development-harness-course/development-harness-course.es.html)

## Install

🇺🇸 English

```text
Help me completely implement https://github.com/samuelfaj/sam-harness in this repository.
Ask me what cannot be inferred, adapt it to this project, implement missing controls in approved stages, and finish with evidence.
```

🇧🇷 Português

```text
Me ajude a implementar aqui o https://github.com/samuelfaj/sam-harness
Pergunte o que o repositório não prova, adapte ao projeto, implemente os controles que faltam em etapas aprovadas e termine com evidência.
```

🇪🇸 Español

```text
Ayúdame a implementar aquí https://github.com/samuelfaj/sam-harness
Pregunta lo que el repositorio no demuestra, adáptalo al proyecto, implementa los controles que falten en etapas aprobadas y termina con evidencia.
```

## What happens

1. `scan` reads manifests, commands, workspaces, CI files, Git state, UI hints, persistence hints, and deployment files. It does not edit the repository.
2. The agent asks about business facts that source code cannot prove, including criticality, data sensitivity, production use, authority, design ownership, rollback, approvals, ambiguous commands, and an undetected CI provider.
3. `plan` recommends `baseline`, `production`, or `regulated`, then records the exact file operations under a cryptographic plan ID that expires after 30 minutes.
4. The user reviews and approves that ID.
5. `apply` rejects stale repository state and writes only the approved operations.
6. `doctor` validates the installed structure. `check` runs the configured local gates and writes an evidence receipt.
7. `pipeline` runs an approved phase—static checks, tests, pre-merge six-role review, artifact, staging, production, observation, rollback, or migration—and writes a phase-specific receipt.
8. If `static`, `test`, `review`, or `artifact` fails and correction was explicitly enabled, `repair` validates the current receipt, runs the configured command in an isolated Git sandbox, enforces cumulative attempt/file/line budgets, reruns static checks and tests, and emits a correction-only patch with its SHA-256.

Sam Harness preserves existing `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, Copilot instructions, `.gitignore`, and GitLab CI content through bounded managed blocks. It adds workflow, reviewer, change-budget, observation, and retirement guidance without replacing user-owned content.

## CLI

```text
sam-harness scan [path] [--format human|json]
sam-harness plan [path] --profile auto|baseline|production|regulated [--answers file]
sam-harness apply --plan <file> --accept <plan-id>
sam-harness onboard [path] [--answers file] [--answers-output file] [--locale en-US|pt-BR|es] [--accept plan-id] [--output file] [--format human|json] [--interactive true|false]
sam-harness adopt --auto [path] [--answers file] [--answers-output file] [--locale en-US|pt-BR|es] [--accept plan-id] [--output file] [--format human|json]
sam-harness adopt --guided [path] [--answers file] [--answers-output file] [--locale en-US|pt-BR|es] [--accept plan-id] [--implement control] [--waiver-control id --waiver-risk text --waiver-reason text] [--output file] [--format human|json]
sam-harness bootstrap github [path] [--accept plan-id] [--format human|json]
sam-harness bootstrap gitlab [path] [--accept plan-id] [--format human|json]
sam-harness stage classifier|context|planning|implementation|review|repair --input file [--format human|json]
sam-harness freeze check [path] [--policy file] [--now rfc3339] [--exception file] [--head sha] [--base sha] [--branch name] [--kind feature] [--scheduled-release true|false]
sam-harness check [path] [--format human|json] [--receipt true|false]
sam-harness doctor [path]
sam-harness upgrade [path] --to <version> [--answers <file>] [--output <file>]
sam-harness pipeline [path] [--config <absolute-or-contained-file>] [--review-base <absolute-directory> --review-base-sha <hex> --review-head-sha <hex>] --phase <static|test|review|artifact|staging|production|observe|rollback|migration|all> [--receipt true|false]
sam-harness repair [path] [--config <absolute-or-contained-file>] --receipt <file> [--receipt-output true|false]
```

Plans go to the operating system's temporary directory unless `--output` names a new file outside the repository. Existing files and repository paths are refused. `scan` and `plan` do not write tracked repository files.

## Complete lifecycle example

First inspect and prepare a plan. The answers file stays outside the repository and records the real commands and owners that source inspection cannot prove:

For a production or regulated plan, use the [workflow configuration shape](skills/sam-harness/references/workflow-configuration.md) and replace every example value with an approved repository or provider command.

```bash
sam-harness scan /path/to/repository --format json
sam-harness plan /path/to/repository --profile auto --answers /tmp/sam-harness-answers.json
```

Review the profile rationale, unresolved decisions, exact file operations, expiry, and plan ID. Only after approving that exact ID:

```bash
sam-harness apply --plan /tmp/sam-harness-plan.json --accept <plan-id>
sam-harness doctor /path/to/repository
sam-harness check /path/to/repository --receipt true
```

For a production lifecycle, run each boundary separately so its status remains visible. A pre-merge review also needs a separate trusted checkout of the proposed base plus the exact base/head SHAs. Before staging, migration, production, or rollback, confirm authority for that external effect:

```bash
sam-harness pipeline /path/to/repository --phase static --receipt true
sam-harness pipeline /path/to/repository --phase test --receipt true
sam-harness pipeline /path/to/repository --review-base /absolute/path/to/trusted-base --review-base-sha <base-sha> --review-head-sha <head-sha> --phase review --receipt true
sam-harness pipeline /path/to/repository --phase artifact --receipt true
sam-harness pipeline /path/to/repository --phase staging --receipt true
sam-harness pipeline /path/to/repository --phase migration --receipt true
sam-harness pipeline /path/to/repository --phase production --receipt true
sam-harness pipeline /path/to/repository --phase observe --receipt true
```

`--review-base`, `--review-base-sha`, and `--review-head-sha` are valid only for `review` or `all`; the SHA flags must appear together and require the base directory. Secret-bearing review requires all three. The 40- or 64-character hexadecimal SHAs must equal the checked-out base and target `HEAD` before and after review. The receipt records `review_base_root`, `review_base_sha`, `review_base_fingerprint`, `review_head_sha`, `review_head_fingerprint`, the canonical `review_patch`, and `review_patch_sha256`; any identity or content drift blocks. A head-only local review without `--review-base` may still be useful, but it does not satisfy the required pre-merge delta gate.

`--phase all` is convenient in an already approved automated workflow, but it does not waive protected-production or manual approvals; supply the base directory and SHA pair when `all` must satisfy provider-bound pre-merge review. Rollback is never part of `all` and is never started automatically after a failure; run its independent manual entry point only when the matching runbook and authority apply:

```bash
sam-harness pipeline /path/to/repository --phase rollback --receipt true
```

If a receipt is failed and bounded correction is enabled:

```bash
sam-harness repair /path/to/repository --receipt .sam-harness/evidence/<failed-receipt>.json --receipt-output true
```

Repair accepts only a current failed or blocked v0.2 `static`, `test`, `review`, or `artifact` receipt from this repository. A receipt transported between clean CI workspaces is rebound only after its repository identity, configuration digest, and before/final fingerprints match the target. Repair passes a structured prompt containing the untrusted receipt—not the raw receipt as instructions—to a temporary autonomous Git sandbox with no inherited Git remotes, repository hooks, or Git credentials. Its clean process environment exposes only configured `repair`-scope secrets. Before running verification or applying anything, Sam Harness blocks if a secret exposed to the correction appears in new regular-file bytes, a changed symlink target, or an added patch line; an unchanged baseline occurrence is not treated as a new leak. It also blocks escaping symlinks, Git-control changes, stale target state, and budget overruns. A successful delta must pass fresh `static` and `test` checks before being applied to the unchanged target. The receipt records the correction-only patch and `repair_patch_sha256`.

If change-request publishing is enabled and separately authorized, CI sends that patch/receipt pair as untrusted data to a credential-separated publisher. The publisher requires exactly one patch and one receipt, verifies their filename and SHA-256 relationship, disables hooks, applies only that patch to the configured repair branch, and opens a PR or MR. It never pushes directly to a protected branch.

To upgrade a legacy production installation, provide the required v0.2 workflow decisions in an answers file:

```bash
sam-harness upgrade /path/to/repository --to 0.3.1 --answers /tmp/sam-harness-v0.3-answers.json
```

`upgrade` merges explicit answers over the installed configuration and produces an expiring plan; it does not apply it. Review unresolved decisions and every operation, then approve and apply the exact new plan ID. Use the [workflow configuration shape](skills/sam-harness/references/workflow-configuration.md) for the static/test guard coverage, provider secret-name, protected-agent-environment and agent-control-plane decisions, filesystem and trusted-command attestations, and lifecycle commands that legacy v0.1 production configuration does not contain.

## Trust boundaries

- `scan` and `plan` do not edit tracked source files. `pipeline` orchestrates configured commands and receipts rather than editing source itself, but those commands may change the repository or external systems. `apply` writes only an unexpired, explicitly approved plan whose repository fingerprint still matches.
- A configured command is executable policy, not standing permission. The user must authorize the current remote, deployment, rollback, migration, credential, or irreversible action.
- Sam Harness fingerprints the repository before and after `static` and `test` and blocks the phase if those commands mutate it. Blocking does not restore the tree. This detects repository mutation; it does not sandbox external effects. Each configured external command remains the user's explicit execution boundary.
- At runtime, `review` and `repair` require network authority. Staging, migration, and observation require network plus deploy authority; production and rollback require network plus both deploy and release authority.
- Review is a pre-merge gate over the exact trusted-base and proposed-head SHAs and fingerprints plus canonical patch hash. `ci_secret_bindings` stores only a scope, environment-variable name, and GitHub/GitLab secret name; `review` and `repair` use separate scopes. The answers field `agent_secret_environments`, installed as `ci.agent_secret_environments`, names the protected provider environment in which those agent secrets must live; it is distinct from the production release environment. The answers field `agent_control_planes`, installed as `ci.agent_control_planes`, defines the required provider status check and either the dedicated GitHub App credential names or the external GitLab project. Sam Harness never serializes secret values into versioned files.
- Ordinary GitHub `pull_request`/`merge_group` jobs and GitLab merge-request jobs receive no bound agent secrets. For a bound GitHub scope, the default-branch-owned `.github/workflows/sam-harness-agents.yml` uses `pull_request_target`, a `repository_dispatch` named `sam_harness_merge_group_review`, or a failed-run `workflow_run`. It never listens directly to `merge_group`: an external App/webhook must forward the exact provider merge-queue head SHA, current default-branch base SHA, and `gh-readonly-queue` ref as data. The resolver re-fetches those provider refs before review and before check conclusion; missing dispatch or drift blocks the required check. It runs the released v0.2 CLI with trusted base configuration, scopes model secrets to the matching review or repair step, and never runs target setup, hooks, caches, local actions, or repository commands. Automatic repair is never published for a merge-group run. A credential-free correction scope stays in the ordinary workflow; mixed review/repair bindings move only the bound scope and retain the applicable local repairs.
- GitLab does not generate a complete in-repository secret-bearing agent loop. For any bound review or repair scope, the configured external project must run the trusted control plane and publish the configured required status for the exact current MR head; the corresponding secret-bound MR job is omitted. Missing external status blocks merge, while credential-free review or correction scopes remain in the MR pipeline where their evidence is available.
- Creating provider credentials and configuring the control plane remain external administration tasks. Restrict the GitHub agent environment to default/protected branches, require human reviewers, enable prevent-self-review, keep the dedicated App ID and private key only in that environment, require its exact status check, and remotely read back all settings. Configure and read back the GitLab external project, protected variables/environment, status check, protected branch, and approval rules. A fork, unavailable secret, missing check, missing trusted base configuration, missing released runtime, or changed head fails closed; never silently skip or downgrade the gate. The generated Sam Harness self workflow therefore needs its v0.2 release and base configuration established through an approved trusted bootstrap path before agents receive secrets. Use a provider `ci_secret_waivers` reason only when approved commands genuinely need no credentials.
- Every reviewer requires a user-verified `filesystem_read_only: true` attestation, and enabled correction requires `filesystem_sandboxed: true`. Provider-bound `review` secrets additionally require all six reviewers to set `trusted_external_command: true`; a bound `repair` secret requires correction to do the same. The executable must resolve outside the target repository. `trusted_config_arguments` lists unique, actual zero-based argv positions greater than zero for safe helper files resolved from the trusted configuration directory; unlisted target-like inputs block. Local and waiver-only no-secret commands may remain repository-relative.
- For example, Codex argv may use `codex exec --sandbox read-only -` for review and `codex exec --sandbox workspace-write -` for correction. Verify the installed tool, external executable, indexed helper files, and exact JSON output contract before attesting: arbitrary argv and its sandbox remain part of the trusted computing base, and Sam Harness is not a general OS sandbox. An exact-version `npx` package is allowed under the stricter shape documented in the workflow reference; unpinned package dispatch is not.
- The six reviewers are `architecture`, `security`, `correctness`, `test_quality`, `business_rules`, and `simplicity`. Their commands must be independently configured and attested as filesystem-read-only. The pre-merge prompt and receipt bind the base fingerprint, head fingerprint, canonical patch, and patch SHA-256. Malformed reviewer JSON, a P0/P1 finding, repository mutation, or changed base/head blocks review; P2/P3 findings stay recorded.
- The artifact phase builds once and records artifact, SBOM, and provenance paths and SHA-256 values plus the source fingerprint. Staging and production recheck all of them and promote the same artifact without rebuilding.
- A receipt proves only its own phase and repository fingerprint. A source edit is not a passing check; a passing check is not CI; a deployment is not a healthy observation window.

## Executable coverage and local agent context

Production and regulated planning requires every category in `static_guards` and `test_guards` to have exactly one entry in `commands` or one non-empty approved entry in `waivers`:

- Static: `format`, `lint`, `typecheck`, `architecture`, `security`, `dependencies`, `schema`, `project_policies`.
- Test: `unit`, `integration`, `contract`, `business_invariants`, `property`, `mutation`, `e2e`, `performance`.

Static and test phases run both discovered repository gates and configured guard commands. A waiver is auditable skipped evidence, not a passing check. Missing categories block planning instead of receiving guessed commands.

Application also installs `.agents/skills/sam-harness-<lifecycle>/SKILL.md` for `classify`, `context`, `plan`, `implement`, `review`, `repair`, and `release`; managed `<workspace>/AGENTS.md` contracts for detected non-root workspaces; and `.github/pull_request_template.md` plus `.gitlab/merge_request_templates/sam-harness.md` with the evidence ladder and human-facing UX checklist. Load only the local skill for the current state and follow the closest `AGENTS.md`. Templates organize claims; they do not prove them.

## Profiles

`baseline` installs repository instructions, authority boundaries, deterministic local gates, evidence rules, and user-facing quality controls.

`production` also installs CI integration, scoped secret-name bindings or explicit waivers, protected agent-environment and provider-control-plane declarations, trusted external command boundaries for provider-secret review and repair, six fixed review roles, sandboxed bounded correction and a separated patch publisher, immutable artifact/SBOM/provenance commands, staging and protected production promotion, manual rollback, health and observation checks, migration commands, canary percentages, and a release schedule.

Those controls are requirements, not proof by themselves. Promotion still requires receipts for the actual CI run, artifact digest, SBOM, provenance, approvals, and live observation.

`regulated` adds threat modeling, data governance, separated approvals, audit evidence, recovery exercises, and retirement controls. It does not claim regulatory certification.

Production and regulated plans remain inapplicable until every required executable workflow decision is explicit. Baseline may omit remote delivery controls.

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
