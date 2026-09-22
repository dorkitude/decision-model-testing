---
title: jev-vs-LLM-for-evals
type: public-release-documentation
---

# Jev vs. LLM judges

Compare benchmark-label agreement, service failures, latency, tokens, estimated cost and selective routing on LLMBar, JudgeBench and RewardBench 2. Agreement with benchmark labels is not independently verified correctness. Native Jev interfaces and established LLM judging protocols are distinguished in the Go harness.

[Aggregate measurements](results/primary/) retain numerical quality, reliability, native-interface comparisons and rating sensitivity. Benchmark examples, submitted answers, frozen request receipts and full archives are not bundled. The private originals remain unchanged; numerical projections cannot substitute for receipt-level audit.

`go run ./cmd/evalevaluation --help` lists the CLI. `go run ./cmd/evalevaluation prepare` acquires pinned upstream sources into the local ignored cache with hash verification. Configs and runtime third-party adapters are included with their licenses. Provider-backed campaign commands incur costs; use a new output directory. The official-data replay integration test requires private historical fixtures and is skipped in this source-free distribution.

## Reproduce

See [shared setup and limits](../../REPRODUCTION.md). From this directory, build with `go build ./...`, run synthetic checks with `go test ./...`, and inspect CLI help before any paid run. Numerical reports are under `results/`. No historical evidence download is required or available in this distribution.

## Final service-retry comparison

| Judge | LLMBar Vanilla | JudgeBench vanilla | RewardBench 2 ratings |
|---|---:|---:|---:|
| Jev | 87.36% | 69.03% | 68.12% |
| GPT-OSS 120B | 87.05% | 77.10% | 79.56% |
| DeepSeek V4 Flash | 88.73% | 75.81% | 65.99% |
| GLM 5p3 | 91.18% | 74.19% | 27.59% |
| Qwen3p8 Max | 92.53% | 88.39% | 81.57% |

These final scores include the separately reported service-error retries, charging both passes. Required-stage failures score zero. LLMBar/RewardBench average subsets equally; JudgeBench pools cases. Compare within a column. [Retry results](results/primary-recovery-analysis-v1/service-recovery.json) are separate from the original-run metrics.

An offline Jev → GPT-OSS cascade on 513 held-out JudgeBench cases scored 80.12% versus 76.41% for GPT-OSS alone, with 43.69% lower accounting cost. The paired gain was 3.70 points (95% interval 1.17–6.43). This simulation uses original unrecovered outputs and development-selected thresholds; timing adds separately measured calls. Other pairings lost quality: the LLMBar → Qwen cascade lost 3.52 points. [All cascade measurements](results/cascade-evaluation-v1/cascades.json). Prices are dated list estimates, not invoices; intervals are unadjusted for multiple comparisons.
