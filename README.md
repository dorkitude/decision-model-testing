---
title: Decision Model Revolution
type: public-release-documentation
---

# Decision Model Revolution

Experiments testing specialized decision models against LLM and retrieval baselines. Each experiment is a self-contained subfolder of `experiments/`:

- [Jev vs. LLM judges](experiments/jev-vs-LLM-for-evals/README.md): benchmark-label agreement, failures, resources, and selective routing.
- [Kimi RAG filtering](experiments/jev-search-result-narrower/README.md): can Jev reduce answering costs?
- [Claude RAG filtering](experiments/jev-search-result-narrower-claude-RAG/README.md): probability and categorical filtering before Claude.
- [Jev vs. rerankers](experiments/jev-vs-rerankers/README.md): passage ordering against human relevance judgments.

This distribution contains code, original prompts, synthetic tests and aggregate measurements. Datasets, source-bearing answers/receipts, archives, private correspondence and prior Git history are excluded. See [reproduction and limits](REPRODUCTION.md) and [third-party notices](THIRD_PARTY_NOTICES.md).
