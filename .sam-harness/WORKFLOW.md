# Executable workflow

`.sam-harness/config.yaml` is the canonical source for every executable command. Commands remain argv arrays; this document is a human-readable inventory and does not grant authority.

## Delivery graph

Happy path: check → test → build → deploy → verify → release → monitor.

Exception path: failure → repair / rollback → verify. After repair or rollback, re-enter verify; do not skip it.

Generated GitLab stages use those names. GitHub job names stay `static`, `test`, `e2e`, `artifact`, `staging`, `production`, and `observe`; map them onto the same graph. Do not collapse the graph into one stage.

## Unify redundant CI

Prefer generated `sam-harness-*` jobs as the canonical gates. After apply or upgrade, delete or disable host jobs that only repeat a Harness phase (the same lint, typecheck, unit, contract, build, or browser command). Keep unique host work such as the live deploy path, infrastructure, seed, and a host review that Harness does not own.

If a host job is the only coverage for a command (`ci.external_coverage`), move that command into a Harness gate before deleting the host job, then retarget or drop the coverage entry so it names a job that still exists. Align parent `stages:` with the graph above, including `repair`. Independent jobs use `needs: []`.

## Local phases

- static `go:.:build` in `.`: `'go' 'build' './...'`
- static `go:.:typecheck` in `.`: `'go' 'vet' './...'`
- test `go:.:test` in `.`: `'go' 'test' './...'`
- e2e `complete customer installation lifecycle` in `.`: `'sh' 'testdata/e2e-full-flow.sh'`

## Static guard command-or-waiver inventory

- format command `gofmt is clean` in `.` (120s): `'sh' '-c' 'test -z "$(gofmt -l .)"'`
- lint command `go vet` in `.` (600s): `'go' 'vet' './...'`
- typecheck command `compile all Go packages` in `.` (600s): `'go' 'test' '-run' '^$' './...'`
- architecture command `package import boundaries` in `.` (600s): `'go' 'test' './internal/architecture'`
- security command `reachable vulnerability scan` in `.` (900s): `'go' 'run' 'golang.org/x/vuln/cmd/govulncheck@v1.7.0' './...'`
- dependencies command `verify module dependencies` in `.` (120s): `'go' 'mod' 'verify'`
- schema command `configuration schema and traceability` in `.` (600s): `'go' 'test' './internal/config' './internal/traceability'`
- project_policies command `portable skill validation` in `.` (120s): `'python3' 'scripts/validate-skill.py' 'skills/sam-harness'`

## Test guard command-or-waiver inventory

- unit command `complete Go test suite` in `.` (900s): `'go' 'test' './...'`
- integration command `apply, configuration and renderer integration` in `.` (900s): `'go' 'test' './internal/apply' './internal/config' './internal/render'`
- contract command `CLI and planning contracts` in `.` (900s): `'go' 'test' './internal/cli' './internal/planner' './internal/config'`
- business_invariants command `authority, evidence and book traceability invariants` in `.` (900s): `'go' 'test' './internal/pipeline' './internal/apply' './internal/traceability'`
- property waiver: No stable generative-property harness is configured; boundary behavior is covered by table-driven unit tests.
- mutation waiver: No Go mutation-testing tool is currently part of the supported toolchain; security and failure-path tests are explicit release gates.
- performance waiver: The CLI has no published latency or throughput SLO yet; a performance gate will be added when a measurable budget is adopted.

## Artifact and promotion

- Artifact build `build the immutable CLI binary` in `.` (900s): `'go' 'build' '-trimpath' '-o' 'dist/sam-harness' './cmd/sam-harness'`
- Artifact path: `dist/sam-harness`
- SBOM `record the binary module inventory` in `.` (120s): `'sh' '-c' 'go version -m dist/sam-harness > dist/sam-harness.sbom.txt'`
- SBOM path: `dist/sam-harness.sbom.txt`
- Provenance `record source and toolchain provenance` in `.` (120s): `'sh' '-c' '{ git rev-parse HEAD; go version; } > dist/sam-harness.provenance.txt'`
- Provenance path: `dist/sam-harness.provenance.txt`
- Staging `verify the release candidate in staging` in `.` (300s): `'sh' '-c' 'test -x "$SAM_HARNESS_ARTIFACT_PATH" && test "$(shasum -a 256 "$SAM_HARNESS_ARTIFACT_PATH" | awk '"'"'{print $1}'"'"')" = "$SAM_HARNESS_ARTIFACT_SHA256" && "$SAM_HARNESS_ARTIFACT_PATH" doctor .'`
- Production `promote the approved binary to the GitHub release` in `.` (600s): `'sh' '-c' 'test -n "${SAM_HARNESS_RELEASE_TAG:-}" && gh release upload "$SAM_HARNESS_RELEASE_TAG" "$SAM_HARNESS_ARTIFACT_PATH" --clobber'`
- Rollback `withdraw a failed GitHub release` in `.` (300s): `'sh' '-c' 'test -n "${SAM_HARNESS_RELEASE_TAG:-}" && gh release edit "$SAM_HARNESS_RELEASE_TAG" --draft'`
- Migration `prove backward-compatible configuration migration` in `.` (600s): `'go' 'test' './internal/config' '-run' 'Backward|Workflow'`

