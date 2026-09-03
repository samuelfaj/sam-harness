<!-- sam-harness:start -->
## Sam Harness

This repository uses sam-harness 0.10.0 with the production profile.

Read these files before changing code:

- [.sam-harness/config.yaml](.sam-harness/config.yaml) for commands, profile, and authority.
- [.sam-harness/WORKFLOW.md](.sam-harness/WORKFLOW.md) for the executable lifecycle.
- [.sam-harness/REVIEWERS.md](.sam-harness/REVIEWERS.md) for independent review roles.
- [.sam-harness/CHANGE_BUDGET.md](.sam-harness/CHANGE_BUDGET.md) for bounded correction.
- [.sam-harness/INVARIANTS.md](.sam-harness/INVARIANTS.md) for conditions that must stay true.
- [.sam-harness/GATES.md](.sam-harness/GATES.md) for the evidence required before promotion.
- [.sam-harness/DELEGATION.md](.sam-harness/DELEGATION.md) before delegating or crossing a permission boundary.
- [.sam-harness/UX_GATES.md](.sam-harness/UX_GATES.md) for user-facing work.

Do not treat an edit, test, commit, push, review, CI run, artifact, deployment, or live observation as the same state. Report each state only with its own evidence. Preserve unrelated work. Do not commit, push, release, deploy, alter credentials, or perform an irreversible operation unless the user has granted that exact authority.

After install or upgrade, unify redundant host CI: keep generated `sam-harness-*` jobs as the canonical gates and remove host jobs that only repeat those lint, typecheck, unit, contract, build, or browser commands. Suggested stages: check → test → build → deploy → verify → release → monitor. Exception path: failure → repair / rollback → verify.
<!-- sam-harness:end -->
