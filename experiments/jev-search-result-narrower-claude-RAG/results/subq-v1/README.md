---
title: SubQ Snap aggregate results for Claude RAG filtering
type: results-documentation
---

# SubQ Snap aggregate results

## TL;DR

All three decision-model filters reduced the context sent to Claude, and the common GLM 5p3 Flash judge favored the filtered answers on this 100-question sample. In threshold mode, GLM accepted 80 unfiltered answers, 87 Jev-filtered, 89 Decisions-filtered, and 92 SubQ-filtered; Decisions and SubQ improved on baseline by +9 points [+2, +16] and +12 [+5, +19], while Jev's +7 interval touched zero [0, +14]. In categorical mode, the scores were 80, 81, 89, and 90, with Decisions +9 [+3, +15] and SubQ +10 [+3, +18]. SubQ retained 264 threshold chunks and 292 categorical chunks, compared with Jev's 322 and 420. These answer differences do not isolate the filter: baseline and Jev answers used Claude Code Max, whereas Decisions and SubQ used Team. GLM is also a model judge, not human ground truth, and used its supported low-reasoning mode. The results support substantial context reduction, but provider rankings remain confounded by route and sample.

The common-judge aggregates report scores, paired intervals, judge agreement, and usage for [threshold mode](../glm-5p3-threshold-common-judge-v1/rejudge-comparison.jsonl) and [categorical mode](../glm-5p3-categorical-common-judge-v1/rejudge-comparison.jsonl).

SubQ Snap scored the same frozen Claude-study chunks as Jev in both filtering modes. Each SubQ filter also produced 100 Claude Opus 5 answers through Claude Code Team. Under the common GLM 5p3 Flash judge, threshold-mode acceptance was 80/100 unfiltered, 87/100 with Jev, 89/100 with Decisions, and 92/100 with SubQ. Categorical-mode acceptance was 80/100, 81/100, 89/100, and 90/100. Baseline and Jev answers used Claude Code Max, so route effects are confounded with filter effects.

| Mode | Jev kept | SubQ kept | Keep/drop agreement | Cohen's κ | SubQ filter requests | Estimated cost |
|---|---:|---:|---:|---:|---:|---:|
| Probability threshold | 322/2,000 | 264/2,000 | 95.9% | 0.836 | 407 | $0.0887 |
| Categorical | 420/2,000 | 292/2,000 | 91.9% | 0.725 | 416 | $0.0992 |

Every filter request succeeded on its first attempt. Median round trips were 0.308 s in threshold mode and 0.270 s in categorical mode.

As an answer judge on the study's 200 saved answers, SubQ accepted 93 unfiltered and 90 Jev-filtered answers. It agreed with the frozen DeepSeek verdict on 192/200 judgments (κ 0.673) and with Jev's answer rescore on 182/200 (κ 0.579). All 200 requests succeeded on the first attempt, used 213,949 input tokens, and cost an estimated $0.0107. These are model-judge assessments, not independently verified correctness.

The compact [aggregate projection](summary.json) excludes questions, email text, retrieved chunks, generated answers, and request/response receipts.
