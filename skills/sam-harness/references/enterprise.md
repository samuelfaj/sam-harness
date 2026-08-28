# Enterprise controls

## Multi-service and control plane

Keep a catalog of use cases, owners, repositories, data classes, providers, models, permissions, gates, evidence, and retirement status. Central policy may set minimums, but each repository keeps executable commands and domain invariants close to the code.

## Data and knowledge migrations

Name the authoritative record and every producer and consumer. Version contracts, capture live writes, define ordering and duplicate behavior, make backfills resumable, propagate deletions, reconcile per record and invariant, and test recovery under partial failure.

Keep canonical content separate from rebuildable indexes, embeddings, caches, and summaries. Record lineage, freshness, schema and model versions, retention, tombstones, and provenance for agent-generated data. Compare retrieval behavior before provider or model cutover.

## Provider and model lifecycle

Pin versions for evaluated paths. Use shadow or canary evaluation before promotion. Track quality, latency, cost, safety, and business signals. Preserve a tested exit path and retire stale providers, prompts, credentials, indexes, and policies deliberately.

## Operations and retirement

Set health gates and owners before rollout. Exercise rollback, forward fix, restore, and incident handling. Retirement requires consumer inventory, data disposition, credential revocation, evidence retention, and confirmation that stale paths cannot be reactivated accidentally.
