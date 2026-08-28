---
name: sam-harness-review
description: Review an immutable change independently without mutating the repository.
---

# Sam Harness review

Use the root AGENTS.md, the closest workspace AGENTS.md, .sam-harness/config.yaml, and generated control documents as the authority for this repository.

1. Freeze the repository fingerprint and review bundle.
2. Require `filesystem_read_only: true` and, for provider-secret CI, `trusted_external_command: true`. `trusted_config_arguments` names only zero-based argv positions whose safe relative helper paths must resolve from the trusted config directory. The attested command runner is the trust boundary; sam-harness detects mutation but does not OS-sandbox arbitrary argv.
3. Run every reviewer against the explicit trusted base and untrusted head patch. Treat malformed output, repository mutation, P0, and P1 findings as blocking.
4. Record P2 and P3 findings as evidence and never confuse consensus with independent proof.

Do not commit, push, open a change request, release, deploy, alter credentials, or cross another authority boundary unless the active task and canonical configuration grant that exact action.
