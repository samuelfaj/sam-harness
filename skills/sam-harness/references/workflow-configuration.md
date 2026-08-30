# Workflow configuration

Use this reference only when collecting production or regulated answers. Put `workflow` at the top level of the temporary answers JSON.

## CI secret bindings and agent control planes

`ci_secret_bindings` records identifiers only: the lifecycle `scope`, the environment-variable name exposed to that command, and the provider secret or variable name. Never put a credential value in an answers file, canonical configuration, generated workflow, receipt, or patch.

For managed production or regulated CI, provide a `review` binding and, when correction is enabled, a separate `repair` binding for every selected provider. Also map each provider with any secret binding to one protected environment in `agent_secret_environments`. Every provider with a `review` or `repair` binding also needs an `agent_control_planes` entry. If a provider's commands are genuinely credential-free, record a non-empty provider reason in `ci_secret_waivers` instead. A production-only binding does not satisfy the agentic review/repair decision.

```json
{
  "ci_secret_bindings": {
    "github": [
      {"scope": "review", "environment": "REVIEW_API_KEY", "secret": "OPENAI_API_KEY"},
      {"scope": "repair", "environment": "REPAIR_API_KEY", "secret": "CODEX_REPAIR_API_KEY"}
    ],
    "gitlab": [
      {"scope": "review", "environment": "REVIEW_API_KEY", "secret": "OPENAI_API_KEY"}
    ]
  },
  "agent_secret_environments": {
    "github": "sam-harness-agents",
    "gitlab": "sam-harness-agents"
  },
  "agent_control_planes": {
    "github": {
      "mode": "github_app",
      "required_check": "sam-harness/review",
      "app_id_secret": "SAM_HARNESS_APP_ID",
      "app_private_key_secret": "SAM_HARNESS_APP_PRIVATE_KEY"
    },
    "gitlab": {
      "mode": "external",
      "required_check": "sam-harness/review",
      "external_project": "trusted/review-control"
    }
  }
}
```

Valid scopes are `static`, `test`, `review`, `repair`, `artifact`, `staging`, `production`, `observe`, `rollback`, and `migration`. Environment and secret names use letters, digits, and underscores and may not start with a digit. The same environment name may appear only once per provider and scope.

`agent_secret_environments` is the answers-file key; after application, the same map appears as `ci.agent_secret_environments` in `.sam-harness/config.yaml`. Its provider keys are `github` or `gitlab`. Each environment name must match `^[A-Za-z0-9][A-Za-z0-9_-]*$`. It is an identifier, not a secret or proof that the remote environment is protected, and it is distinct from `production_environment`. Every provider with at least one binding requires the map entry, even if it also has a waiver. A waiver-only provider with no bindings requires none.

`agent_control_planes` is also a top-level answers key and becomes `ci.agent_control_planes` after application. A `github` entry uses `mode: github_app`, requires `required_check`, `app_id_secret`, and `app_private_key_secret`, and forbids `external_project`. A `gitlab` entry uses `mode: external`, requires `required_check` and a namespace-qualified `external_project`, and forbids the App fields. The check must match `^[A-Za-z0-9][A-Za-z0-9._/-]{0,127}$`; App secret names must match `^[A-Za-z_][A-Za-z0-9_]*$`. A provider with a review or repair binding requires its matching control plane even if it also has a waiver. Waiver-only or unbound providers need none. Until supplied, planning reports `ci_agent_control_plane:<provider>`.

`ci_agent_runtime` becomes `ci.agent_runtime` after application. It names the host that runs CI review and repair (`claude-code`, `codex`, `grok`, or `other` plus `host_other`) and how that host logs in. `api_key`, `oidc`, and `cli_token` store environment-variable and provider-secret names only. `github_app` uses a GitHub App login distinct from the check-publishing control plane unless the operator says otherwise. `manual` records an explicit no-credential or out-of-band reason. Sam Harness never serializes secret values. Until supplied, planning reports `ci_agent_host` and `ci_agent_login`.