The artifact phase builds once and records the configured artifact digest. CI packages the artifact, SBOM, and provenance paths into one tar archive so relative paths and executable modes survive transport; receipts remain a separate artifact. Staging and production extract and promote that same identity and SHA-256 digest; rebuilding during promotion is forbidden.

Deploy authority is not granted. Generated staging, migration, production, observation, and rollback jobs remain present but structurally inactive until a newly approved configuration grants deploy authority.

The production release boundary is not fully authorized. GitHub production and rollback receive no contents-write permission or GH_TOKEN, and those jobs remain structurally inactive.

Release schedule: `0 15 * * 3` in IANA timezone `UTC`. GitHub cron is evaluated in UTC; translate and verify the configured local schedule before enabling it. GitLab schedules are provider-side and require remote configuration plus readback.

## Provider-side controls

The six-role review is a pre-merge required-status gate. GitHub branch protection, the configured GitHub App check, merge queue rules, GitLab external status checks, protected branches, merge-request approvals, and protected production environments are provider-side controls. Generated files declare local boundaries only; read every remote rule and required check back before claiming it is active.

Trusted review uses the pinned released harness, trusted base configuration, explicit `--review-base`, `--review-base-sha`, and `--review-head-sha`; its receipt binds the exact provider SHAs, fingerprints, patch, and SHA-256. Each reviewer prompt carries only `review_patch_path` and `review_patch_sha256`; the exact canonical patch is materialized as a regular file inside the sandbox's excluded evidence area and must be read as untrusted diff data. Every initial finding must identify a current added or modified base-to-head line, or line 0 only for deletion-only, deleted, or pure-rename file-level evidence. A repair-branch re-review may pass `--prior-review-receipt` to close the frozen manifest; only same-role frozen IDs remain eligible, and new P0/P1 findings follow the same prior-head-to-current-head scope rule. Unrelated pre-existing findings do not reopen repair. Missing proof fails closed. Missing trusted control-plane inputs or credentials fails closed. JSON receipts are emitted with escaped standalone HTML companions beside them.

GitHub keeps `.github/workflows/sam-harness.yml` credential-free for pull requests and direct `merge_group` events. The base-owned `.github/workflows/sam-harness-agents.yml` uses `pull_request_target` for pull requests, `repository_dispatch` type `sam_harness_merge_group_review` for merge-queue review, and failed-run `workflow_run` for bound repair. It never listens directly to `merge_group`, because that workflow definition would come from the synthetic queue ref. A dedicated external App/webhook control plane must observe each provider `merge_group` checks request and send the exact current `head_sha`, current default-branch `base_sha`, and `merge_group_ref` in `client_payload`; the generated resolver re-fetches both provider refs before any review and again before check conclusion. Absence or drift leaves required check `sam-harness/trusted-review` missing or failed and blocks the queue. The workflow checks out exact base/head SHAs as data, never runs target setup, caches, hooks, local actions, or repository commands, and scopes model secrets to the review or repair step. Configure `sam-harness-agents` as a protected agent environment restricted to default/protected branches with required reviewers and prevent-self-review, then read those settings back. Store both GitHub App credentials `SAM_HARNESS_GITHUB_APP_ID` and `SAM_HARNESS_GITHUB_APP_PRIVATE_KEY` in that protected environment, never as repository-level secrets; every job that reads either credential declares the environment. The in-workflow App tokens request only the permissions needed by each job. The external dispatcher needs repository-dispatch authority, and the trusted repair publisher needs contents:write and pull_requests:write only when enabled. Require the App check on the exact pull-request and merge-group head. Cancellation leaves it pending, and automatic repair is never published for a merge-group run. Initial adoption without the released harness/base config, external merge-queue dispatcher, or protected credentials fails closed.

Merge-queue dispatcher payload:

```json
{
  "event_type": "sam_harness_merge_group_review",
  "client_payload": {
    "head_sha": "<provider merge-group SHA>",
    "base_sha": "<current default-branch SHA>",
    "merge_group_ref": "refs/heads/gh-readonly-queue/<provider ref>"
  }
}
```
