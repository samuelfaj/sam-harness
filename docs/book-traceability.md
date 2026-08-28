# Book traceability

This matrix keeps the implementation tied to the 20 chapters of Development Harness. A chapter needs an executable control, a required decision, a generated template, or a behavioral test. Summary prose alone does not count.

| Chapter | Mechanism from the book | Representation in sam-harness |
|---:|---|---|
| 1 | A harness governs execution around the model | Root contract and evidence ladder in `AGENTS.md` |
| 2 | Turn requests into executable contracts | Approved plans, invariants, and gates in `internal/planner/planner.go` |
| 3 | Select context and govern memory and instructions | Progressive disclosure and adapters in `skills/sam-harness/SKILL.md` |
| 4 | Separate classifier, context, planning, execution, and proof | Public command boundaries in `internal/cli/cli.go` |
| 5 | Use a bounded implementation loop | Fingerprint, expiry, approval, and stop conditions in `internal/apply/apply.go` |
| 6 | Review and correct with independent roles | Role boundaries and returned-evidence rules in `.sam-harness/DELEGATION.md` |
| 7 | Make tests deterministic guardrails | Gate execution and specific receipts in `internal/check/check.go` |
| 8 | Separate capability from authority | Least-authority matrix in `.sam-harness/DELEGATION.md` |
| 9 | Control dependencies and supply chain | Signed checksums, SBOMs, and provenance in `.github/workflows/release.yml` |
| 10 | Integrate through CI and protected review | Pinned verification workflow in `.github/workflows/ci.yml` |
| 11 | Promote immutable artifacts and handle data safely | Reconciliation, restore, cutover, and rollback in `.sam-harness/runbooks/migration.md` |
| 12 | Observe production and learn from incidents | Live observation and recovery in `.sam-harness/runbooks/incident.md` |
| 13 | Balance people, governance, and cost | Explicit authority and approval ownership in `.sam-harness/DELEGATION.md` |
| 14 | Adopt controls by maturity and risk | Profile recommendation and downgrade acceptance in `internal/planner/planner.go` |
| 15 | Learn by running laboratories | Stack fixture contract in `testdata/fixtures/typescript/package.json` |
| 16 | Use reusable playbooks and rubrics | Generated promotion rubric in `.sam-harness/GATES.md` |
| 17 | Preserve design consistency and finish for humans | Visual, accessibility, localization, and human-label gates in `.sam-harness/UX_GATES.md` |
| 18 | Coordinate enterprise use cases through a control plane | Ownership and cross-system controls in `skills/sam-harness/references/enterprise.md` |
| 19 | Govern the agentic stack lifecycle | Versioned upgrade planning in `internal/cli/cli.go` |
| 20 | Roll out, operate, and retire deliberately | Cutover, exit, and retirement guidance in `skills/sam-harness/references/enterprise.md` |
