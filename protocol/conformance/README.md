# Protocol conformance (skeleton)

The suite asserts the **protocol**, not a particular deployment method.

1. `GET /.well-known/bruiser/protocol` lists capabilities including
   `ACQUIRE`, `RENEW`, `QUEUE`, `AUTHORIZE`.
2. Acquire for a second principal of the same customer is `BUSY` unless
   `waiting.mode: bounded`, in which case it is `QUEUED` then `BUSY`.
3. Same principal before expiry is `ALREADY_HELD`.
4. Unmatched routes are `ALLOW` when `unmatched: allow`.

`go test ./protocol/conformance` hits a live gateway when
`BRUISER_CONFORMANCE_URL` is set; otherwise it checks the checked-in
OpenAPI document only.
