---
title: jev-search-result-narrower-claude-RAG
type: public-release-documentation
---

# Claude RAG filtering

Jev filters top20 reranked search results before Claude Opus 5 answers through Claude Code Max. The baseline gets all chunks; treatments keep probability ≥0.5 or categories `direct_evidence` and `supporting_evidence` (discard `irrelevant`). Categorical mode uses no numerical cutoff. Whole chunks are paged under 6,000 proxy tokens, 24,000 bytes and six decisions.

| Pipeline | DeepSeek accepted /100 | Estimated model cost | Mean time to final answer |
|---|---:|---:|---:|
| Unfiltered | 96 | $26.91 | 5.93 s |
| Jev threshold | 95 | $6.21 | 4.89 s |
| Jev categorical | 96 | $7.44 | 5.38 s |

Costs cover answering, filtering, embedding and reranking; exclude judging, indexing and infrastructure. Claude costs are API-equivalent estimates, not incremental Max charges. Timing begins at the first Claude invocation and includes CLI startup and intervening searches; it excludes initial retrieval/filtering and grading. Categorical comparison reuses historical controls on an inspected sample; outcomes do not establish superiority. Two question workers were used. The sample differs from Kimi's, so this is not a controlled model comparison.

## Reproduce

See [shared setup and limits](../../REPRODUCTION.md). From this directory, build with `go build ./...`, run synthetic checks with `go test ./...`, and inspect CLI help before any paid run. Numerical reports are under `results/`. No historical evidence download is required or available in this distribution.
