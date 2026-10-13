---
title: SubQ Snap source-free eval-judging aggregates
type: result-documentation
experiment: decision-models-vs-llm-judges
author: Kyle Wild
---

# SubQ Snap aggregates

## TL;DR

No judge dominated every benchmark. Qwen3p8 Max led the three headline LLMBar, JudgeBench, and RewardBench 2 methods at 92.53%, 88.39%, and 81.57% strict agreement. Among decision models, SubQ was strongest at 88.06%, 72.10%, and 69.39%, exceeding Jev's 87.36%, 69.03%, and 68.12% on all three; its point estimate also exceeded Jev on 18 of 19 primary methods. Decisions was competitive on some LLMBar rating prompts but fell to 23.87% on JudgeBench vanilla because it selected the first-shown response in both presentation orders for 71.1% of pairs. SubQ showed a much smaller 9.2% version of that bias and completed 29,487 requests without failure or refusal, while Decisions refused 159 RewardBench requests. A held-out Jev-first JudgeBench cascade improved GPT-OSS from 76.41% to 80.12% while reducing accounting cost by 43.69%; other cascades were weaker or inconclusive. Scores measure benchmark-label agreement, not independently verified correctness, and reported intervals are not multiplicity-adjusted.

[`summary.json`](summary.json) contains source-free aggregate measurements for SubQ Snap on the 19 primary LLMBar, JudgeBench and RewardBench 2 methods and the two native-interface LLMBar methods. It includes strict benchmark-label agreement, paired bootstrap intervals against Jev, reliability, calibration, response-position counts, held-out cascade results, latency, token usage and list-price cost estimates.

Benchmark examples, submitted answers, request bodies and provider responses are excluded. The source hashes identify the private evidence used to produce the projection, but the projection cannot replace receipt-level audit.
