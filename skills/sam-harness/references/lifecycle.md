# Executable lifecycle

Use this reference after an approved plan has installed a valid workflow. Read `.sam-harness/WORKFLOW.md`, `.sam-harness/REVIEWERS.md`, `.sam-harness/CHANGE_BUDGET.md`, and the runbook for the requested phase before execution.

## Select the phase

Run only the smallest phase that answers the user's request:

| Phase | What it proves |
|---|---|
| `static` | Discovered repository gates and configured static guard commands completed; approved category waivers remain skipped evidence. |
| `test` | Discovered test gates and configured test guard commands completed; approved category waivers remain skipped evidence. |
| `review` | The exact base-to-head change passed the pre-merge six-role gate: the receipt bound both fingerprints plus the canonical patch SHA-256, reviewer commands had verified read-only-filesystem attestations, returned valid JSON, and produced no P0/P1 finding or repository mutation. |
| `artifact` | One artifact was built; artifact, SBOM, and provenance SHA-256 values plus the source fingerprint were recorded. |
| `staging` | The configured staging target received that same artifact path and digest and passed its health checks. |
| `production` | The configured production command promoted that same artifact path and digest. CI supplies the separate protected/manual approval evidence. |
| `observe` | Configured technical and business observation commands passed for the interval they measured. |
| `rollback` | The separately triggered manual rollback command ran; recovery and data compatibility still need their configured checks. |
| `migration` | Configured migration and reconciliation commands passed. |
| `all` | Each configured forward phase passed in order. Rollback is never part of `all`; receipts and approval boundaries remain separate. |

Use:

```text
sam-harness pipeline <root> [--config <absolute-or-contained-file>] [--review-base <absolute-directory> --review-base-sha <hex> --review-head-sha <hex>] --phase <phase> --receipt true
```

`pipeline` executes stored argv arrays in their contained workdirs and does not intentionally edit source files. Sam Harness fingerprints the repository before and after `static` and `test`; any mutation by discovered gates or guard commands blocks that phase. Detection does not revert the mutation, so inspect and recover the tree safely before retrying. It also does not sandbox network or provider effects.

Staging, production, rollback, migration, and any other configured command may still cause its declared external effect. The user-approved argv and current authority are the execution boundary. Confirm authority immediately before running the phase, and do not use `all` to bypass a protected production or manual approval.

At runtime, review and repair require `network` authority. Staging, migration, and observation require network plus `deploy`. Production and rollback require network plus both `deploy` and `release`. Generated rollback jobs are independent manual entry points: GitHub uses `workflow_dispatch`, and GitLab uses a manual job. A failed production phase never starts rollback automatically.

For production and regulated workflows, every static and test guard category has exactly one configured command or one non-empty approved waiver. A waiver records why no executable control exists and appears as skipped receipt evidence; it is never a passing command. Do not describe waived coverage as tested.

## Review contract

The review phase is a pre-merge gate over the exact proposed base-to-head change. Give `--review-base` a separate absolute base-checkout directory. `--review-base-sha` and `--review-head-sha` are a paired 40- or 64-character hexadecimal identity; secret-bearing review requires all three flags. They are valid only for `review` or `all`. Runtime requires each SHA to equal its checkout's Git `HEAD`, snapshots both sides, creates a canonical patch, and records `review_base_root`, `review_base_sha`, `review_base_fingerprint`, `review_head_sha`, `review_head_fingerprint`, `review_patch`, and `review_patch_sha256`. It rechecks the Git identities and contents after all reviewers; a changed input blocks. A local head-only review without `--review-base` does not satisfy the pre-merge delta gate.

Review runs exactly the six configured roles: `architecture`, `security`, `correctness`, `test_quality`, `business_rules`, and `simplicity`. Each command receives `SAM_HARNESS_REVIEW_ROLE` and a structured prompt containing the base/head identities and untrusted patch on standard input. Its standard output must be exact JSON with `review_complete: true` and a `findings` array. Each actionable finding must state the exact `required_change` and an observable `acceptance` condition. Reviewers must report every finding in their role now rather than stopping after the highest-severity issue. A review performed only after merge does not satisfy this gate.

Each reviewer must carry the approved `filesystem_read_only: true` attestation. A Codex argv using `codex exec --sandbox read-only -` is one example, not a portable guarantee; verify the installed tool and environment and ensure any wrapper emits only the final findings JSON before attesting. P0/P1 findings, incomplete or malformed output, changed review inputs, and any repository fingerprint mutation block the phase. Sam Harness consolidates all findings, including P2/P3, into one deterministic repair manifest bound to the repository, review base/head lineage, patch digest, and a manifest SHA-256. P2/P3 findings remain recorded and must not be reported as a clean review.

Provider-bound review secrets add a separate command boundary. All six reviewers require `trusted_external_command: true`; their executable must resolve outside the target checkout, and `--config` must point outside it. `trusted_config_arguments` lists only unique, actual zero-based argv positions greater than zero for safe helper files that runtime resolves from the trusted configuration directory. Target-relative executables, unlisted path-like arguments, and unlisted interpreter inputs block. Local or waiver-only no-secret review may still use repository-relative commands.

Provider-bound review and repair secrets must exist only in the protected agent environment named by `agent_secret_environments`; this is not the production release environment. Their `agent_control_planes` entry defines the provider check and trusted control-plane identity. Generated files declare these boundaries but cannot prove remote settings.

