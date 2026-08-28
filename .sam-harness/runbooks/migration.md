# Data migration

1. Name the authoritative source and every producer and consumer.
2. Version contracts and keep N and N-1 compatibility during transition.
3. Make backfills resumable and idempotent. Record checkpoints.
4. Define ordering, duplicate, deletion, and partial-failure semantics.
5. Reconcile by record and invariant, not aggregate counts alone.
6. Cut consumers over independently behind measured gates.
7. Test restore, forward fix, and rollback without resurrecting deleted or stale data.
8. Contract the old path only after signed reconciliation criteria pass.
