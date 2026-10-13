---
title: SubQ Snap adapter for Jev-compatible requests
type: public-release-documentation
experiment: subq-adapter
author: Kyle Wild
---

# SubQ Snap adapter

SubQ Snap (`subq-snap-preview`) exposes a Jev-compatible decision endpoint at `POST https://api.subq.ai/v1/systemone`. The adapter preserves each experiment's state, typed questions, criteria, and order; it sets the top-level model name and records both the reference-body hash and the exact sent-body hash.

Supported question types are `noul`, `choice`, and `score`. The client validates documented request limits before sending, strictly checks one typed answer per question, keeps refusals and malformed responses visible, records every attempt, and retries only transport errors, HTTP 408/429, and 5xx responses. Authorization values and cookies are excluded from receipts.

The service also enforces an observed maximum of 16 questions per request, returning HTTP 422 above it. The adapter applies that limit before sending; benchmark arms above it must be reported as excluded rather than split into a different protocol.

## Configuration

- `SUBQ_API_KEY`: bearer token, read from the environment only.
- `SUBQ_URL`: endpoint; defaults to `https://api.subq.ai/v1/systemone` in the public adapter.
- `SUBQ_MODEL`: model name; defaults to `subq-snap-preview`.
- `SUBQ_WORKERS` and `SUBQ_MAX_ATTEMPTS`: concurrency and retry bounds.
- `SUBQ_USD_PER_MTOK_INPUT`, `SUBQ_USD_PER_MTOK_CACHED_INPUT`, and `SUBQ_USD_PER_MTOK_OUTPUT`: accounting rates.

SubQ Snap's [published list price](https://docs.subq.ai/using-the-api/models/) is **$0.05 per million input tokens, with output free**. Cached input has no separately published discount and is priced at the input rate. Dollar values are list-price estimates rather than invoices.

`go test ./...` exercises request preservation, validation, response parsing, retry histories, receipt redaction, cost accounting, endpoint behavior, and a deterministic local fake. `go run ./cmd/subq-probe --smoke` checks a real account with one small three-question request and incurs a small provider charge.

SubQ documents indefinite organization retention by default. Confirm the account's data-retention terms before sending private or source-bearing material. The public TREC and synthetic studies in this repository do not imply that private-data use is appropriate.
