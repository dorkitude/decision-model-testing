---
title: Reproduction and release scope
type: public-release-documentation
---

# Reproduction

Use Go 1.27.1 or later. Run `go test ./...` and `go vet ./...` in each experiment directory (six Go modules, including the OpenAI Decisions adapter). The public overlays skip six historical receipt tests; three public-data integration tests run after reranker `prepare`; seven request-shaping tests run after its `prepare`, three of which also need private frozen receipts and keep skipping; all remaining tests use synthetic fixtures; historical receipt replay cannot run from this distribution. Downloaded datasets and provider receipts must stay ignored and local. The safe aggregate JSON files are derived projections, with the source file's SHA-256; they cannot reconstruct original text or support full receipt-level audit.

Run `sh scripts/bootstrap-dependencies.sh` from this repository root to clone pinned public dependencies locally. This is a network operation, not inference. It does not restore private experiment history or evidence. The eval harness's `prepare` downloads pinned benchmark sources locally; reranker `prepare` downloads pinned candidates and qrels.

RAG replication additionally requires uv, Fireworks, TypeSafe and Turbopuffer credentials; Claude requires Claude Code authenticated with Max. Fetch the pinned EnronQA source using the CLI, build a private dense index with 1,024-token chunks/204 overlap, and configure a matching lexical index. Use fresh namespaces in the exported configs; the original researcher's indexes are not dependencies available to readers. The Kimi `prepare` creates the lexical index; Claude validates existing dense/lexical indexes. Dense-index construction is an external prerequisite and is not implemented by these harnesses or EnronQA CLI. Embed chunks with `fireworks/qwen3-embedding-8b`, normalize each vector, and store 4,096-dimensional vectors in your own Turbopuffer namespace. Match IDs exactly: `sha256("chunks1024" + document_id + decimal_start_byte + original_chunk_text)`. Reconstruct chunks using `unitsFor` in `prepare.go`; the namespace must contain all 84,375 chunks. EnronQA CLI only fetches/exports data; it does not create this index. Consequently fresh RAG replication is not a single fetch-and-run command.

For the Claude sample, locally export Kimi's seeded 100 question IDs to `data/excluded-questions.jsonl` before `prepare` (seed 20260918; Claude uses seed 20260919). Export with `EnronQA-cli`'s `questions export --set test --limit 100 --seed 20260918`; no question text is bundled here. The resulting ID exclusions select the same disjoint sample from the pinned dataset. New source/config hashes necessarily differ from frozen historical runs.

Provider-backed `run`, `preflight` and indexing commands cost money. Do not run them for offline validation. Use new output directories for replications; never overwrite reported measurements. Provider versions and service access can change. The numerical reports are historical observations, not guaranteed results.

## OpenAI Decisions re-runs

Every experiment was re-run on 2026-10-06 against OpenAI's [Decisions API](https://developers.openai.com/api/docs/guides/decisions) (`POST /v1/decisions`, model `gpt-6-luna`, public beta, wire format `guide-2026-10-06`) by sending each experiment's frozen Jev request bodies through the [Decisions adapter](experiments/openai-decisions-adapter/README.md). The adapter is public and fully tested offline (`go test ./...` in its folder, no credentials). The per-experiment wiring that plugged it into each harness, with its receipts, resume and budget handling, will be released in a later export; until then this distribution publishes the Decisions results as aggregates and generated reports only, and a reader can reproduce a Decisions arm by calling `decisions.Client.Do` with the request bodies the public harnesses build.

- **Access and credentials.** Export `OPENAI_API_KEY` (or `OPENAI_DECISIONS_KEY`) and use `https://api.openai.com/v1/decisions`. `go run ./cmd/decisions-probe` in the adapter folder checks access for free; `--smoke` sends one tiny paid request. Keys are read from the environment only and never written to receipts.
- **Costs.** $0.10 per million input tokens; output, cache reads and cache writes are not billed. Regional-processing and long-context multipliers mentioned in the guide were not modeled. List-price estimates of the published re-runs: rerankers $5.30 (twelve arms), eval judging $2.84 (plus $0.63 of reused frozen helper calls), Kimi RAG filter $0.18, Claude RAG filters $0.19 and $0.20 plus $0.02 rescoring, request shaping $19.98. Answering models (Kimi K3, Claude Opus 5) and the DeepSeek re-judges are billed separately by their providers.
- **Refusal policy.** Decisions may answer a question with `{"type": "refusal"}`. A refusal is a non-answer: it is recorded, never retried and never given a value. Rerankers fall back to BM25 order for the affected query; eval judging scores a refused required stage as wrong; the RAG filters drop the chunk; request-shaping reports exclude the decision from every metric and tag affected rows. Every README reports refusal counts.
- **Pacing.** The larger runs paced requests client-side (120 to 3,000 requests per minute depending on the experiment). Median request latency was about 0.12 s; job and query wall times in the reports are dominated by pacing and are not model speed.
- **Judges.** The frozen DeepSeek judge model used by the RAG experiments has been retired by its host, so the RAG re-runs judged all arms with `deepseek-v4p1-flash` and the frozen judge prompt. Use a new output directory for any re-run and never overwrite the frozen results.

## Publication boundary

This is a fresh, allowlisted export. The original repository, issues, releases, evidence archives and Git history remain private. Do not change their visibility or mirror them. A new public repository should receive only this audited tree, after reviewing `EXPORT_MANIFEST.json`. No source-bearing evidence is redistributed by this policy; it does not assert dataset redistribution rights.

## Local EnronQA acquisition

From either RAG experiment directory after dependency bootstrap:

```sh
uv run --project EnronQA-cli --frozen enronqa fetch --data-dir ../../.local/enronqa
```

For Claude's excluded sample, create the local file before preparing the corpus:

```sh
mkdir -p data
uv run --project EnronQA-cli --frozen enronqa questions export \
  --set test --limit 100 --seed 20260918 --data-dir ../../.local/enronqa \
  > data/excluded-questions.jsonl
```

These commands acquire source material directly from upstream for local use. They do not build the required dense index or run inference. Both `data/` and `.local/` are ignored. Review upstream terms before use.
