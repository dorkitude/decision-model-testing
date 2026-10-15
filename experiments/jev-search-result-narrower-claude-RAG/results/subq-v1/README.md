---
title: SubQ Snap aggregate results for Claude RAG filtering
type: results-documentation
---

# SubQ Snap aggregate results

## TL;DR

All four decision-model filters reduced the context sent to Claude, and the common GLM 5p3 Flash judge favored the filtered answers on this 100-question sample. In threshold mode, GLM accepted 80 unfiltered answers, 87 Jev 1.13-filtered, 87 Jev 1.14-filtered, 89 Decisions-filtered, and 92 SubQ-filtered; Decisions and SubQ improved on baseline by +9 points [+2, +16] and +12 [+5, +19], while both Jev versions' +7 intervals touched zero. In categorical mode, the scores were 80, 81, 91, 89, and 90, with Jev 1.14 +11 [+4, +18], Decisions +9 [+3, +15] and SubQ +10 [+3, +18]. These answer differences do not isolate the filter: baseline and Jev 1.13 answers used Claude Code Max, whereas Jev 1.14, Decisions and SubQ used Team, and Jev 1.13's own filter decisions scored 3 and 6 points higher when answered on Team. With both Jev versions answered on Team and scored in one GLM pass, Jev 1.14 − Jev 1.13 was −3 points [−8, +1] in threshold mode and +6 [+1, +12] in categorical mode, against a noise floor of about ±3 points from Claude's answer variation and judge repeatability. SubQ retained 264 threshold chunks and 292 categorical chunks and Jev 1.14 retained 308 and 378, compared with Jev 1.13's 322 and 420. GLM is also a model judge, not human ground truth, and used its supported low-reasoning mode. The results support substantial context reduction, but provider rankings remain confounded by route and sample.

The common-judge aggregates report scores, paired intervals, judge agreement, and usage for [threshold mode](../glm-5p3-threshold-common-judge-v1/rejudge-comparison.jsonl) and [categorical mode](../glm-5p3-categorical-common-judge-v1/rejudge-comparison.jsonl).

SubQ Snap scored the same frozen Claude-study chunks as Jev 1.13 in both filtering modes. Each SubQ filter also produced 100 Claude Opus 5 answers through Claude Code Team. Under the common GLM 5p3 Flash judge, threshold-mode acceptance was 80/100 unfiltered, 87/100 with Jev 1.13, 87/100 with Jev 1.14, 89/100 with Decisions, and 92/100 with SubQ. Categorical-mode acceptance was 80/100, 81/100, 91/100, 89/100, and 90/100. Baseline and Jev 1.13 answers used Claude Code Max, so route effects are confounded with filter effects.

| Mode | Jev 1.13 kept | SubQ kept | Keep/drop agreement | Cohen's κ | SubQ filter requests | Estimated cost |
|---|---:|---:|---:|---:|---:|---:|
| Probability threshold | 322/2,000 | 264/2,000 | 95.9% | 0.836 | 407 | $0.0887 |
| Categorical | 420/2,000 | 292/2,000 | 91.9% | 0.725 | 416 | $0.0992 |

Every filter request succeeded on its first attempt. Median round trips were 0.308 s in threshold mode and 0.270 s in categorical mode.

As an answer judge on the study's 200 saved answers, SubQ accepted 93 unfiltered and 90 Jev-filtered answers. It agreed with the frozen DeepSeek verdict on 192/200 judgments (κ 0.673) and with Jev's answer rescore on 182/200 (κ 0.579). All 200 requests succeeded on the first attempt, used 213,949 input tokens, and cost an estimated $0.0107. These are model-judge assessments, not independently verified correctness.

The compact [aggregate projection](summary.json) excludes questions, email text, retrieved chunks, generated answers, and request/response receipts.
