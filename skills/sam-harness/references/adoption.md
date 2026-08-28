# Adoption workflow

## Discovery

Run `sam-harness scan <root> --format json`. Use its output to identify stacks, workspaces, package managers, declared commands, CI providers, user interfaces, persistence, deployment files, Git state, and any existing harness.

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
- `allowed_actions`: any explicit subset of `write_repository`, `network`, `commit`, `push`, `release`, and `deploy`.
- `command_overrides`: argv arrays grouped by `<stack>:<path>` and gate when the user identifies authoritative commands.
- `command_waivers`: a reason grouped by `<stack>:<path>` when the user explicitly accepts that no executable gate exists.
- `ci_setup_commands`: ordered workdir and argv entries per provider when a managed CI job needs repository setup.
- `ci_setup_waivers`: an explicit reason per provider when the runner already contains everything required.
- `gitlab_image`: required for managed non-Go GitLab jobs so Sam Harness does not guess the execution image.
- `risk_acceptance`: required when the user chooses a profile below the recommendation.
- `observation_window`, `rollback_owner`, and `production_environment` when production applies.

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
