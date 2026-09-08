# Bruiser audit events

Default retention **13 months** (`BRUISER_AUDIT_RETENTION`). Export:
`GET /v1/admin/export` (JSONL). Purge writes `AUDIT_PURGED`.

| Type | When |
|------|------|
| `EXECUTION_GRANTED` | New lease (`reason=granted` or `promoted`) |
| `EXECUTION_REQUESTED` | Same-principal re-acquire (`already_held`) |
| `EXECUTION_RENEWED` | Heartbeat or resume |
| `EXECUTION_BUSY` | Domain at `max_active` and queue full or disabled |
| `EXECUTION_QUEUED` | Intra-customer waiter accepted |
| `EXECUTION_DEQUEUED` | Waiter left the queue |
| `EXECUTION_QUEUE_EXPIRED` | Waiter TTL elapsed |
| `EXECUTION_RELEASED` | Holder released |
| `EXECUTION_EXPIRED` | Sweeper materialised expiry |
| `EXECUTION_REVOKED` | Customer or admin revoke |
| `EXECUTION_HANDED_OFF` | Preempt / cooperative handoff |
| `AUTHORITY_CHECK` | Last go-live gate report |
| `AUDIT_PURGED` | Retention job |
| `POLICY_ACTIVATED` | `PUT /v1/policy` |

Every row is tenant-scoped (`merchant_id`). Events answer who (customer +
principal), which execution, which rule, fence, and request id. No PII
beyond the merchant's own customer identifier.
