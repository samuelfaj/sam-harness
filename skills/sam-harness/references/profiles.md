# Maturity profiles

## Baseline

Use for libraries, prototypes, and repositories without production delivery or persistent data. Baseline still requires repository instructions, scoped context, explicit authority, deterministic local checks, review evidence, and user-facing quality gates when applicable.

## Production

Use when the system is deployed, changes persistent data, or affects a live service. Add CI, dependency and security checks, immutable artifact identity, SBOM, provenance, migration controls, rollout, rollback, health gates, and an owned observation window.

## Regulated

Use for regulated data, controlled environments, or high-criticality irreversible actions. Add a threat model, data lineage and retention, separated approval duties, audit evidence, tested recovery, and incident exercises. The profile does not certify legal or regulatory compliance.

## Downgrades

If the user selects a profile below the recommendation, record the reason in `risk_acceptance`. Explain the missing controls before applying. Do not silently weaken the recommendation.
