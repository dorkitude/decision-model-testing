---
title: Jev 1.14 reranking aggregates
type: public-release-results
model: jev-1.14.0
---

# Jev 1.14 reranking aggregates

[`summary.json`](summary.json) contains source-free aggregate metrics for Jev 1.14 (`jev-1.14.0`) on the three primary scoring methods and nine exploratory prompt variants over the 97 TREC DL queries. It holds per-arm, per-year nDCG@10, MRR@10, recall and tie fractions; paired query-bootstrap intervals against Qwen, against each family's original prompt, and against the Jev 1.13 (`jev-1.13.0`) arm with the same prompt; and request counts, retries, HTTP status counts, response model ids, token usage, list-price cost and latency for both Jev versions.

Fields under `main` (three primary arms) and `prompt_study` (all twelve Jev arms):

- `aggregates`, `resources`, `paired_vs_qwen`, `paired_vs_original_jev`: Jev 1.14 metrics per `method/stratum` (`all`, `dl19`, `dl20`), with Qwen, BM25 and the label oracle alongside.
- `versus_jev_1_13.paired_jev_1_14_minus_jev_1_13`: per-arm, per-stratum differences in nDCG@10 with 95% bootstrap bounds (queries resampled within year, 10,000 replicates, seed 20260918), plus both versions' nDCG@10, MRR@10, recall@10 and top-10 tie fractions.
- `versus_jev_1_13.requests_jev_1_14` / `requests_jev_1_13`: requests, retries, attempt and status counts, response model ids, input/output tokens, list-price USD and request round-trip p50/p95/mean in seconds. `query_wall` holds query wall-time p50/p95 and failed jobs for both versions.

All 29,100 primary-arm and 116,400 total Jev 1.14 requests returned HTTP 200 on the first attempt, and every response reported `jev-1.14.0`. Both Jev versions billed the same input tokens per arm. Jev 1.13 and Jev 1.14 timings come from separate collection days and are descriptive. Request bodies, passage and query text, and raw receipts are excluded from this public distribution.