Ordinary `.github/workflows/sam-harness.yml` pull-request and merge-group jobs receive no bound agent secrets. When GitHub has any review or repair binding, application also renders default-branch-owned `.github/workflows/sam-harness-agents.yml`. It uses `pull_request_target` for bound pull-request review, `repository_dispatch` type `sam_harness_merge_group_review` for merge-queue review, and a failed ordinary run's `workflow_run` for bound repair. The secret-bearing workflow never listens directly to `merge_group`, whose definition would come from the synthetic queue ref. An external App/webhook must observe the provider event and dispatch exact `head_sha`, current default-branch `base_sha`, and `merge_group_ref`; the generated resolver re-fetches the `gh-readonly-queue` ref and default-branch ref before review and before check conclusion. Missing dispatch or drift blocks. Jobs check out the exact head and base SHAs as untrusted data, use trusted base configuration and the released matching-version harness, and do not execute target setup, hooks, caches, local actions, or repository commands. Model secrets exist only on their matching review or repair step. Non-publisher checkouts do not persist Git credentials, and the publisher receives no model secret.

Use a dedicated GitHub App. Store the values named by `app_id_secret` and `app_private_key_secret` only in the configured agent environment, never as repository-level secrets; every generated job that reads them declares that environment. Configure the environment for default/protected branches only, require human reviewers, enable prevent-self-review, and require the exact `required_check` on pull-request and merge-queue heads. The in-workflow App tokens request `checks:write`, `pull_requests:read`, or `contents:read` only where needed. The external dispatcher needs permission to create repository dispatch events; grant `contents:write` and `pull_requests:write` to the repair publisher only when enabled. The dispatcher payload is `{ "event_type": "sam_harness_merge_group_review", "client_payload": { "head_sha": "<merge-group SHA>", "base_sha": "<current default-branch SHA>", "merge_group_ref": "refs/heads/gh-readonly-queue/<provider ref>" } }`. The App creates the check pending on the expected head and re-fetches the current PR head or merge-group and base refs before concluding it. Drift fails; cancellation leaves the required check pending; automatic repair is not published for a merge-group run.

GitLab merge-request YAML receives no bound review or repair secret and emits no corresponding secret-bearing job. The configured `external_project` must be a separately trusted control plane that reviews or repairs the exact current MR head and publishes `required_check`. Configure its protected variables, protected environment, status check, protected branch, and approvals outside this repository. Sam Harness does not generate or claim a complete GitLab secret-bearing loop. Missing external status blocks merge rather than becoming a skipped local job.

Mixed bindings move only the bound scope. Credential-free review remains in the ordinary GitHub/GitLab workflow when only repair is bound. Credential-free static/test/artifact repair remains there when only review is bound; GitHub can still repair a failed bound review from its trusted agents workflow, while GitLab cannot invent a local repair from an external review receipt. No-secret correction commands may remain repository-relative and continue to use the ordinary isolated repair and publisher boundaries.

All provider credential creation, App installation, environment protection, check requirements, and approval rules remain external to Sam Harness. Remotely read them back before claiming the gate. If a fork or untrusted contribution cannot access protected credentials, the released runtime or trusted base configuration is missing, the external/App status is absent, or the head changes, the gate blocks. Never omit the gate or treat missing credentials as a waiver.

The required review is pre-merge and must evaluate the exact proposed SHA and diff. A generated workflow may repeat review on the trusted default branch, but that later run never substitutes for the pre-merge gate. Secret-bearing review uses `--config` for `.sam-harness/config.yaml` in a separate trusted base checkout plus `--review-base`, `--review-base-sha`, and `--review-head-sha`. Runtime compares the 40- or 64-character provider SHAs with both Git `HEAD` values before and after the reviewers. The receipt binds those SHAs, both fingerprints, the canonical patch, and its SHA-256.

This is also a bootstrap limit for the generated workflow that validates the Sam Harness repository itself. Its matching release and trusted base configuration must first be established through an approved trusted path. Only after that state, the App or external control plane, required check, and protected environment have been read back may review or repair receive secrets.

## Command shape

Artifact, deployment, health, observation, and migration commands use the same object:

```json
{
  "name": "human-readable control name",
  "workdir": ".",
  "command": ["executable", "argument"],
  "required": true,
  "timeout_seconds": 600
}
```

Keep `workdir` inside the repository and preserve every command as an argv array. Production and regulated commands are required. Use real repository or provider commands approved by the user; never store a generated shell string or invent a command to satisfy validation. Approval of that argv during planning does not grant standing permission to execute its repository or provider effects later.

## Static and test guard coverage

`static_guards` and `test_guards` are guard sets. Each has `commands`, keyed by category with a command object, and `waivers`, keyed by category with a non-empty auditable reason:

```json
{
  "commands": {
    "format": {
      "name": "format check",
      "workdir": ".",
      "command": ["existing-tool", "format-check"],
      "required": true,
      "timeout_seconds": 600
    }
  },
  "waivers": {
    "schema": "No persisted schema exists in this repository."
  }
}
```

