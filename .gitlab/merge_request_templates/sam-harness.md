<!-- sam-harness:start -->
## Sam Harness Merge request evidence

Do not mark an item complete without a receipt tied to this exact change.

### Evidence ladder

- [ ] Source: intended paths and diff are identified.
- [ ] Local checks: required static and test commands passed; every waiver is linked and justified.
- [ ] Commit: the reviewed commit SHA contains the change.
- [ ] Remote: the expected remote branch contains that SHA.
- [ ] Review: independent findings and approvals belong to the same SHA.
- [ ] CI: required status checks passed for that SHA; branch protection and merge queue or merge-request approval rules were read back from the provider.
- [ ] Artifact: immutable digest, SBOM, and provenance came from the same CI run.
- [ ] Deployment: staging and production report that exact digest after required environment approval.
- [ ] Live proof: technical and business signals stayed healthy for the full observation window.

### Human-facing and UX checks

- [ ] Not applicable, with the affected surface and reason recorded; or all applicable checks below are complete.
- [ ] Loading, empty, error, success, unavailable, and destructive states were exercised.
- [ ] Keyboard access, focus, contrast, responsive layout, and reduced motion were checked.
- [ ] Visible labels use human names rather than internal IDs; dates, money, permissions, and status match user context.
- [ ] Localization and accessible names were verified for every affected locale.
- [ ] Browser or device evidence shows the changed states at relevant widths.

Provider YAML declares jobs and environment boundaries. It does not prove remote protection, approval, or required-status settings.
<!-- sam-harness:end -->
