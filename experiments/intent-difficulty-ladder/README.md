---
title: Graded intent-routing difficulty
type: public-release-documentation
experiment: intent-difficulty-ladder
author: Kyle Wild
---

# Graded intent routing

How do decision models and a generative LLM control behave as objectively labeled intent menus grow, and how stable are their labels when the same options move?

## TL;DR

The decision models were more accurate and substantially more label-stable than Haiku 5.5 on these held-out intent-routing samples. On CLINC's 30-intent plus out-of-scope task, Jev 1.14 scored 194/200 and Jev 1.13, Decisions, and SubQ each scored 192/200, versus 112/200 for Haiku. At all 77 BANKING intents, Decisions led with 110/120, while both Jev versions and SubQ scored 108/120 and Haiku scored 90/120. Candidate shuffling changed 116/298 Haiku labels, compared with 8/299 for Jev 1.13, 9/300 for Jev 1.14, 5/299 for Decisions, and 3/300 for SubQ; byte-identical repeats changed 51/270 Haiku labels, 1/269 for Jev 1.13, 4/270 for Jev 1.14, and none for Decisions or SubQ. All of Jev 1.14's label changes came from a single in-scope CLINC+`NONE` case. SubQ's labels were the most stable, yet its gold-option probabilities still moved by mean absolute 0.025–0.049 when candidates were reordered. Jev 1.14 matched or slightly exceeded Jev 1.13 on menu-width accuracy, with no width showing a reliable difference, and it completed every call without an error or retry. The samples contain only 30 independent examples per menu width, so small provider differences are descriptive, and the non-monotonic results do not support a universal menu-size cutoff.

The study uses held-out examples from CLINC150 (CC BY 3.0) and BANKING77 (CC BY 4.0). Candidate descriptions and examples come only from each training split. For each dataset, the same 30 test utterances are classified against nested candidate menus, each in a seeded order and its exact reverse with two byte-identical calls per order. CLINC also has a separate 50-case in-scope/out-of-scope stage with a `NONE` option. Strict accuracy counts refusals and invalid labels as wrong. The generative control is Claude Haiku 5.5 with thinking disabled; the decision models are Jev 1.13 (`jev-1.13.0`), Jev 1.14 (`jev-1.14.0`), OpenAI Decisions, and SubQ Snap. Both Jev versions receive identical planned request bodies except for the pinned `model` value, and every Jev response reported the requested version.

## CLINC150

Each menu-width cell contains 120 correlated requests from 30 independent examples. The `30 + NONE` cell contains 200 requests from 50 independent examples.

| Provider | 2 intents | 4 | 8 | 15 | 30 | 30 + `NONE` |
|---|---:|---:|---:|---:|---:|---:|
| Haiku 5.5 | 116/120 | 116/120 | 114/120 | 116/120 | 100/120 | 112/200 |
| Jev 1.13 | 118/120 | 118/120 | 118/120 | 118/120 | **120/120** | 192/200 |
| Jev 1.14 | 117/120 | **120/120** | **120/120** | **120/120** | 119/120 | **194/200** |
| OpenAI Decisions | **120/120** | 114/120 | 118/120 | 116/120 | 114/120 | 192/200 |
| SubQ Snap | **120/120** | **120/120** | 118/120 | 116/120 | 116/120 | 192/200 |

Bold marks the best score in each column. On CLINC's OOS stage, SubQ selected `NONE` correctly for 94/100 native-OOS requests and selected the correct intent for 98/100 in-scope requests. Jev 1.14 selected `NONE` correctly for 98/100 native-OOS requests and the correct intent for 96/100 in-scope requests (Jev 1.13: 98/100 and 94/100). Jev 1.13, Decisions, and SubQ scored 192/200 overall; Haiku scored 112/200. The two Jev versions differ on only one or two of the 30 (or 50) independent cases in any column, and every paired case-bootstrap interval for their difference includes zero. Menu width is not strictly monotonic difficulty—several rows improve or fluctuate—so these data do not define a universal threshold.

## BANKING77

Each cell contains 120 correlated requests from the same 30 independent examples.

| Provider | 2 intents | 4 | 8 | 16 | 32 | 77 |
|---|---:|---:|---:|---:|---:|---:|
| Haiku 5.5 | 107/120 | 106/120 | 105/120 | 97/120 | 98/120 | 90/120 |
| Jev 1.13 | 112/120 | **112/120** | **108/120** | 108/120 | **108/120** | 108/120 |
| Jev 1.14 | **116/120** | **112/120** | **108/120** | 108/120 | **108/120** | 108/120 |
| OpenAI Decisions | 114/120 | 108/120 | **108/120** | **110/120** | **108/120** | **110/120** |
| SubQ Snap | 110/120 | 106/120 | **108/120** | 106/120 | 106/120 | 108/120 |