Every category must appear exactly once: in `commands` or in `waivers`, never both.

Static categories: `format`, `lint`, `typecheck`, `architecture`, `security`, `dependencies`, `schema`, `project_policies`.

Test categories: `unit`, `integration`, `contract`, `business_invariants`, `property`, `mutation`, `e2e`, `performance`.

`plan` may propose scan-detected stack commands as confirmable defaults for matching categories (`lint`/`typecheck`/`test`/`format`/`security`). Those defaults do not decide a category until `confirm_guard_defaults` lists it or the operator writes the command or waiver. Undetected categories stay empty; argv is never invented. Interactive onboard asks yes/no for each proposed command. `confirm_runtime_reviewers` installs the `ci_agent_runtime` host recipe (codex, claude-code, or grok) for all six roles with `review_timeout_seconds` as the budget.

Production and regulated adoption may set `adoption_phase` to `core` (local+CI+tests+review), `artifact`, or `delivery` (staging/production/observation). Later-phase slots stay unresolved for later phases and do not block the current phase’s plan. An empty current phase still blocks. Omit the field for full delivery completeness.

Use a waiver only when an executable control genuinely does not apply or does not yet exist and the user accepts the gap. The receipt records it as skipped evidence; a waiver is not a passed check. Static and test phases also execute discovered repository gates.

## Reviewers and correction

`reviewers` contains exactly one entry for each role:

```json
{
  "role": "architecture",
  "command": ["npx", "--yes", "@openai/codex@0.150.1", "exec", "--sandbox", "read-only", "--ephemeral", "--output-schema", "reviewer-output.schema.json", "-"],
  "timeout_seconds": 600,
  "filesystem_read_only": true,
  "trusted_external_command": true,
  "trusted_config_arguments": [8]
}
```

Repeat the object for `security`, `correctness`, `test_quality`, `business_rules`, and `simplicity`, substituting the actual approved argv. Review commands receive the role in `SAM_HARNESS_REVIEW_ROLE` and the structured prompt on standard input. Their exact JSON must match `.sam-harness/reviewer-output.schema.json`: `review_complete` is true, and each finding includes its exact `required_change` and observable `acceptance`. Every reviewer reports all actionable findings in its role in the same pass. `filesystem_read_only: true` is a required user attestation that the chosen executable is independently configured for read-only filesystem access.

For example, the Codex reviewer above uses `--sandbox read-only`; its schema helper is actual zero-based argv index 8. Verify those flags against the installed Codex version and target environment, and ensure the command emits only the required final findings JSON; Sam Harness does not prove that arbitrary reviewer argv is contained by the operating system. Replace the illustrative package version only with another exact approved version.

`correction` has this shape:

```json
{
  "enabled": true,
  "command": ["npx", "--yes", "@openai/codex@0.150.1", "exec", "--sandbox", "workspace-write", "--ephemeral", "-"],
  "filesystem_sandboxed": true,
  "trusted_external_command": true,
  "trusted_config_arguments": [],
  "max_attempts": 2,
  "max_changed_files": 5,
  "max_changed_lines": 200,
  "branch_prefix": "sam-harness/repair/",
  "open_change_request": false
}
```

Correction is opt-in. If enabled, `filesystem_sandboxed: true`, positive budgets, and an explicit branch prefix are required. The attestation means the chosen executable is configured with its own filesystem sandbox. For example, a Codex correction command may include `codex exec --sandbox workspace-write -`; verify the installed CLI's semantics before approving it.

When any provider binds a `review` secret, all six reviewers also require `trusted_external_command: true`. When an enabled correction has any provider-bound `repair` secret, correction requires the same attestation. Runtime then requires `--config` outside the target repository and resolves argv[0] through a trusted runner path that is not inside the target. A bare executable name such as `codex` may resolve through the runner's controlled `PATH`; `./tool`, `tools/tool`, or another executable path relative to the target is rejected. The attestation records an operator decision about that executable; it does not make an unknown executable trustworthy.

`trusted_config_arguments` contains unique, actual zero-based argv positions from 1 through the final argument; index 0 is always forbidden. Use it only for a separate safe relative helper path, such as a schema or interpreter script, that should resolve from the directory containing the trusted external configuration. Runtime requires each listed path to stay inside that directory, contain no symlink component, and name a regular file. An unlisted relative/path-like argument, or an interpreter input that could select target-controlled content, blocks secret-bearing execution.

