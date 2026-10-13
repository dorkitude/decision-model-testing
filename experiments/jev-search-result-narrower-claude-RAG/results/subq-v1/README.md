---
title: SubQ Snap aggregate results for Claude RAG filtering
type: results-documentation
---

# SubQ Snap aggregate results

SubQ Snap scored the same frozen Claude-study chunks as Jev in both filtering modes. No Claude answers were generated for these filter-only comparisons, so they support filter-agreement and resource findings rather than answer-accuracy claims.

| Mode | Jev kept | SubQ kept | Keep/drop agreement | Cohen's κ | SubQ filter requests | Estimated cost |
|---|---:|---:|---:|---:|---:|---:|
| Probability threshold | 322/2,000 | 264/2,000 | 95.9% | 0.836 | 407 | $0.0887 |
| Categorical | 420/2,000 | 292/2,000 | 91.9% | 0.725 | 416 | $0.0992 |

Every filter request succeeded on its first attempt. Median round trips were 0.308 s in threshold mode and 0.270 s in categorical mode.

As an answer judge on the study's 200 saved answers, SubQ accepted 93 unfiltered and 90 Jev-filtered answers. It agreed with the frozen DeepSeek verdict on 192/200 judgments (κ 0.673) and with Jev's answer rescore on 182/200 (κ 0.579). All 200 requests succeeded on the first attempt, used 213,949 input tokens, and cost an estimated $0.0107. These are model-judge assessments, not independently verified correctness.

The compact [aggregate projection](summary.json) excludes questions, email text, retrieved chunks, generated answers, and request/response receipts.
