# Maturity profiles

## Baseline

Use for libraries, prototypes, and repositories without production delivery or persistent data. Baseline still requires repository instructions, scoped context, explicit authority, deterministic local checks, evidence rules, and user-facing quality gates when applicable. It may omit remote delivery controls. If it configures reviewers or enabled correction, the same reviewer `filesystem_read_only` and correction `filesystem_sandboxed` attestations still apply.

## Production

Use when the system is deployed, changes persistent data, or affects a live service. Planning blocks until every static/test guard category has exactly one command or waiver; managed CI has explicit scoped secret-name bindings or a provider waiver, a named protected agent environment for any binding, and a matching agent control plane for every review/repair binding; every reviewer has a verified read-only-filesystem attestation; enabled correction has a verified filesystem-sandbox attestation; provider-secret review/repair has verified trusted-external-command attestations and indexed trusted-config helpers; and the six reviewer commands, correction policy, immutable artifact/SBOM/provenance commands, staging/production/manual-rollback commands, health and observation checks, canary percentages, migration commands, and release schedule are explicit. GitHub's dedicated App, protected environment, required head check, human approval/prevent-self-review rules, external merge-group-to-repository-dispatch handler, and merge-queue rules—or GitLab's external trusted project, required current-head status, protected variables/environment, and MR rules—must be configured and read back before a secret-bearing pre-merge gate is claimed. Ordinary PR/MR CI remains credential-free.

## Regulated

Use for regulated data, controlled environments, or high-criticality irreversible actions. Retain the complete production workflow and add a threat model, data lineage and retention, separated approval duties, audit evidence, tested recovery, and incident exercises. The profile does not certify legal or regulatory compliance.

## Downgrades

If the user selects a profile below the recommendation, record the reason in `risk_acceptance`. Explain the missing controls before applying. Do not silently weaken the recommendation.
