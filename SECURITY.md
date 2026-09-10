# Security policy

## Reporting a vulnerability

Do **not** file a public GitHub issue for a security defect.

Use GitHub private vulnerability reporting:

https://github.com/Nareik33L/bruiser-gateway/security/advisories/new

If that form is unavailable, email the repository owner through the GitHub
profile associated with this repo and say the message is a security report.

**SLA**

| Step | Target |
|------|--------|
| Acknowledge receipt | 3 business days |
| Initial severity / next-step update | 10 business days |
| Fix or coordinated disclosure plan | as soon as a patch can be verified |

Please include reproduction steps, affected version / commit, and impact
(especially anything that grants a second execution to one customer).

## Supported versions

`main` and the current RC1 release line. Lab / demo binaries are not supported
as production artifacts.

## Release integrity

Tagged releases (`v*`) produce:

- checksummed binaries (`bin/bruiser`, `bin/simtix`)
- a container image
- **cosign** keyless signatures (Fulcio / Rekor)
- **SLSA-style** GitHub build attestations (`actions/attest-build-provenance`)

Verify a blob:

```bash
cosign verify-blob --bundle bruiser.cosign.bundle \
  --certificate-identity-regexp 'https://github.com/Nareik33L/bruiser-gateway/' \
  --certificate-oidc-issuer https://token.actions.githubusercontent.com \
  bruiser
```

The origin / Embedded SDK should pin a verified image digest and refuse
unsigned builds in production.

## External pentest

A third-party penetration test is **required before go-live**. Track findings
to close (or accept with an owner) before raising enforcement above observe.
The tester should be given the failure-mode suite (`internal/failuremode`) and
the Authority Check as a starting pack, not as the whole engagement.

## Secrets

Production refuses empty values and the well-known lab placeholders
(`change-me*`, `*-secret-dev`). Generate unique admin, operator, edge, and
origin secrets. Never commit them. CI (`scripts/check-secrets.sh` + gitleaks)
fails if the lab values reappear outside allowlisted lab sources.

## Branch protection

`main` must not accept direct pushes. Merges require:

1. a pull request
2. at least one approving review
3. green required checks (`test`, `govulncheck`, `gitleaks`, `sbom`, `image`)

Apply via the GitHub ruleset / branch-protection UI if the API token cannot
write repository settings. Admins should not bypass these rules.
