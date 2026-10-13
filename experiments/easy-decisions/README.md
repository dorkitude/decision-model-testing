---
title: Easy deterministic decision controls
type: public-release-documentation
experiment: easy-decisions
author: Kyle Wild
---

# Easy deterministic decision controls

Can decision models reliably solve simple, objectively scored tasks when candidate display order, API option order, question order, or record position changes?

## TL;DR

Jev and OpenAI Decisions were perfectly accurate across all 880 exact-choice, lookup, and arithmetic requests, while SubQ was perfect on the 560 exact-choice and lookup requests but scored 312/320 on arithmetic. SubQ's eight misses were not random: two arithmetic cases failed reproducibly only when the correct response appeared second and the API choice options were reversed. All three models returned identical labels on every byte-identical repeat, so the main distinction is ordering robustness rather than ordinary run-to-run instability. SubQ used the fewest reported input tokens but had higher observed round-trip latency; differing provider routes and loads make that latency comparison descriptive only. These deliberately easy synthetic controls do not establish general task accuracy, but they show that crossing display order with API option order can reveal systematic failures that aggregate accuracy or simple repetition would miss.

The study uses 60 synthetic base cases with two byte-identical repetitions across every ordering condition. Twenty exact-code cases and twenty 12-record lookups produce 560 requests per provider. Twenty one-digit arithmetic answer pairs produce another 320. There is no external dataset, LLM judge, or outcome-guided prompt tuning. Repetitions and ordering variants are correlated observations, not independent cases.

## Results

| Provider | Exact choices | Record lookups | Arithmetic | Repeat pairs agreeing | Refusals / failed attempts |
|---|---:|---:|---:|---:|---:|
| Jev | 320/320 | 240/240 | 320/320 | 440/440 | 0 / 0 |
| OpenAI Decisions | 320/320 | 240/240 | 320/320 | 440/440 | 0 / 0 |
| **SubQ Snap** | **320/320** | **240/240** | **312/320** | **440/440** | **0 / 0** |

Jev and OpenAI Decisions kept the correct label in every ordering of all 60 base cases. SubQ did the same for exact matching and lookup. Its eight arithmetic errors form a repeatable interaction: two base cases flip only when the correct response is displayed second and the API choice options are reversed. Both question orders and both byte-identical repetitions reproduce the result. SubQ arithmetic accuracy is 160/160 with the correct response first versus 152/160 second, and 160/160 with option order `XY` versus 152/160 with `YX`.

| Provider | Exact/lookup input tokens | Arithmetic input tokens | Exact/lookup p50 / p95 | Arithmetic p50 / p95 |
|---|---:|---:|---:|---:|
| Jev | 266,000 | 131,216 | 98.7 / 140.2 ms | 94.0 / 136.0 ms |
| OpenAI Decisions | 233,440 | 109,760 | 104.1 / 144.4 ms | 113.9 / 200.0 ms |
| SubQ Snap | 138,400 | 58,896 | 225 / 251 ms | 223 / 266 ms |

At SubQ Snap's published $0.05 per million input-token price, the exact/lookup arm cost an estimated **$0.0069** and the arithmetic arm **$0.0029**. Provider routes and loads differ, so round trips are deployment observations rather than an intrinsic speed ranking.

Perfect performance on these small synthetic controls does not establish general accuracy or absence of ordering effects under uncertainty. The SubQ arithmetic interaction shows why independently crossing response and option order matters even on easy examples.

## Evidence

Source-free generated summaries are provided for [Jev](results/jev/summary.json), [OpenAI Decisions](results/openai-decisions/summary.json), and [SubQ](results/subq/summary.json), with corresponding [arithmetic summaries](results/arithmetic/). Raw receipts are excluded.
