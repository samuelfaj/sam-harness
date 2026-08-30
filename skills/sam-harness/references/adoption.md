# Adoption workflow

## CLI availability

Locate the repository root and check whether `sam-harness` is available. If it is absent, ask before downloading it. After approval, use [bootstrap.sh](../scripts/bootstrap.sh) on macOS/Linux or [bootstrap.ps1](../scripts/bootstrap.ps1) on Windows. The bootstrap requires Cosign and verifies the signed checksum bundle before installing the release binary in the user cache. The GitHub repository URL alone is enough to discover this skill, the verified CLI contract, and the commands below; do not fetch the binary until asked.

Prefer this installed `$sam-harness` skill when it is present. A fresh agent that only has the GitHub URL must follow the same scan → interview → plan → approve → apply contract so both paths produce the same canonical plan (profile, unresolved set, operations) for the same repository and answers.

## Guided commands

Customer-facing prompts may be en-US, pt-BR, or es. Run one controller; do not invent production commands or infrastructure.

```text
sam-harness onboard <root> [--answers file] [--answers-output file] [--locale en-US|pt-BR|es] [--accept plan-id]
sam-harness adopt <root> --auto [--answers file] [--answers-output file] [--locale en-US|pt-BR|es] [--accept plan-id]
sam-harness adopt <root> --guided [--answers file] [--answers-output file] [--locale en-US|pt-BR|es] [--accept plan-id] [--implement control]
```

These commands scan first, ask only decisions the tree or provider cannot prove (each with impact and a safe default), write a reusable credential-free answers file outside the repository, support resume and non-interactive input, and print the proposed files, authority changes, gates, and plan ID before any repository write. Apply only with `--accept` of that plan ID. Re-running an already applied plan is a no-op.

`--guided` also emits a coverage map whose only states are `existing-and-validated`, `missing-but-implementable`, `human-decision-required`, and `external-provider-required`. Approve each missing control separately. Convert an approved missing control into a bounded implementation task (acceptance, paths, commands, tests, budget, stop conditions), then re-scan and create a new expiring plan. Keep prior answers; reject stale approvals. Never turn a missing control into a waiver unless the operator gives an explicit risk and reason. Finish only when every control is implemented-and-proved, already validated, explicitly waived, or blocked with a named owner and next action. The finish report separates source, local checks, remote, CI, artifact, deployment, live observation, freeze, and production stability.

`sam-harness bootstrap github` and `sam-harness bootstrap gitlab` mutate provider policy only from a separately accepted plan. They never create, print, or persist credential values. Ordinary PR/MR job texts stay free of bound agent secrets. Readiness requires remote readback equal to the plan.

`sam-harness freeze check` evaluates the executable freeze policy. Ordinary features are blocked inside the window; only a fully evidenced configured exception may proceed.

`sam-harness stage classifier|context|planning|implementation|review|repair --input file` runs the executable lifecycle stages. Each receipt is bound to the approved plan ID and repository fingerprint. An agent summary is not proof. `scan` and `plan` remain the trusted deterministic controls and still do not write tracked files.

## Discovery

Run `sam-harness scan <root> --format json` or let `onboard`/`adopt` do it. Use the scan to identify stacks, workspaces, package managers, declared commands, CI providers, user interfaces, persistence, deployment files, Git state, and any existing harness.

Do not ask the user for facts the repository already proves. Do not turn a filename hint into a business fact.

## Required decisions

Collect these fields in a temporary JSON file outside the repository:

