---
title: GLM common-judge comparison
type: results
---

## Common-judge comparison

Every arm's saved answers were judged by `accounts/fireworks/models/glm-5p3-flash` using the frozen judge prompt and request shape, with the model changed and reasoning_effort=low for model compatibility.

| Judge | Hybrid + rerank | + Jev filter | + Decisions filter | + SubQ filter |
|---|---:|---:|---:|---:|
| `accounts/fireworks/models/glm-5p3-flash` (primary) | 94/100 | 93/100 | 90/100 | 95/100 |
| DeepSeek `accounts/fireworks/models/deepseek-v4-flash-0731` (frozen reference) | 98/100 | 98/100 | n/a | n/a |
| Jev | 90/100 | 94/100 | n/a | n/a |

| Judge | Comparison | Difference | Paired 95% interval |
|---|---|---:|---:|
| rejudged accounts/fireworks/models/glm-5p3-flash | subq_filtered_minus_decisions_filtered | +5.0 pp | [+0.0, +10.0] pp |
| rejudged accounts/fireworks/models/glm-5p3-flash | subq_filtered_minus_jev_filtered | +2.0 pp | [-2.0, +6.0] pp |
| rejudged accounts/fireworks/models/glm-5p3-flash | subq_filtered_minus_baseline | +1.0 pp | [-3.0, +5.0] pp |
| rejudged accounts/fireworks/models/glm-5p3-flash | decisions_filtered_minus_jev_filtered | -3.0 pp | [-7.0, +0.0] pp |
| rejudged accounts/fireworks/models/glm-5p3-flash | decisions_filtered_minus_baseline | -4.0 pp | [-11.0, +2.0] pp |
| rejudged accounts/fireworks/models/glm-5p3-flash | jev_filtered_minus_baseline | -1.0 pp | [-7.0, +5.0] pp |
| frozen accounts/fireworks/models/deepseek-v4-flash-0731 | jev_filtered_minus_baseline | +0.0 pp | [+0.0, +0.0] pp |
| jev | jev_filtered_minus_baseline | +4.0 pp | [+0.0, +9.0] pp |

| Frozen DeepSeek vs replacement judge | Agree | Cohen's kappa | Frozen correct, replacement incorrect | Frozen incorrect, replacement correct |
|---|---:|---:|---:|---:|
| baseline | 96/100 | 0.485 | 4 | 0 |
| jev_filtered | 95/100 | 0.427 | 5 | 0 |
| all_overlapping | 191/200 | 0.454 | - | - |

Re-judge usage: 400 requests, 357319 input and 4591 output tokens.

Intervals are paired cluster bootstrap intervals by source document (5,000 resamples), descriptive and unadjusted.
