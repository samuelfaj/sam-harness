# Security policy

## Supported version

Security fixes are applied to the latest published release. Before reporting a problem, reproduce it against that version when doing so is safe.

## Report a vulnerability

Use GitHub private vulnerability reporting for `samuelfaj/sam-harness`. Do not open a public issue with exploit details, credentials, private repository content, or personal data.

Include the affected version, operating system, repository shape, exact command, observed result, expected boundary, and the smallest safe reproduction. Redact tokens, customer data, internal URLs, and unrelated files.

## Release verification

Each release publishes `checksums.txt`, a keyless Cosign signature and certificate for that file, CycloneDX SBOMs, and GitHub build provenance. The bootstrap scripts refuse installation when signature or checksum verification fails.