- `criticality`: `low`, `medium`, or `high`.
- `data_sensitivity`: `public`, `internal`, `confidential`, or `regulated`.
- `deploys_to_production`: boolean.
- `persistent_data`: boolean.
- `irreversible_actions`: boolean.
- `design_source_of_truth`: required when a user interface exists.
- `approvers`: one or more human owners.
- `allow_ci_changes`: boolean.
- `ci_providers`: `github`, `gitlab`, or both when CI changes are approved and the provider cannot be discovered.
- `ci_agent_runtime`: required when CI changes are approved and a production, regulated, or enabled workflow will run agents. `host` is `claude-code`, `codex`, `grok`, or `other` with `host_other`. `login_method` is `api_key`, `oidc`, or `cli_token` plus `login_environment` and `login_secret` names; `github_app`; or `manual` with `login_reason`. Identifiers only, never values. Interactive answers may use `ci_agent_host` (`other:<name>`) and `ci_agent_login` (`api_key ENV SECRET`).
- `standardize_commits`: boolean. When true, installed instructions require Conventional Commits (`feat:`, `fix:` for bugs, `docs:`, `test:`, `refactor:`, `chore:`, `ci:`).
- `allowed_actions`: any explicit subset of `write_repository`, `network`, `commit`, `push`, `release`, and `deploy`.
- `command_overrides`: argv arrays grouped by `<stack>:<path>` and gate when the user identifies authoritative commands.
- `command_waivers`: a reason grouped by `<stack>:<path>` when the user explicitly accepts that no executable gate exists.
- `ci_setup_commands`: ordered workdir and argv entries per provider when a managed CI job needs repository setup.
- `ci_setup_waivers`: an explicit reason per provider when the runner already contains everything required.
- `gitlab_image`: required for managed non-Go GitLab jobs so Sam Harness does not guess the execution image.
- `ci_secret_bindings`: provider secret names mapped to exact phase and environment-variable names. For managed production or regulated CI, `review` and enabled `repair` are separate scopes.
- `agent_secret_environments`: the protected agent environment name for each provider with any secret binding. It is distinct from `production_environment`. A waiver-only provider with no bindings needs no agent environment, but a provider that has both a binding and a waiver still does.
- `agent_control_planes`: required for every provider with a `review` or `repair` binding. GitHub uses `mode: github_app`, the required check, and the dedicated App ID/private-key secret names. GitLab uses `mode: external`, the required check, and its external trusted project. A waiver-only or unbound provider needs no entry.
- `ci_secret_waivers`: a non-empty provider-specific reason when its configured agentic commands need no credentials. Never use a waiver to hide an unknown credential decision.
- `risk_acceptance`: required when the user chooses a profile below the recommendation.
- `observation_window`, `rollback_owner`, and `production_environment` when production applies.
- `workflow`: required for production and regulated profiles. It must contain explicit executable decisions rather than prose. Read [workflow-configuration.md](workflow-configuration.md) for the exact nested shape only when one of those profiles applies.
- Reviewer and correction containment attestations: every reviewer sets `filesystem_read_only: true`, and enabled correction sets `filesystem_sandboxed: true`, only after the user verifies the chosen argv's actual sandbox flags.
- Trusted-command decisions for provider-bound agent secrets: every reviewer that can receive a `review` secret, and enabled correction that can receive a `repair` secret, sets `trusted_external_command: true` only after the executable is verified outside the target checkout. Put each safe trusted-config-relative helper's actual zero-based argv position in `trusted_config_arguments`; index 0 is forbidden. Local or waiver-only no-secret commands may remain repository-relative.

Until those decisions are explicit, planning reports `ci_agent_control_plane:<provider>`, `workflow.reviewers.<role>.trusted_external_command`, or `workflow.correction.trusted_external_command` as applicable. Do not clear them by adding control planes or attestations the operator has not verified.

An empty `allowed_actions` array means read-only authority and blocks application. Include `write_repository` only when the user authorizes Sam Harness to install the approved files. It is different from an omitted field.

If `scan` returns a `commands:<stack>:<path>` question, the repository does not expose an unambiguous command contract. Ask the user which existing commands are authoritative. Do not invent a gate or edit a manifest before that separate change is approved.

## Proposal

Run:

```text
sam-harness plan <root> --profile auto --answers <answers-file>
```

