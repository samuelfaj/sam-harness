# Observability and release observation

Observation window: until the release assets, checksums, signature bundle, provenance and clean installation are verified

## Health checks

- Health `verify the promoted release remains public` in `.` (120s): `'sh' '-c' 'test -n "${SAM_HARNESS_RELEASE_TAG:-}" && gh release view "$SAM_HARNESS_RELEASE_TAG" --json isDraft --jq '"'"'select(.isDraft == false) | .isDraft'"'"''`

## Observation checks

- Observation `verify release assets remain available` in `.` (120s): `'sh' '-c' 'test -n "${SAM_HARNESS_RELEASE_TAG:-}" && test "$(gh release view "$SAM_HARNESS_RELEASE_TAG" --json assets --jq '"'"'.assets | length'"'"')" -gt 0'`

Keep technical and business signals separate. Record unavailable evidence as unavailable, stop promotion on a failed required check, and do not shorten the configured observation window without a new approved plan.
