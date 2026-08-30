# Evidence and authority

## Evidence ladder

Keep these states separate:

1. Source contains the intended change.
2. Local checks passed against that source state.
3. A commit contains the checked source.
4. The expected remote branch contains that commit.
5. Pre-merge review findings, the complete hashed repair manifest, and approvals refer to the exact provider base/head SHAs, fingerprints, and canonical patch SHA-256.
6. Required CI jobs passed for that commit.
7. An immutable artifact was built from that CI state.
8. The target environment reports that artifact digest.
9. Live technical and business signals stayed healthy for the observation window.
10. Migration or rollback completed with its own reconciliation or recovery evidence, when applicable.

Never use one receipt to claim a later state.

## Authority

Capability is not permission. Read `.sam-harness/DELEGATION.md` before using the network, creating commits, pushing, publishing a release, deploying, modifying credentials, or performing an irreversible operation.

Ask immediately before a new authority boundary. Approval for repository files does not grant remote or production authority. A command recorded in canonical configuration proves that the argv was approved for the plan; it does not prove that the user authorized this execution now. Static/test repository mutation detection does not sandbox external effects from configured commands.

At runtime, review and repair require `network` authority. Staging, migration, and observation require network plus `deploy`; production and rollback require network plus both `deploy` and `release`. Generated rollback jobs are manual and independent of the forward pipeline.

`ci_secret_bindings` contains provider secret names, target environment-variable names, and exact phase scopes only. Review and repair bindings are separate. `agent_secret_environments` names the provider environment that contains agent credentials, while `agent_control_planes` names the required check plus the dedicated GitHub App credential identifiers or external GitLab project. Both are installed under `ci`; neither contains credential values or denotes the production release environment.

Ordinary GitHub pull-request/merge-group and GitLab MR jobs receive no bound agent secrets. GitHub's default-branch-owned agents workflow scopes model secrets to the matching command step and App credentials to jobs that declare the protected agent environment; its publisher receives no model credential. Pull requests use `pull_request_target`. A secret-bearing workflow must never listen directly to `merge_group`; an external App/webhook sends `repository_dispatch` type `sam_harness_merge_group_review` with exact provider head/base/ref data, which the resolver re-fetches before use. Restrict the agent environment to default/protected branches, require human review with prevent-self-review, keep App credentials only there, require its exact check, and read back the App, dispatcher, environment, branch/merge-queue rules, approvals, and check. The App publishes pending against the expected head and revalidates the current PR head or merge-group and default-branch refs before success.

GitLab's generated MR YAML omits bound review or repair jobs. Its external trusted project must publish the configured status against the exact current MR head using protected variables/environment and provider-side approvals. Read those controls back; Sam Harness does not supply a complete in-repository GitLab secret-bearing loop. Missing App/external status, protected credentials, released runtime, trusted base configuration, or unchanged current head blocks instead of silently skipping. Mixed bindings leave credential-free review or correction in ordinary CI when the required local evidence exists.

A secret-bearing review receipt is valid only when the job used the trusted released matching-version runtime, trusted base configuration, and exact `--review-base-sha`/`--review-head-sha` identities. It preserves both provider SHAs and fingerprints plus the canonical patch hash. Missing control-plane input is blocking evidence, not permission to fall back to the proposed change. For the Sam Harness self workflow, establish the release and base configuration before enabling agents.

Provider-bound agent secrets also require an external command boundary. The canonical configuration must be outside the target repository; `trusted_external_command: true` attests that the reviewer or correction executable resolves outside the target. `trusted_config_arguments` may identify only safe relative helper files by unique, actual zero-based argv positions greater than zero; runtime resolves them from the trusted configuration directory and rejects target-controlled, escaping, symlinked, or unlisted path-like inputs. This attestation does not prove the executable is safe. Local and waiver-only no-secret commands may remain repository-relative.

`filesystem_read_only: true` on every reviewer and `filesystem_sandboxed: true` on enabled correction are explicit attestations about the selected executables. They are required decisions, not proof produced by Sam Harness. Arbitrary argv and its OS sandbox remain in the trusted computing base; repository fingerprints, the standalone repair Git copy, and change budgets detect narrower violations but do not provide generic process containment.

## Failures and recovery

Stop on missing evidence, scope drift, stale plans, ambiguous destructive targets, or required gate failures. Preserve the real error and the relevant receipt. Prefer reversible operations. A code rollback is not a data rollback; check compatibility and recovery state first.

Receipts are immutable evidence about one execution, not reusable permission. Preserve the repository fingerprint, timestamps, command argv, findings, complete repair manifest and its hash, review base/head fingerprints and patch hash, artifact/SBOM/provenance hashes, source fingerprint, repair patch hash when applicable, and exact final status. A blocked receipt never proves a later phase. A manifest prescribes one bounded correction; only a fresh independent review proves that the resulting change is clean.

## Delegation

Give delegated work an exact path scope, expected output, allowed tools, checks, and stopping condition. Verify the returned artifacts yourself. A worker's summary is not proof.
