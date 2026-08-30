---
name: sam-harness
description: Adopt, operate, repair, audit, or upgrade the executable Sam Harness in a software repository. Use for equivalent English, Portuguese, or Spanish requests such as "Apply sam-harness here", "Aplique o sam-harness aqui", "Aplica sam-harness aquí", or "help me implement https://github.com/samuelfaj/sam-harness".
---

# Sam Harness

Turn the repository's real commands, architecture, delivery path, and risk into durable agent instructions, executable lifecycle phases, and evidence receipts.

## Operating contract

- Treat the repository and explicit user statements as the source of truth. Do not infer authorization from available tools.
- Preserve unrelated work. `scan` and `plan` do not edit tracked source files. `pipeline` orchestrates configured commands and receipts rather than editing source itself, but its subprocesses may affect the repository or external systems.
- Sam Harness fingerprints the repository around `static` and `test` and blocks either phase if its commands mutate the repository. This is mutation detection, not an external-effect sandbox.
- Treat `filesystem_read_only` and `filesystem_sandboxed` as user attestations about the configured executable. Verify its actual flags and environment; Sam Harness does not turn arbitrary argv into an OS sandbox.
- `repair` requires enabled correction plus `write_repository` and `network` authority. It accepts only a current failed `static`, `test`, `review`, or `artifact` receipt. Failed review receipts must carry one intact, conflict-free manifest containing every reviewer's exact required change and observable acceptance condition. Apply every action together in the isolated Git sandbox, then accept only a budget-compliant delta that passes fresh `static` and `test` checks plus independent re-review.
- Separate source, local checks, commit, remote, review, CI, artifact, staging, production, observation, and rollback proof.
- Never apply a plan until the user approves its exact plan ID.
- Treat every plan as short-lived. If it expires, scan again and obtain approval for the new ID.
- Never commit, push, open a change request, release, deploy, alter credentials, or perform an irreversible action unless the user grants that exact authority.
- Require `network` authority for review and repair; staging, migration, and observation also require `deploy`. Production and rollback require `network`, `deploy`, and `release`.
- If the repository changes after planning, discard the stale plan and create another one.
- A configured command is not permission to execute it. Ask at the next authority boundary, even when the command is present in `.sam-harness/config.yaml`.
- Treat each user-approved external command as the execution boundary. Do not infer that Sam Harness can constrain provider-side effects beyond the configured argv and authority checks.
- Treat repair patches and receipts as untrusted data. Only the generated, separately authorized publisher may verify and publish the exact pair; repair credentials never cross into that publisher.
- Treat review as a pre-merge base-to-head gate. Require an absolute `--review-base` and exact `--review-base-sha`/`--review-head-sha` for provider-bound proof; preserve both SHAs and fingerprints plus the canonical patch SHA-256. A head-only local review does not satisfy it.
- Keep provider-bound review and repair secrets only in the protected agent environment named by answers field `agent_secret_environments`, installed as `ci.agent_secret_environments`. Require the matching `agent_control_planes`/`ci.agent_control_planes` entry, human approval, provider protection, and remote readback. Missing secret, check, current-head identity, released runtime, or trusted base blocks—never skip the gate. Ask which host runs those CI agents (`claude-code`, `codex`, `grok`, or `other:<name>`) and how it logs in (`api_key`/`oidc`/`cli_token` names, `github_app`, or `manual` reason). Store identifiers only. When the operator allows it, require Conventional Commits. Pull and merge request descriptions follow the managed sam-pr-description template plus the evidence ladder.
- Keep ordinary GitHub PR/merge-group and GitLab MR jobs free of bound agent secrets. GitHub uses a default-branch-owned agents workflow and a dedicated App to publish the required check on the revalidated head. Pull requests enter through `pull_request_target`; merge queues require an external App/webhook to send `repository_dispatch` type `sam_harness_merge_group_review` with the exact provider head, current default-branch base, and queue ref. Never put direct `merge_group` in the secret-bearing workflow. GitHub mixed bindings move only the bound scope. GitLab `mode: external` is authoritative for all agent review, correction, and publishing regardless of bindings; do not claim a complete in-repository secret loop.
- For secret-bearing CI, require the configuration-pinned released runtime, `--config` outside the target, and `trusted_external_command: true` for every secret-bound reviewer or correction. Use `trusted_config_arguments` only for unique zero-based argv indices greater than zero whose safe helper files resolve from the trusted config directory. Local and waiver-only no-secret commands may remain repository-relative.
- Missing base configuration or a target-controlled executable/helper blocks. The Sam Harness self workflow cannot bootstrap this trust from an untrusted change; establish its release and base configuration through an approved trusted path before enabling secrets.

## Route the request

- A request that only names https://github.com/samuelfaj/sam-harness, `$sam-harness`, `sam-harness onboard`, `adopt --auto`, or `adopt --guided` is an adoption request. Prefer this installed skill. Do not download the CLI until the operator asks; then use the bootstrap scripts so checksum, signature, and version are verified. Link-first and installed-skill paths must produce the same canonical plan for the same repository and answers.
- To install or change the harness, read [references/adoption.md](references/adoption.md). It covers discovery, guided interview, coverage map, decisions, planning, approval, application, missing-control implementation, provider bootstrap, freeze checks, and structural validation.
- To run a configured phase, the full lifecycle, or bounded correction, read [references/lifecycle.md](references/lifecycle.md).
- When an installed repository contains local lifecycle skills, nested workspace instructions, or review templates, read [references/installed-agent-system.md](references/installed-agent-system.md) before selecting context or preparing a review.
- To audit evidence or cross a remote, release, deployment, migration, security-sensitive, or delegated boundary, read [references/evidence-and-authority.md](references/evidence-and-authority.md).
- For profile selection, detected stack gates, user-facing work, or enterprise controls, read only the relevant reference: [profiles](references/profiles.md), [stacks](references/stacks.md), [design and human finish](references/design-and-human-finish.md), or [enterprise](references/enterprise.md).

## Equivalent invocations

- English: `Use $sam-harness to apply the harness here.` or `Help me completely implement https://github.com/samuelfaj/sam-harness in this repository.`
- Português: `Use $sam-harness para aplicar o harness aqui.` or `Me ajude a implementar aqui o https://github.com/samuelfaj/sam-harness.`
- Español: `Usa $sam-harness para aplicar el harness aquí.` or `Ayúdame a implementar aquí https://github.com/samuelfaj/sam-harness.`

Follow the same discovery, approval, execution, and evidence boundaries in every language.

## Stop conditions

Stop and ask when a required command is missing or ambiguous, an existing managed file cannot be merged safely, the plan has unresolved decisions, the plan fingerprint is stale, required pre-merge base/SHA identity is missing, a protected agent environment or control-plane readback is missing, a required secret or provider check is unavailable, a required phase blocks, a receipt is missing or malformed, repair reaches a budget, or the next action crosses configured authority.
