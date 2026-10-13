---
title: GLM common-judge comparison
type: results
---

## Common-judge comparison

Every arm's saved answers were judged by `accounts/fireworks/models/glm-5p3-flash` using the frozen judge prompt and request shape, with the model changed and reasoning_effort=low for model compatibility.

| Judge | Hybrid + rerank | + Jev filter | + Decisions filter | + SubQ filter |
|---|---:|---:|---:|---:|
| `accounts/fireworks/models/glm-5p3-flash` (primary) | 80/100 | 81/100 | 89/100 | 90/100 |
| DeepSeek `accounts/fireworks/models/deepseek-v4-flash-0731` (frozen reference) | 96/100 | 96/100 | n/a | n/a |
| Jev | n/a | n/a | n/a | n/a |

| Judge | Comparison | Difference | Paired 95% interval |
|---|---|---:|---:|
| rejudged accounts/fireworks/models/glm-5p3-flash | subq_filtered_minus_decisions_filtered | +1.0 pp | [-3.0, +6.0] pp |
| rejudged accounts/fireworks/models/glm-5p3-flash | subq_filtered_minus_jev_filtered | +9.0 pp | [+2.0, +16.0] pp |
| rejudged accounts/fireworks/models/glm-5p3-flash | subq_filtered_minus_baseline | +10.0 pp | [+3.0, +18.0] pp |
| rejudged accounts/fireworks/models/glm-5p3-flash | decisions_filtered_minus_jev_filtered | +8.0 pp | [+1.0, +15.0] pp |
| rejudged accounts/fireworks/models/glm-5p3-flash | decisions_filtered_minus_baseline | +9.0 pp | [+3.0, +15.0] pp |
| rejudged accounts/fireworks/models/glm-5p3-flash | jev_filtered_minus_baseline | +1.0 pp | [-7.0, +8.0] pp |
| frozen accounts/fireworks/models/deepseek-v4-flash-0731 | jev_filtered_minus_baseline | +0.0 pp | [-3.0, +3.0] pp |

| Frozen DeepSeek vs replacement judge | Agree | Cohen's kappa | Frozen correct, replacement incorrect | Frozen incorrect, replacement correct |
|---|---:|---:|---:|---:|
| baseline | 84/100 | 0.286 | 16 | 0 |
| jev_filtered | 85/100 | 0.302 | 15 | 0 |
| all_overlapping | 169/200 | 0.294 | - | - |

Re-judge usage: 400 requests, 382193 input and 7614 output tokens.

Intervals are paired cluster bootstrap intervals by source document (5,000 resamples), descriptive and unadjusted.
