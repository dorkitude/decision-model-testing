---
title: Jev 1.14 source-free eval-judging aggregates
type: result-documentation
experiment: decision-models-vs-llm-judges
model: jev-1.14.0
author: Kyle Wild
---

# Jev 1.14 aggregates

## TL;DR

No judge dominated every benchmark. Qwen3p8 Max led the three headline LLMBar, JudgeBench, and RewardBench 2 methods at 92.53%, 88.39%, and 81.57% strict agreement. Among decision models, SubQ and Jev 1.14 were strongest: SubQ scored 88.06%, 72.10%, and 69.39%, and Jev 1.14 scored 88.24%, 70.81%, and 69.14%, both above Jev 1.13's 87.36%, 69.03%, and 68.12%. Jev 1.14's point estimate exceeded Jev 1.13 on all 19 primary methods, SubQ's on 18 of 19. Decisions was competitive on some LLMBar rating prompts but fell to 23.87% on JudgeBench vanilla because it selected the first-shown response in both presentation orders for 71.1% of pairs. SubQ showed a much smaller 9.2% version of that bias and Jev 1.14 2.9%; both completed every job without refusal or invalid answer, while Decisions refused 159 RewardBench requests. A held-out Jev 1.13-first JudgeBench cascade improved GPT-OSS from 76.41% to 80.12% while reducing accounting cost by 43.69%; other cascades were weaker or inconclusive. Scores measure benchmark-label agreement, not independently verified correctness, and reported intervals are not multiplicity-adjusted.

[`summary.json`](summary.json) contains source-free aggregate measurements for Jev 1.14 (`jev-1.14.0`) on the 19 primary LLMBar, JudgeBench and RewardBench 2 methods and the two native-interface LLMBar methods, paired with Jev 1.13 (`jev-1.13.0`) on the same jobs. Both Jev versions received the same request content and the same GPT-OSS helper outputs. The [provider comparison](../../README.md#provider-comparison) reports the headline numbers.

Fields under the top level:

- `primary.scores` and `native.scores`: per benchmark and method, both Jev versions' strict score, valid jobs, accounting USD (Jev input at $0.042 per million plus shared helper calls), tokens, request p50/p95 (HTTP round trip), median job time and order agreement.
- `primary.jev_1_14_minus_jev_1_13_original`, `_retry_policy` and `_jointly_valid`: paired strict-score differences (and published-score differences) with 2,000-replicate bootstrap 95% intervals, unadjusted. *Original* scores each version's first pass, with required-stage failures as zero; *retry policy* applies the service-error retry pass to Jev 1.13's failed jobs (Jev 1.14 had none); *jointly valid* drops cases either version failed.
- `primary.jev_1_14_minus_llm_original` and `_retry_policy`, `native.jev_1_14_minus_llm`: paired Jev 1.14 − LLM-judge differences for GPT-OSS 120B, DeepSeek V4 Flash, GLM 5p3 and Qwen3p8 Max.
- `primary.retry_policy_scores` and `service_retry_policy_headline`: final strict scores under the service-error retry policy for both Jev versions and the four LLM judges.
- `primary.reliability`, `native.reliability` and `requests`: attempts, transient error attempts by kind, terminal failures, HTTP 429s, refusals, invalid jobs, tokens, list-price input cost and request round-trip percentiles for both Jev versions.
- `primary.calibration`: Brier score, equal-width ECE and coverage of first-order choice probabilities on JudgeBench vanilla and LLMBar Vanilla, both versions.
- `primary.llmbar_order_metrics`: per LLMBar method, the share of pairs with the same verdict in both presentation orders and per-order accuracy, both versions. `primary.first_position_rate`: share of valid per-order choices that picked the first-shown response.
- `position_bias`: on JudgeBench vanilla, JudgeBench arena_hard and LLMBar Vanilla, how many pairs got the same verdict in both orders (`order_consistent`), the first-shown response in both orders, the second-shown in both, other mixed verdicts, or an incomplete pair, for both versions.
- `cascades_jev_1_14_first`: the four preregistered cascades with Jev 1.14 as the first stage, thresholds selected on development cases and evaluated once on held-out cases, including the exact-question overlap sensitivity.
- `totals`: Jev 1.14 jobs, requests, input tokens, list-price USD, accounting USD, invalid jobs, error attempts and service-retry jobs.

Benchmark examples, submitted answers, case identifiers, request bodies and provider responses are excluded. `sources` lists the private evidence files used to produce the projection with their SHA-256, but the projection cannot replace receipt-level audit. Prices are dated list-price estimates, not invoices.
