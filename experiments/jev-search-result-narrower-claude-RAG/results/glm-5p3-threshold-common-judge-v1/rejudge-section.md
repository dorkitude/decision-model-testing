---
title: GLM common-judge comparison
type: results
---

## Common-judge comparison

Every arm's saved answers were judged by `accounts/fireworks/models/glm-5p3-flash` using the frozen judge prompt and request shape, with the model changed and reasoning_effort=low for model compatibility.

| Judge | Hybrid + rerank | + Jev filter | + Decisions filter | + SubQ filter |
|---|---:|---:|---:|---:|
| `accounts/fireworks/models/glm-5p3-flash` (primary) | 80/100 | 87/100 | 89/100 | 92/100 |
| DeepSeek `accounts/fireworks/models/deepseek-v4-flash-0731` (frozen reference) | 96/100 | 95/100 | n/a | n/a |
| Jev | n/a | n/a | n/a | n/a |

| Judge | Comparison | Difference | Paired 95% interval |
|---|---|---:|---:|
| rejudged accounts/fireworks/models/glm-5p3-flash | subq_filtered_minus_decisions_filtered | +3.0 pp | [-2.0, +8.0] pp |
| rejudged accounts/fireworks/models/glm-5p3-flash | subq_filtered_minus_jev_filtered | +5.0 pp | [+0.0, +11.0] pp |
| rejudged accounts/fireworks/models/glm-5p3-flash | subq_filtered_minus_baseline | +12.0 pp | [+5.0, +19.0] pp |
| rejudged accounts/fireworks/models/glm-5p3-flash | decisions_filtered_minus_jev_filtered | +2.0 pp | [-3.0, +7.0] pp |
| rejudged accounts/fireworks/models/glm-5p3-flash | decisions_filtered_minus_baseline | +9.0 pp | [+2.0, +16.0] pp |
| rejudged accounts/fireworks/models/glm-5p3-flash | jev_filtered_minus_baseline | +7.0 pp | [+0.0, +14.0] pp |
| frozen accounts/fireworks/models/deepseek-v4-flash-0731 | jev_filtered_minus_baseline | -1.0 pp | [-3.0, +0.0] pp |

| Frozen DeepSeek vs replacement judge | Agree | Cohen's kappa | Frozen correct, replacement incorrect | Frozen incorrect, replacement correct |
|---|---:|---:|---:|---:|
| baseline | 84/100 | 0.286 | 16 | 0 |
| jev_filtered | 92/100 | 0.521 | 8 | 0 |
| all_overlapping | 176/200 | 0.385 | - | - |

Re-judge usage: 400 requests, 380728 input and 6861 output tokens.

Intervals are paired cluster bootstrap intervals by source document (5,000 resamples), descriptive and unadjusted.
