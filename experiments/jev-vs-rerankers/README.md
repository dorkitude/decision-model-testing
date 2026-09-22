---
title: jev-vs-rerankers
type: public-release-documentation
---

# Jev vs. rerankers

Compare Qwen3 Reranker 8B and Jev probability, expected-grade and ordinal scores over the same 100 BM25 candidate passages for all 97 TREC DL 2019/2020 judged queries. Human qrels determine nDCG@10, MRR and recall; no answer-model judge is involved.

The nine-prompt exploratory follow-up found combined nDCG@10 of 0.6994 for Qwen, 0.6962 for explicit ordinal criteria and 0.6927 for the Noul utility statement. This small, reused sample does not establish equivalence. Aggregate JSON retains cost, timing, coverage and ranking metrics; source-bearing candidate text and receipts are omitted.

After dependency bootstrap, `go run . prepare` downloads upstream data without inference. `go run . run --study prompts --out results/new-prompts --max-cost 20` performs paid inference. `report` and `audit` operate on your own locally generated receipts. Whole-query timing depends on batching and concurrency.

## Reproduce

See [shared setup and limits](../../REPRODUCTION.md). From this directory, build with `go build ./...`, run synthetic checks with `go test ./...`, and inspect CLI help before any paid run. Numerical reports are under `results/`. No historical evidence download is required or available in this distribution.
