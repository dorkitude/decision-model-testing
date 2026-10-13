---
title: SubQ reranking aggregates
type: public-release-results
---

# SubQ reranking aggregates

## TL;DR

SubQ produced the best relevance ranking in this 97-query TREC DL comparison: its expected-grade score reached 0.7206 nDCG@10, ahead of Decisions at 0.7006, Qwen3 Reranker 8B at 0.6994, and Jev at 0.6917. Against Jev, SubQ's expected-grade gain was +0.0289 with a 95% interval of [+0.0124, +0.0470]; its +0.0212 interval against Qwen narrowly included zero. Expected grade was the strongest frozen scoring formulation for every decision-model provider, while probability was weakest. Decisions roughly matched Qwen and Jev on the primary arms, and 23 refusals hurt one exploratory ordinal prompt. SubQ also had the lowest estimated list cost for its three primary arms, at $0.46 total versus $0.62 for Jev, $1.28 for Decisions, and $0.33 for Qwen. The benchmark is small, public, and potentially present in model training; prompt variants were inspected on the same queries, so the exploratory best-prompt results are not held-out validation.

[`summary.json`](summary.json) contains source-free aggregate metrics for SubQ's three primary scoring methods and nine exploratory prompt variants on the 97 TREC DL queries. It includes paired query-bootstrap intervals against Qwen and Jev, request counts, latency, token usage and list-price cost estimates.

The three primary arms completed 29,100/29,100 requests on the first attempt. Across all twelve arms, 116,400/116,400 requests succeeded on the first attempt. Source-bearing requests, passage text and raw receipts are excluded from this public distribution.
