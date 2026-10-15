---
title: Easy deterministic decision controls
type: public-release-documentation
experiment: easy-decisions
author: Kyle Wild
---

# Easy deterministic decision controls

Can decision models reliably solve simple, objectively scored tasks when candidate display order, API option order, question order, or record position changes?

## TL;DR

Jev 1.13, Jev 1.14, and OpenAI Decisions were perfectly accurate across all 880 exact-choice, lookup, and arithmetic requests, while SubQ was perfect on the 560 exact-choice and lookup requests but scored 312/320 on arithmetic. SubQ's eight misses were not random: two arithmetic cases failed reproducibly only when the correct response appeared second and the API choice options were reversed. All four models returned identical labels on every byte-identical repeat, so the main distinction is ordering robustness rather than ordinary run-to-run instability. The two Jev versions used identical tokens and differed only in gold-option probabilities, by at most 0.02. SubQ used the fewest reported input tokens but had the highest observed round-trip latency; differing provider routes and loads make that latency comparison descriptive only. These deliberately easy synthetic controls do not establish general task accuracy, but they show that crossing display order with API option order can reveal systematic failures that aggregate accuracy or simple repetition would miss.

The study uses 60 synthetic base cases with two byte-identical repetitions across every ordering condition. Twenty exact-code cases and twenty 12-record lookups produce 560 requests per model. Twenty one-digit arithmetic answer pairs produce another 320. Jev 1.13 (`jev-1.13.0`) and Jev 1.14 (`jev-1.14.0`) receive the same frozen request bodies, differing only in the pinned `model` value; every response reported the requested model id. There is no external dataset, LLM judge, or outcome-guided prompt tuning. Repetitions and ordering variants are correlated observations, not independent cases.

## Results

| Model | Exact choices | Record lookups | Arithmetic | Repeat pairs agreeing | Refusals / failed attempts |
|---|---:|---:|---:|---:|---:|
| Jev 1.13 (`jev-1.13.0`) | 320/320 | 240/240 | 320/320 | 440/440 | 0 / 0 |
| Jev 1.14 (`jev-1.14.0`) | 320/320 | 240/240 | 320/320 | 440/440 | 0 / 0 |
| OpenAI Decisions | 320/320 | 240/240 | 320/320 | 440/440 | 0 / 0 |
| **SubQ Snap** | **320/320** | **240/240** | **312/320** | **440/440** | **0 / 0** |

Both Jev versions and OpenAI Decisions kept the correct label in every ordering of all 60 base cases. SubQ did the same for exact matching and lookup. Its eight arithmetic errors form a repeatable interaction: two base cases flip only when the correct response is displayed second and the API choice options are reversed. Both question orders and both byte-identical repetitions reproduce the result. SubQ arithmetic accuracy is 160/160 with the correct response first versus 152/160 second, and 160/160 with option order `XY` versus 152/160 with `YX`.

| Model | Exact/lookup input tokens | Arithmetic input tokens | Exact/lookup p50 / p95 | Arithmetic p50 / p95 |
|---|---:|---:|---:|---:|
| Jev 1.13 | 266,000 | 131,216 | 98.7 / 140.2 ms | 94.0 / 136.0 ms |
| Jev 1.14 | 266,000 | 131,216 | 70.2 / 84.5 ms | 68.5 / 84.5 ms |
| OpenAI Decisions | 233,440 | 109,760 | 104.1 / 144.4 ms | 113.9 / 200.0 ms |
| SubQ Snap | 138,400 | 58,896 | 225 / 251 ms | 223 / 266 ms |

At Jev's $0.042 per million input-token list price, each Jev version cost an estimated **$0.0112** for exact/lookup and **$0.0055** for arithmetic ($0.016683 in total); the two versions reported identical input (and output) token counts. At SubQ Snap's published $0.05 per million input-token price, the exact/lookup arm cost an estimated **$0.0069** and the arithmetic arm **$0.0029**. Provider routes and loads differ, and each model was measured sequentially at a different time, so round trips are deployment observations rather than an intrinsic speed ranking.

At ceiling accuracy, the two Jev versions differ only in how much probability they place on the correct option:

| Gold-option probability | Jev 1.13 min / median / mean; # exactly 1.00 | Jev 1.14 min / median / mean; # exactly 1.00 |
|---|---|---|
| Exact-code (320) | 1.00 / 1.00 / 1.000000; 320 | 0.99 / 1.00 / 0.99991; 317 |
| Lookup (240) | 1.00 / 1.00 / 1.000000; 240 | 1.00 / 1.00 / 1.000000; 240 |
| Arithmetic (320) | 0.98 / 1.00 / 0.998437; 273 | 0.98 / 1.00 / 0.999531; 306 |

Paired request by request, Jev 1.14 is 0.01 lower on three exact-code requests and identical on the other 317 and on all lookups; on arithmetic it is higher on 39 requests, lower on 7 and identical on 274 (mean +0.00109, range −0.01 to +0.02). Its mean absolute arithmetic probability change when reversing one factor is smaller for display order (0.000938 vs 0.002875) and option order (0.000563 vs 0.001750), and slightly larger for question order (0.000563 vs 0.000375); on exact-code choices Jev 1.13 never moved, while Jev 1.14 moved on 3 of 160 display-order and 3 of 160 option-order pairs. None of these shifts changes an answer.

Perfect performance on these small synthetic controls does not establish general accuracy or absence of ordering effects under uncertainty. Sixty clustered base cases cannot separate two models that are both at ceiling, so the Jev 1.13 and Jev 1.14 results should be read as equivalent on this control rather than as evidence about harder judgments. The SubQ arithmetic interaction shows why independently crossing response and option order matters even on easy examples.

## Evidence

Source-free generated summaries are provided for [Jev 1.13](results/jev/summary.json), [Jev 1.14](results/jev-1.14.0/summary.json), [OpenAI Decisions](results/openai-decisions/summary.json), and [SubQ](results/subq/summary.json), with corresponding [arithmetic summaries](results/arithmetic/). Raw receipts are excluded.
