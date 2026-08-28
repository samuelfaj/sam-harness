# Release and rollback

Rollback owner: Samuel Fajreldines
Observation window: until the release assets, checksums, signature bundle, provenance and clean installation are verified

1. Freeze the reviewed commit and produce one immutable artifact.
2. Record the artifact digest, SBOM, provenance, test receipts, and approvals.
3. Package the configured paths once so relative paths and executable modes survive transfer, then promote the same digest between environments. Never rebuild for production.
4. Use a canary or staged rollout calibrated to traffic, risk, and response capacity.
5. Stop promotion when technical or business health gates fail.
6. Invoke rollback only through its explicit manual boundary and without running production first. Roll back code only when stored data remains compatible; otherwise use the tested forward-fix or recovery plan.
7. Keep the observation window open until the configured owner accepts the live evidence.