Bold marks the best score in each column. At 77 intents, SubQ and both Jev versions scored 108/120 and trailed Decisions by two answers in this sample. Jev 1.14 and Jev 1.13 score identically at every width except two intents, where one case is correct on all four Jev 1.14 calls. Jev 1.14 also agreed with itself on all 60 reverse-order pairs at 77 intents (Jev 1.13: 58/60). With only 30 independent examples per width and multiple inspected contrasts, the result is descriptive rather than a general provider ranking.

## Ordering consistency

Ten cases in each of three settings—CLINC30, CLINC30+`NONE`, and BANKING77—receive ten byte-identical calls and ten distinct seeded candidate-order shuffles. A change means the selected label differs, not necessarily that it becomes wrong.

| Provider | Changed label on identical calls | Changed label after shuffle |
|---|---:|---:|
| Haiku 5.5 | 51/270 | 116/298 |
| Jev 1.13 | 1/269 | 8/299 |
| Jev 1.14 | 4/270 | 9/300 |
| OpenAI Decisions | **0/270** | 5/299 |
| SubQ Snap | **0/270** | **3/300** |

All three SubQ shuffle changes occur on one in-scope CLINC+`NONE` case. Ordinary CLINC, BANKING77, and all 50 native-OOS shuffle pairs have no SubQ label flips. Its reported gold-option probability still moves when candidates move: mean absolute changes are 0.025 on CLINC30, 0.049 on CLINC+`NONE`, and 0.038 on BANKING77. Stable labels therefore do not imply invariant confidence.

Jev 1.14's changes are likewise concentrated: all four identical-input changes and all nine shuffle changes come from one in-scope CLINC+`NONE` case, on which it alternated between the correct intent and `NONE` even for byte-identical requests. Jev 1.13 answered that case correctly on all 20 calls. In the other direction, Jev 1.14 had no shuffle changes on CLINC30 (Jev 1.13: 5/99) and answered all 200 CLINC30 calls correctly (Jev 1.13: 194/200). Strict ordering-consistency accuracy was 569/600 for Jev 1.14 and 577/600 for Jev 1.13. Both Jev versions had no label flips on BANKING77 or on the 50 native-OOS shuffle pairs. With ten source questions per setting, this is one case moving in each direction, not evidence of a general consistency difference. Jev 1.14's mean absolute gold-probability shifts under shuffling were 0.028 on CLINC30, 0.024 on CLINC+`NONE`, and 0.010 on BANKING77 (Jev 1.13: 0.035, 0.027, and 0.007).

SubQ completed 2,120 benchmark calls without a refusal, retry, transport failure, or invalid answer. Its p50/p95 HTTP round trips were 175/216 ms for CLINC, 178/229 ms for BANKING77, and 183/245 ms for ordering consistency. At the published $0.05 per million input-token price, the calls cost an estimated **$0.1504** in total.

Jev 1.14 also completed 2,120 benchmark calls with no HTTP error, retry, refusal, or invalid answer. Its p50/p95 HTTP round trips were 72.1/86.6 ms for CLINC, 72.5/96.3 ms for BANKING77, and 80.2/101.8 ms for ordering consistency (Jev 1.13: 93.6/135.3, 95.9/132.6, and 129.6/4,105 ms). Jev 1.13's ordering-consistency calls included 25 transient HTTP 5xx attempts and one request that exhausted its four attempts, which inflates that tail. These are client-observed round trips under different loads, not intrinsic model speed. At $0.042 per million input tokens for both Jev versions, Jev 1.14's 3,795,448 input tokens cost an estimated **$0.1594** in total; output tokens are not priced.

## Evidence and limits

[Source-free CLINC aggregates](results/clinc.json), [BANKING77 aggregates](results/banking.json), and [ordering aggregates](results/ordering.json) include all public provider arms, plan hashes, case-level intervals, reliability, usage, and latency. In these files the `jev` provider is Jev 1.13 (`jev-1.13.0`) and the `jev-1.14.0` provider is Jev 1.14. Dataset text, requests, and raw provider receipts are excluded. Public benchmark overlap with model training is possible, the provider interfaces differ, and the small samples do not establish statistical equivalence or architectural superiority. The Jev 1.14 versus Jev 1.13 differences amount to one or two cases per cell and cannot be distinguished from case-level noise, and the single unstable CLINC+`NONE` case shows that Jev 1.14 is not fully deterministic at a fixed input.

Upstream sources: [CLINC150](https://github.com/clinc/oos-eval) (Larson et al., EMNLP 2019) and [BANKING77](https://github.com/PolyAI-LDN/task-specific-datasets) (Casanueva et al., 2020).
