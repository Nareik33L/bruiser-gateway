# Release, licensing, and signing-key checklist

This document inventories what the repository already contains and what
only a human may do. It does **not** grant a licence, assert trademark
rights, or make legal claims on behalf of the founder.

The source repository is **not** published as open source by this work.
There is **no** public Open Source page.

## Repository status (as shipped)

| Item | Status |
|------|--------|
| `LICENSE` | **Absent.** Do not assume Apache-2.0, BSL, or any other grant. |
| Copyright notices | Source files generally do not carry a product copyright header. Adding them needs counsel. |
| Third-party notices | [NOTICE](../../NOTICE) — inventory of Go module licences, **not** a Bruiser licence. |
| Dependency / licence inventory | `go.mod` / `go.sum`; CI CycloneDX SBOM job (`sbom`). |
| Release metadata | Git tags `v*` drive `.github/workflows/release.yml`. Helm `Chart.yaml` `version: 0.1.1`, `appVersion: 0.1.0` (chart metadata ≠ product licence). |
| Versioning | Product identity for this freeze is git SHA on `main` (V1 canonical `fee0ff9…`). A `v1.0.0` tag requires founder approval. |
| Changelog | [CHANGELOG.md](../../CHANGELOG.md) |
| Security documentation | [SECURITY.md](../../SECURITY.md), [docs/security/threat-model.md](../security/threat-model.md), [docs/11-rc1.md](../11-rc1.md) |
| Signing / release documentation | Release workflow: checksummed binaries, **cosign keyless** (Fulcio / Rekor), GitHub build attestations. Verify commands in SECURITY.md. |

README “Licensing (intended, pending legal review)” is an **intention**,
not a licence.

## Proposed signing-key lifecycle

Do **not** generate or commit production private keys in this repository.

```
Generate → store securely → sign release → verify → rotate → revoke
```

| Step | What exists today | What is still human |
|------|-------------------|---------------------|
| Generate | CI uses **keyless** cosign (no long-lived private key in git). Optional merchant-held cosign key is **not** in tree. | If the merchant requires a held key: generate offline (founder / security). |
| Store securely | GitHub OIDC → Fulcio; Rekor log. | Held-key: HSM / vault. Access list. |
| Sign release | `cosign sign-blob` / `cosign sign` on tag `v*`. | Approve the tag. |
| Verify | `cosign verify-blob` with `certificate-identity-regexp` on this GitHub repo (see SECURITY.md). Pin image digest at origin. | Operator verify before promote. |
| Rotate | Keyless: Fulcio certs are ephemeral per build. Held-key: new key, dual-sign a transition release. | Founder / security. |
| Revoke | Keyless: compromise is a GitHub identity / workflow issue. Held-key: publish revoke material out of band. | Founder / security / incident. |

Image signing admission (Kyverno/Gatekeeper) is **not** shipped. Supply
your own.

## Requires founder approval

- Tagging `v1.0.0` (or any production marketing version)
- Publishing binaries/images outside the existing GHCR-on-tag workflow
- Changing the intended licence split (protocol/SDK vs gateway core)
- Any statement that Bruiser is “open source”
- Hosted / SaaS offering (explicitly out of V1)

## Requires legal counsel

- Drafting and adding `LICENSE` / `COPYING`
- Copyright headers on source
- Customer contract, DPA, SLA, warranty, indemnity
- Export / cryptography notices if counsel says they apply
- Confirming third-party NOTICE completeness

## Requires trademark review

- Use of “Bruiser”, logos, and club/partner names in commercial material
- The Arsenal-like lab profile is an **unverified analogue**, not an endorsement

## Requires external signing infrastructure

- Production cosign **held** keys (if keyless is unacceptable to the merchant)
- Admission controllers that refuse unsigned images
- Offline verification in air-gapped origins

## Requires production credential ownership (merchant)

- IdP client secrets, JWKS private keys (never Bruiser’s)
- `BRUISER_ORIGIN_SECRET` / `BRUISER_EDGE_SECRET` / admin / operator
- Postgres credentials and backups
- Staging JWT used with `--identity-token` (ephemeral operator secret)
- DNS, TLS certificates, edge configuration

## Requires human / GitHub admin

- Branch protection on `main` (no direct push, 1 review, required checks
  `test`, `govulncheck`, `gitleaks`, `sbom`, `image`) — API tokens may 403
- Third-party pentest **before go-live** (SECURITY.md)
- Hosted sandbox DNS, Stage A outbound (if those programmes exist)

## REQUIRES HUMAN / MERCHANT

Exact remaining human actions:

1. Own real staging/production secrets (never commit).
2. Point JWKS/issuer/audience at the merchant IdP; complete [staging-idp.md](staging-idp.md) evidence.
3. Replace placeholder catalogue IDs and routes with the merchant’s real (internal) list.
4. Deploy, `config validate`, `/readyz`, Authority Check `--require-certificate`.
5. Dry Run, then ramp 10/25/50/75/100; keep observation at 100%.
6. File the certificate JSON and IdP evidence in the merchant’s evidence store.
7. Counsel: licence, copyright, customer terms; **do not add LICENSE without that**.
8. Trademark review before public commercial use of the name.
9. Decide keyless vs held release-signing keys; do not generate held keys in git.
10. GitHub admin: enable branch protection if not already on.
11. External pentest closeout for the **staging topology** (lab retest does not replace it).
12. Founder: tag/release approval. Do not treat this PR as a public OSS launch.