Summarize the recommendation without hiding controls that the repository cannot support yet. Show every create, update, and no-op operation. The plan file lives outside the target repository and expires after 30 minutes.

## Approval and application

Accept only a clear approval that refers to the current plan ID. Then run:

```text
sam-harness apply --plan <plan-file> --accept <plan-id>
```

If the command reports a stale fingerprint, scan and plan again. Do not reuse the old approval.

After application, run `doctor` before `check`. A failed command stays failed until the repository or configuration changes and the exact command passes.

Application installs the workflow contract; it does not execute staging, production, rollback, or migration. After `doctor`, use [lifecycle.md](lifecycle.md) only for phases the user has separately authorized.

Secret bindings, agent-environment mappings, and control-plane fields store identifiers, never values. Sam Harness does not provision provider credentials, install the GitHub App, create the GitLab external project, or prove remote settings from generated YAML.

For GitHub, create a dedicated App, keep its ID/private key and the named model secrets only in the configured agent environment, restrict that environment to default/protected branches, require human reviewers, enable prevent-self-review, and require the configured App check on exact PR and merge-queue heads. Add an external App/webhook handler for provider `merge_group` checks requests; it must create `repository_dispatch` event `sam_harness_merge_group_review` with the exact current `head_sha`, current default-branch `base_sha`, and provider `merge_group_ref`. The secret-bearing workflow must never listen directly to `merge_group`. Read the App installation, dispatcher, environment, approvals, secret association, required check, branch protection, and merge queue back from GitHub. Ordinary `pull_request`/`merge_group` jobs remain credential-free; the default-branch-owned agents workflow receives protected credentials and revalidates the current head and base before concluding the check.

For GitLab, configure the named external project as the trusted secret-bearing control plane, protect its variables/environment, and require its configured status on the exact current MR head. Read those settings plus protected branches and MR approvals back from GitLab. The generated MR YAML stays credential-free and omits the corresponding bound review or repair job; it does not implement a complete secret-bearing loop inside the repository.

Do not run or claim a secret-bearing review or repair until provider readback succeeds. A fork, missing protected secret, absent App/external status, missing released v0.2 harness, missing trusted base configuration, or changed head blocks. Never convert the absence into a skipped gate. Mixed bindings move only the bound scope: a credential-free review or correction remains in the ordinary CI flow when its evidence is locally available.

Generated secret-bearing jobs use a trusted released v0.2 CLI and pass `--config` for a separate trusted base configuration outside the target repository. Review also passes `--review-base`, `--review-base-sha`, and `--review-head-sha`, binding the receipt to the provider SHAs, both fingerprints, and a canonical patch hash. The configured executable must resolve outside the target, and any explicitly indexed helper resolves from the trusted configuration directory. This creates an intentional bootstrap boundary for the Sam Harness repository itself: publish or otherwise approve v0.2 and land the base configuration through a trusted path before enabling agent credentials. Do not use the proposed CLI, configuration, executable, or helper script to bootstrap its own secret access.

## Upgrade from v0.1

`upgrade` creates a new expiring plan; it does not rewrite the installed repository directly. It preserves decisions that remain readable from the installed configuration and merges explicit values from an optional answers file.

A legacy production or regulated configuration does not contain the required v0.2 workflow, CI secret decision, protected agent-environment/control-plane mappings, filesystem attestations, or trusted-external-command decisions. Collect the complete [workflow configuration](workflow-configuration.md), including provider bindings or explicit waivers, protected agent environments, control planes, verified reviewer/correction attestations, and any trusted config argv positions, in a temporary answers file outside the repository, then run:

```text
sam-harness upgrade <root> --to 0.5.0 --answers <answers-file>
```

Show unresolved decisions, every file operation, and the new plan ID. Apply only after the user approves that exact ID. If the repository fingerprint changes or the plan expires, discard it and create another upgrade plan.
