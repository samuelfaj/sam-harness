# Evidence and authority

## Evidence ladder

Keep these states separate:

1. Source contains the intended change.
2. Local checks passed against that source state.
3. A commit contains the checked source.
4. The expected remote branch contains that commit.
5. Review findings and approvals refer to that commit.
6. Required CI jobs passed for that commit.
7. An immutable artifact was built from that CI state.
8. The target environment reports that artifact digest.
9. Live technical and business signals stayed healthy for the observation window.

Never use one receipt to claim a later state.

## Authority

Capability is not permission. Read `.sam-harness/DELEGATION.md` before using the network, creating commits, pushing, publishing a release, deploying, modifying credentials, or performing an irreversible operation.

Ask immediately before a new authority boundary. Approval for repository files does not grant remote or production authority.

## Failures and recovery

Stop on missing evidence, scope drift, stale plans, ambiguous destructive targets, or required gate failures. Preserve the real error and the relevant receipt. Prefer reversible operations. A code rollback is not a data rollback; check compatibility and recovery state first.

## Delegation

Give delegated work an exact path scope, expected output, allowed tools, checks, and stopping condition. Verify the returned artifacts yourself. A worker's summary is not proof.
