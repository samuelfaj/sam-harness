# Gates

A gate advances only when its stated evidence exists. Fluent output is not evidence.

## Configured commands

- [ ] `go:.:build` in `.`: `go build ./...`
- [ ] `go:.:test` in `.`: `go test ./...`
- [ ] `go:.:typecheck` in `.`: `go vet ./...`

## Evidence ladder

- [ ] Source: the intended files contain the change.
- [ ] Local checks: required commands passed against the current tree.
- [ ] Commit: the commit SHA contains the reviewed change.
- [ ] Remote: the expected remote branch contains that SHA.
- [ ] Review: findings and approvals belong to the same SHA.
- [ ] CI: required jobs passed for the reviewed SHA.
- [ ] Artifact: the immutable digest came from that CI run.
- [ ] Deployment: the environment reports that exact digest.
- [ ] Live proof: technical and business signals stayed healthy for the observation window.

Never collapse two boxes into one claim.

## Production promotion

- [ ] Security and dependency checks passed.
- [ ] Required branch protection was read back from the remote provider.
- [ ] SBOM and provenance belong to the promoted digest.
- [ ] The same immutable artifact was promoted rather than rebuilt.
- [ ] Migration and rollback paths were exercised when data changes.
- [ ] Health gates and the observation window have owners.
