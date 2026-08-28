# Migration and retirement

1. Inventory producers, consumers, stored data, credentials, schedules, and rollback dependencies.
2. Introduce versioned compatibility before cutover and keep migration commands resumable and idempotent.
3. Reconcile records and invariants, verify deletion propagation and restore, then collect explicit owner acceptance.
4. Remove the old path only after the observation window and rollback boundary close. Revoke credentials and archive evidence according to policy.

## Executable controls

- Migration `prove backward-compatible configuration migration` in `.` (600s): `'go' 'test' './internal/config' '-run' 'Backward|Workflow'`
- Rollback `withdraw a failed GitHub release` in `.` (300s): `'sh' '-c' 'test -n "${SAM_HARNESS_RELEASE_TAG:-}" && gh release edit "$SAM_HARNESS_RELEASE_TAG" --draft'`
- Release schedule: `0 15 * * 3` in `UTC`
