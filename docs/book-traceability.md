# Book traceability

This matrix keeps the implementation tied to the 20 chapters of Development Harness. A chapter needs an executable control, a required decision, a generated template, or a behavioral test. Summary prose alone does not count.

| Chapter | Mechanism from the book | Representation in sam-harness |
|---:|---|---|
| 1 | A harness governs execution around the model | Versioned profiles, phases, workflow controls, authority, and evidence types in `internal/model/model.go` |
| 2 | Turn requests into executable contracts | Repository fingerprint, expiring plan ID, unresolved-decision blocking, and exact operations in `internal/planner/planner.go` |
| 3 | Select context and govern memory and instructions | Request routing plus conditional, progressive-disclosure references in `skills/sam-harness/SKILL.md` |
| 4 | Separate classifier, context, planning, execution, and proof | Public command boundaries in `internal/cli/cli.go` for scan, plan, apply, check, pipeline, repair, doctor, and upgrade |
| 5 | Use a bounded implementation loop | Current repairable-receipt validation, autonomous Git sandboxing, cumulative budgets, fresh static/test gates, and correction-only patch evidence in `internal/pipeline/repair.go` |
| 6 | Review and correct with independent roles | Canonical base-to-head patch evidence plus verified provider SHAs and immutable fingerprints in `internal/pipeline/review.go`, with six-role JSON, severity, and mutation blocking in `internal/pipeline/pipeline.go` |
| 7 | Make tests deterministic guardrails | Discovered gates, full static/test category coverage, command-or-waiver execution, containment, timeouts, and receipts in `internal/pipeline/pipeline.go` |
| 8 | Separate capability from authority | Per-action grants, exact-scope secret bindings, protected agent-environment and provider-control-plane identifiers, and filesystem/trusted-command attestations in `internal/model/model.go` |
| 9 | Control dependencies and supply chain | Build-once artifact, SBOM, and provenance SHA-256 identities tied to the source fingerprint in `internal/pipeline/pipeline.go` |
| 10 | Integrate through CI and protected review | Credential-free PR/MR CI, default-branch-owned GitHub App agents with external merge-group dispatch and current-head required checks, external GitLab agent status, exact base/head identities, mixed-binding repair separation, protected production, and manual rollback in `internal/render/render.go` |
| 11 | Promote immutable artifacts and handle data safely | Digest-checked staging/production promotion, migration commands, health checks, and rollback in `internal/pipeline/pipeline.go` |
| 12 | Observe production and learn from incidents | Observation and rollback phases with exact receipts and configured health checks in `internal/pipeline/pipeline.go` |
| 13 | Balance people, governance, and cost | Approvers, correction budgets, canary percentages, release cadence, and authority configuration in `internal/model/model.go` |
| 14 | Adopt controls by maturity and risk | Profile recommendation, downgrade acceptance, category-level command-or-waiver requirements, and production/regulated workflow blocking in `internal/planner/planner.go` |
| 15 | Learn by running laboratories | Executable production lifecycle fixture and explicit command inventory in `testdata/fixtures/full-flow/answers.production.json` |
| 16 | Use reusable playbooks and rubrics | Seven repository-local lifecycle skills plus generated workflow, reviewer, change-budget, runbook, and review-template rubrics in `internal/render/render.go` |
| 17 | Preserve design consistency and finish for humans | Generated browser, accessibility, localization, state, and human-label gates in repository docs and provider review templates from `internal/render/render.go` |
| 18 | Coordinate enterprise use cases through a control plane | Canonical workflow rendering plus managed root and nested workspace contracts in `internal/render/render.go` |
| 19 | Govern the agentic stack lifecycle | Version reporting, legacy-answer merging, configuration-preserving upgrade planning, and explicit target validation in `internal/cli/cli.go` |
| 20 | Roll out, operate, and retire deliberately | Generated release lifecycle skill, rollout observation, and retirement runbooks with migration and rollback controls in `internal/render/render.go` |
