---
name: sam-harness
description: Inspect a software repository, ask for the risk and authority facts that cannot be discovered, propose a development harness, and apply it only after approval. Use when the user asks to apply, adopt, instalar, aplicar, configurar, or auditar sam-harness in a repository, including "Aplique o sam-harness aqui", "Apply sam-harness here", and "Aplica sam-harness aquí".
---

# Sam Harness

Turn the repository's real commands, architecture, delivery path, and risk into durable agent instructions and deterministic gates.

## Operating contract

- Treat the repository and explicit user statements as the source of truth. Do not infer authorization from available tools.
- Preserve unrelated work. `scan` and `plan` must not change tracked files.
- Separate source, local checks, commit, remote, review, CI, artifact, deployment, and live proof.
- Never apply a plan until the user approves its exact plan ID.
- Treat every plan as short-lived. If it expires, scan again and obtain approval for the new ID.
- Never commit, push, release, deploy, alter credentials, or perform an irreversible action unless the user grants that exact authority.
- If the repository changes after planning, discard the stale plan and create another one.

## Workflow

1. Read [references/adoption.md](references/adoption.md).
2. Locate the repository root and check whether the `sam-harness` binary is available.
3. If the binary is absent, ask before downloading it. After approval, use the platform bootstrap in `scripts/`; it verifies the release signature and checksum.
4. Run `sam-harness scan <root> --format json`.
5. Ask only for unresolved decisions. Keep answers in a temporary JSON file outside the repository.
6. Read the conditional references that affect this repository:
   - [references/profiles.md](references/profiles.md) for the recommended maturity profile.
   - [references/stacks.md](references/stacks.md) for each detected stack.
   - [references/design-and-human-finish.md](references/design-and-human-finish.md) when a user-facing surface exists.
   - [references/enterprise.md](references/enterprise.md) for production, regulated, multi-service, data-migration, provider, or retirement work.
7. Run `sam-harness plan <root> --profile auto --answers <temporary-file>`.
8. Show the profile rationale, unresolved risk, exact operations, and plan ID. Wait for explicit approval of that ID.
9. Apply with `sam-harness apply --plan <plan-file> --accept <plan-id>`.
10. Run `sam-harness doctor <root>` and `sam-harness check <root>`.
11. Report what changed and the proof state reached. Do not imply remote, CI, release, deployment, or live success without its own receipt.

Read [references/evidence-and-authority.md](references/evidence-and-authority.md) before any remote, release, deployment, migration, security-sensitive, or delegated operation.

## Stop conditions

Stop and ask when a required command is ambiguous, an existing managed file cannot be merged safely, the plan has unresolved decisions, the plan fingerprint is stale, a gate fails, or the next action crosses configured authority.
