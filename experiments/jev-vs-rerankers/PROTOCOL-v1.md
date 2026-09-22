---
title: Reranker PROTOCOL-v1
type: public-release-documentation
---

This public replication implements rerank@100 on all 97 TREC DL 2019/2020 judged queries. BM25 candidates are fixed; Qwen and Jev score the same passages. Human qrels determine ranking metrics. The prompt study adds nine variants in `prompts.json`; it is exploratory on the same sample. Whole-query timing reflects batching and concurrency.

Original frozen receipts and source snapshots are retained privately. This public source tree creates new-run provenance and is not the frozen source used for the reported measurements. Download original datasets locally with `go run . prepare`; never commit their text or new receipts.
