# Bounded correction

Correction is opt-in. Every enabled correction command must carry `filesystem_sandboxed: true`; provider-secret repair also requires `trusted_external_command: true`. `trusted_config_arguments` contains actual zero-based argv positions, never index 0, for safe relative helper paths that runtime resolves from the trusted config directory rather than the target checkout. Sam-harness does not OS-sandbox arbitrary argv. It receives a failed receipt on stdin; failed review receipts must contain an intact, conflict-free repair manifest. The repair applies every manifest action in one coherent correction, may write only inside its sandboxed local workspace, must stay inside cumulative file and line budgets measured from the frozen baseline, and must rerun static and test phases after every attempt. Independent re-review remains required. Provider credentials and remote authority remain read-only until a separate trusted publisher boundary.

- Command: `'npx' '--yes' '@openai/codex@0.150.1' 'exec' '--sandbox' 'workspace-write' '--ephemeral' '-'`
- Maximum attempts: 2
- Maximum cumulative changed files from the frozen baseline: 20
- Maximum cumulative changed lines from the frozen baseline: 1000
- Filesystem-sandboxed command attestation: true
- Trusted-external-command attestation: true
- Trusted-config argv positions: []
- Isolated branch prefix: `sam-harness/repair/`
- Open a change request: false

The repair command may change the local workspace inside the cumulative budget, but provider credentials and remote authority remain read-only. Secret-bearing CI repair runs separately from the failed phase on a clean runner with the protected agent environment, pinned released harness, and trusted base configuration; it never inherits repository setup commands. Missing protected secrets fail closed. Runtime emits the validated correction-only patch plus receipts; CI must transport that exact patch without recomputing it from the surrounding worktree. A separate trusted publisher receives no agent secret and may apply exactly one verified patch/receipt pair only on the isolated prefixed branch; it disables repository hooks and never pushes directly to a protected branch. A pull or merge request may be opened only when the configuration explicitly enables it and remote authority permits the required commit, push, and network operations.
