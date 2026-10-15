---
title: Jev 1.14 request-shaping aggregates
type: public-release-results
experiment: request-shaping
model: jev-1.14.0
comparison_model: jev-1.13.0
author: Kyle Wild
---

# Jev 1.14 request-shaping aggregates

[`summary.json`](summary.json) holds source-free totals for Jev 1.14 (`jev-1.14.0`) and Jev 1.13 (`jev-1.13.0`) on the same planned requests in all three request-shaping studies. It lists its private source file with its SHA-256 (`sources`). It contains numbers and machine identifiers only. It has no passage or query text, request bodies, receipts or raw provider responses.

Under `studies.<study>` (`state-packing`, `question-fanout`, `state-position`):

- `jev_1_14`, `jev_1_13`: successful requests, attempts by HTTP status, response `model` ids, reported input and output tokens, list-price USD at $0.042 per million input tokens (output free), and request round-trip latency (p50, p95, mean, in seconds).
- `paired_jev_1_14_minus_jev_1_13`: every answered question, scored and co-asked alike, paired with the other version's answer to the same planned request. It gives the mean Δp, mean |Δp|, the share with |Δp| > 0.05, the share of identical probabilities, and the share of pairs on opposite sides of 0.5.

| Study | Pairs | Mean Δp | Mean \|Δp\| | \|Δp\| > 0.05 | Identical | Cross 0.5 |
|---|---:|---:|---:|---:|---:|---:|
| State packing | 183,256 | −0.0108 | 0.0396 | 24.5% | 13.9% | 3.53% |
| Question fan-out | 186,000 | −0.0047 | 0.0316 | 20.5% | 24.0% | 3.23% |
| Position in state | 32,454 | −0.0093 | 0.0451 | 30.4% | 14.8% | 5.65% |

Every Jev 1.14 response reported `jev-1.14.0`, and both versions billed identical input tokens on every study. Of 100,699 benchmark attempts, 27 failed (25 HTTP 529 and 2 HTTP 499 `internal_error`), and each of those requests succeeded on a later attempt. The two versions' latencies come from separate collection days, so they are descriptive rather than a controlled speed comparison.

The per-arm reports are [state packing](../state-packing-jev-1.14.0/REPORT.md), [question fan-out](../question-fanout-jev-1.14.0/REPORT.md) and [position in state](../state-position-jev-1.14.0/REPORT.md). Each one places Jev 1.13 columns beside the Jev 1.14 estimates. Intervals there are 95% query-cluster bootstrap intervals stratified by year. Costs are list-price estimates from reported usage, not invoices.