Ordinary GitHub pull-request/merge-group jobs receive no bound agent secrets. Default-branch-owned `.github/workflows/sam-harness-agents.yml` uses `pull_request_target` for bound pull-request review, `repository_dispatch` type `sam_harness_merge_group_review` for merge-queue review, or a failed ordinary run's `workflow_run` for bound repair. It never listens directly to `merge_group`. A dedicated external App/webhook sends the exact provider merge-group head SHA, current default-branch base SHA, and `gh-readonly-queue` ref as data; the resolver re-fetches both refs. The workflow treats exact base/head checkouts as data, uses the released v0.2 harness and trusted base configuration, omits target setup/hooks/caches/local actions/repository commands, and scopes model secrets to the matching step. The App creates the configured check against the expected head, then re-fetches the current PR head or merge-group and base refs before success. Missing dispatch, drift, or cancellation leaves the gate failed or pending; a merge-group run never publishes automatic repair. Credential-free review or correction remains in the ordinary workflow when only the other scope is bound.

Before enabling GitHub agents, restrict the named environment to default/protected branches, require human reviewers, enable prevent-self-review, keep the App ID/private key only there, require the exact App check on PR and merge-queue heads, and remotely read back every rule. For GitLab, the MR pipeline receives no bound review or repair secret and omits that job. Its configured external project must execute the trusted control plane and publish the required status for the exact current MR head. Configure and read back its protected variables/environment, status, protected branch, and approvals; Sam Harness does not generate a full secret-bearing GitLab loop inside the repository.

Secret-bearing jobs fail closed when a required protected credential, App/external status, released v0.2 harness, trusted base configuration, or current-head identity is absent. They never fall back to the proposed runtime or configuration. The generated self workflow has the same bootstrap boundary: establish the v0.2 release and base configuration through an approved trusted path before enabling agents. Never add a silent skip or treat unavailable credentials as a waiver.

## Artifact and promotion

Build once in the artifact phase. Record the configured artifact, SBOM, and provenance paths and SHA-256 values together with the source fingerprint. Staging and production re-hash all three files and compare the current source fingerprint before promotion; they reject missing, changed, or mismatched evidence. Do not rebuild between targets. Report protected-environment approval, promotion, and health separately.

## Bounded repair

Repair is opt-in and uses a failed receipt:

```text
sam-harness repair <root> --receipt <failed-receipt.json> --receipt-output true
```

Proceed only when correction is enabled, `write_repository` and `network` authority are granted, the configured command and budgets are present, and the user authorized source edits. Repair accepts only a failed or blocked v0.2 pipeline receipt for `static`, `test`, `review`, or `artifact`. The receipt must be inside the configured evidence directory, belong to this repository, contain failure evidence and valid timestamps, and have matching initial/final fingerprints equal to the current repository state. A failed review receipt must also contain an intact repair manifest and no unresolved arbiter conflict.

Enabled correction must carry the approved `filesystem_sandboxed: true` attestation. Provider-bound `repair` secrets additionally require correction `trusted_external_command: true`; any `trusted_config_arguments` follow the same safe indexed-helper rules as review. Local or waiver-only no-secret repair may keep a repository-relative command. A Codex argv using `codex exec --sandbox workspace-write -` is one example; verify the installed tool's containment before attesting. Sam Harness copies the frozen repository into a temporary autonomous Git sandbox without inherited Git remotes, repository hooks, or Git credentials. It gives the correction a clean process environment containing only configured `repair`-scope secrets. It rejects symlinks that escape the sandbox and aborts if the correction stages, commits, or changes Git control data. The correction command receives a structured prompt on standard input—not the raw receipt as instructions—with the sandbox root, fingerprint, attempt, cumulative budget, untrusted failed receipt, and its repair manifest when present. It must implement every manifest action in one coherent correction rather than stop after the first item or defer known work. `SAM_HARNESS_FAILED_RECEIPT` points to the sandboxed receipt. Fresh independent review of the resulting change remains required.

The standalone Git copy, fingerprint checks, and budget checks are defense in depth, not generic OS containment. Arbitrary reviewer and correction argv and their own sandbox implementation remain part of the trusted computing base.

After every attempt, Sam Harness measures cumulative changed files and lines from the frozen baseline. Before verification or application, it scans changed regular-file bytes, changed symlink targets, and added patch lines for every secret value actually exposed to the correction command. A new occurrence blocks without retry, target application, or patch emission; an unchanged baseline occurrence does not. Sam Harness then reruns `static` and `test` in the sandbox. Verification may not change the validated delta or Git state. Before applying a successful delta, runtime confirms that the target worktree and Git control state are still unchanged. It then applies only that validated delta and emits a correction-only patch plus `repair_patch_sha256` in the repair receipt. A failed receipt transported between clean CI workspaces is accepted only after repository identity, configuration digest, and before/final fingerprints match; root and configuration-source paths are rebound only after those checks.

When `open_change_request` is enabled, generated CI transports that patch and receipt as untrusted data to a separate publisher with commit/push authority but no review or repair credentials. The publisher requires exactly one patch and one repair receipt, checks their basename and SHA-256 relationship, runs `git apply --check`, disables repository hooks, applies only the patch on the configured isolated branch, and opens the PR or MR. It never publishes directly to a protected branch. GitLab additionally requires the operator-controlled `SAM_HARNESS_PUBLISH_REPAIR=true`; otherwise its publisher stays inactive.

Stop when receipt validation fails, the target changes, a symlink or Git boundary is crossed, a budget is reached, the attempt limit is exhausted, verification mutates the delta, or patch validation fails. Do not treat an unsuccessful attempt as permission for a larger change.

## Report the result

Name the requested phase, receipt path, repository fingerprint, exact final status, blocking findings, and all artifact hashes when present. For repair, also report the source receipt, patch path, and patch SHA-256; publishing remains unproven until the separate publisher succeeds. Report the next phase as unproven until its own receipt exists. A rollback receipt proves command execution, not restored business health; an observation receipt proves only its configured window and checks.