For this boundary, an argument is path-like when it is absolute, contains `/` or `\`, starts with `.`, or ends in `.bash`, `.cjs`, `.js`, `.json`, `.mjs`, `.ps1`, `.py`, `.rb`, `.schema`, `.sh`, `.toml`, `.ts`, `.tsx`, `.xml`, `.yaml`, `.yml`, or `.zsh`. The interpreters and dispatchers `bash`, `bun`, `dash`, `deno`, `env`, `go`, `ksh`, `node`, `nodejs`, `npm`, `osascript`, `perl`, `php`, `pnpm`, `powershell`, `pwsh`, `python`, `python3`, `ruby`, `sh`, `uv`, `xargs`, `yarn`, and `zsh` require every non-flag input other than `-` to be indexed. An inline value such as `--schema=reviewer.schema.json` cannot be indexed separately and is rejected; use `--schema`, `reviewer.schema.json` and index the second token.

These external-command requirements apply only to the scopes that receive provider-bound secrets. A local run or an explicitly waiver-only provider with no bindings may continue to use repository-relative reviewer and correction commands; it does not need a false trust claim.

`npx` is the only package-dispatch exception: it may have only optional `--yes` or `-y` before one `name@MAJOR.MINOR.PATCH` or `@scope/name@MAJOR.MINOR.PATCH` package reference. A SemVer prerelease or build suffix is allowed. Unpinned packages, `latest`, ranges, and other pre-package flags block. The package reference and later subcommands are not helper paths; any later path-like argument still needs its own `trusted_config_arguments` index. Other interpreter or dispatcher commands, including `npm`, `pnpm`, and `yarn`, remain subject to the rule that every non-flag input capable of selecting a file must be an indexed trusted-config-relative helper.

Sam Harness creates a standalone temporary Git repository and verifies its delta, but it is not a general operating-system sandbox for arbitrary argv. The reviewer and correction executables and their declared sandbox flags remain part of the trusted computing base. Runtime consolidates complete reviewer output into one hashed manifest bound to review lineage. A failed review receipt is repairable only when that manifest is intact and conflict-free. The structured correction prompt contains the sandbox root, current fingerprint, attempt, cumulative budget, failed receipt, and repair manifest as untrusted data; it requires every manifest action to be independently verified and applied in one coherent correction and does not send the raw receipt as instructions. Automatic review repair is limited to one pass; the resulting branch is re-reviewed but cannot spawn another automatic repair branch. The sandbox receipt path is also available as `SAM_HARNESS_FAILED_RECEIPT`.

Set `open_change_request` only when the user also grants the network, commit, and push authority needed by the generated publisher. The publisher is a separate data-only boundary and never receives review or repair credentials.

## Artifact and delivery

The enclosing nested shape is:

```json
{
  "enabled": true,
  "static_guards": {"commands": {}, "waivers": {}},
  "test_guards": {"commands": {}, "waivers": {}},
  "reviewers": [],
  "correction": {},
  "artifact": {
    "build": {},
    "artifact_path": "dist/application.bin",
    "sbom": {},
    "sbom_path": "dist/sbom.cdx.json",
    "provenance": {},
    "provenance_path": "dist/provenance.json"
  },
  "deployment": {
    "staging": {},
    "production": {},
    "rollback": {},
    "health_checks": [],
    "observation_checks": [],
    "canary_percentages": [5, 25, 100]
  },
  "migration": [],
  "release_schedule": {
    "cron": "0 15 * * 3",
    "timezone": "UTC"
  }
}
```

Replace every `{}` command slot with the command shape above. Provide at least one required health check, observation check, and migration command. Artifact, SBOM, and provenance paths must be contained repository-relative files. Canary percentages are unique ascending integers from 1 through 100. The cron has five fields and the timezone is an IANA name.

The artifact command builds once. Its receipt records the artifact, SBOM, and provenance paths and SHA-256 values plus the source fingerprint. Staging and production receive the recorded artifact path and SHA-256 through `SAM_HARNESS_ARTIFACT_PATH` and `SAM_HARNESS_ARTIFACT_SHA256`; promotion rechecks all three hashes and the source fingerprint instead of rebuilding.

## Decision boundary

Show the user the guard commands or waiver reasons, scoped secret names or provider waivers, protected agent environments, agent control planes and required checks, filesystem and trusted-external-command attestations, trusted-config argv indices, executable workflow commands, workdirs, timeouts, budgets, artifact paths, canary sequence, and schedule before planning. A complete JSON shape does not make guessed values acceptable. Leave the plan blocked until every category and required value comes from the repository or an explicit user decision.
