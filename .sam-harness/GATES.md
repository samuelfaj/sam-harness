# Gates

A gate advances only when its stated evidence exists. Fluent output is not evidence.

## Configured commands

- [ ] `go:.:build` in `.`: `go build ./...`
- [ ] `go:.:test` in `.`: `go test ./...`
- [ ] `go:.:typecheck` in `.`: `go vet ./...`

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
- e2e command `complete customer installation lifecycle` in `.` (1200s): `'sh' 'testdata/e2e-full-flow.sh'`
- performance waiver: The CLI has no published latency or throughput SLO yet; a performance gate will be added when a measurable budget is adopted.

## Evidence ladder

- [ ] Source: the intended files contain the change.
- [ ] Local checks: required commands passed against the current tree.
- [ ] Commit: the commit SHA contains the reviewed change.
- [ ] Remote: the expected remote branch contains that SHA.
- [ ] Review: findings and approvals belong to the same SHA.
- [ ] Review findings identify a current added or modified diff line, or line 0 only for deletion-only, deleted, or pure-rename file-level evidence; convergence closure references the frozen manifest and same-role IDs, and new P0/P1 regressions follow the same prior-head-to-current-head scope rule.
- [ ] CI: required jobs passed for the reviewed SHA.
- [ ] Artifact: the immutable digest came from that CI run.
- [ ] Deployment: the environment reports that exact digest.
- [ ] Live proof: technical and business signals stayed healthy for the observation window.

Never collapse two boxes into one claim.

## Production promotion

- [ ] Security and dependency checks passed.
- [ ] Required branch protection was read back from the remote provider.
- [ ] SBOM and provenance belong to the promoted digest.
- [ ] The same immutable artifact was promoted rather than rebuilt, with configured relative paths and executable modes preserved.
- [ ] Deployment jobs were active only because deploy authority was explicitly granted.
- [ ] Rollback remained an explicit manual action and did not run production first.
- [ ] Migration and rollback paths were exercised when data changes.
- [ ] Health gates and the observation window have owners.

## Provider-side controls

- [ ] Six-role review is required on the exact pull or merge request head before merge.
- [ ] The GitHub App required check, external merge-group dispatcher, and merge queue rules, when used, were read back from GitHub.
- [ ] The secret-bearing GitHub workflow has no direct `merge_group` trigger; merge-queue review arrives only through base-owned `repository_dispatch`.
- [ ] GitHub's agent environment is restricted to default/protected branches, requires approval, prevents self-review, and was read back.
- [ ] GitLab's external trusted-control project/status check, protected variables, protected branch, and merge-request approval rules were read back.
- [ ] The provider reports the production environment as protected and its approval rule is active.

Local YAML cannot prove remote settings. GitHub's ordinary pull-request workflow and GitLab's merge-request YAML receive no bound agent secrets. Missing protected credentials, merge-queue dispatch, or required external/App status fails closed; never treat an omitted secret-bearing MR job as approval.
