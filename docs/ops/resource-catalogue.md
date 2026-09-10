# Closed resource catalogue

Canonicalisation is unchanged. The catalogue is **defence in depth** on
top of folding. The external retest confirmed that NFKC / lookalike /
separator folding already defeats the demonstrated domain-splitting
attack. A closed list stops unknown IDs after that fold.

## How a merchant supplies IDs

1. Inventory every scarce hold/purchase identifier the origin already uses
   (event codes, SKUs, fixture keys). Use **placeholders in git**.
2. Write them as canonical `type:ident` strings in the merchant profile:

   ```yaml
   resources:
     - event:fixture-1
     - ticket:home-opener
   ```

3. Prefixes on routes (`resource_prefix: "event:"` + `resource_from: event_id`)
   must produce one of those IDs after folding.
4. Validate: `bruiser config validate configs/your-profile.yaml`.
   Malformed catalogue entries are `FAIL`. An empty list is `WARN`.
5. Deploy the profile via `BRUISER_PROFILE` (ConfigMap / file mount). The
   list is auditable YAML — not a hidden API.

Example files (placeholders only):

- [configs/production.example.yaml](../../configs/production.example.yaml)
- [configs/production-catalogue.example.yaml](../../configs/production-catalogue.example.yaml)

Do not copy real club SKUs into the repository.

## Grammar (enforced)

After folding, an ID must match:

```
type ":" ident
type  = [a-z][a-z0-9]*
ident = [a-z0-9]+(-[a-z0-9]+)*
```

Folding (before the allowlist): NFKC, dash/space/quote lookalikes, strip
leading/trailing non-alphanumeric runs, collapse separator-class
punctuation (`/:;,` and spaces) to `:`, then `[a-z0-9:-]` only.

Malformed inbound strings are rejected (`malformed resource identifier`).
Lookalike / separator / Unicode variants of a registered ID **do not**
create a second execution domain — they fold onto the same key.

The nine-variant corpus `ticket:cupfinal` + `. - _ ! @` + spaces + U+2010
+ U+2013 folds to one domain. Authority Check still runs that corpus.

## Closed vs empty

| `resources:` | Behaviour |
|--------------|-----------|
| omitted / empty | Fold only. Any grammatical ID is a domain key. Lab / ad-hoc. |
| one or more IDs | Fold, then require a registered ID. Unknown → `unknown resource` (rejected). |

Production **should** ship a closed list. Config validate WARNs when it is
empty. It does not hard-fail: some merchants template IDs at runtime. If
you template, every value that can reach acquire/authorize must still fold
onto a listed ID, or the catalogue will reject it.

## Auditable configuration

- Catalogue lives in the merchant profile YAML (versioned with the
  deployment).
- `bruiser config validate` prints each bad `resources[i]`.
- Policy writes remain exactly one `POLICY_CHANGED` audit event; catalogue
  changes ride with the profile reload / redeploy, not a separate product
  API.
- Runtime domain keys always pass through `resource.Canonical` /
  `Catalogue.Canonical`. Do not change that.
