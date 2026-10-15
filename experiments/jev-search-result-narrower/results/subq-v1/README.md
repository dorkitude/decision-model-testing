---
title: SubQ Snap aggregate results for Kimi RAG filtering
type: results-documentation
---

# SubQ Snap aggregate results

## TL;DR

Filtering cut the estimated cost of the Kimi pipeline by about two-thirds, with no clear loss in answer quality on this 100-question sample. Under one common GLM 5p3 Flash judge, SubQ-filtered answers scored highest at 95/100. Unfiltered and Jev 1.14-filtered answers scored 94, Jev 1.13-filtered 93, and Decisions-filtered 90. None of the paired differences from unfiltered excluded zero: SubQ was +1 point [−3, +5], Jev 1.14 0 [−5, +5], Jev 1.13 −1 [−7, +5], and Decisions −4 [−11, +2]. Jev 1.14 − Jev 1.13 was +1 [−2, +5], and a second all-arm GLM pass moved individual arms by up to 3 points. Jev 1.13 kept 307 of 2,020 chunks, Jev 1.14 kept 283 of 2,000, SubQ kept 267 of 2,020, and Decisions kept 263 of 2,040. On identical keep/drop decisions, Jev 1.14, SubQ and Decisions agreed with Jev 1.13 on 98.25%, 96.0% and 95.6% of chunks. Estimated whole-pipeline costs were $1.60–$1.69 for the filtered arms, against $5.06 unfiltered. GLM is a model judge rather than human ground truth, and all judgments used its supported low-reasoning mode. These results therefore support the cost reduction more strongly than any ranking of the filters.

The [common-judge aggregate](../glm-5p3-common-judge-v1/rejudge-comparison.jsonl) reports the all-arm scores, paired intervals, judge agreement, and usage.

SubQ Snap filtered the same 2,020 chunk decisions used by the Jev arm across 100 EnronQA questions. It kept 267 chunks; Jev kept 307. The two filters agreed on 96.0% of keep/drop decisions (Cohen's κ 0.838), with 60 chunks kept only by Jev and 20 only by SubQ.

Under the common GLM 5p3 Flash judge, the unfiltered pipeline scored 94/100, the Jev-filtered pipeline 93/100, the Decisions-filtered pipeline 90/100, and the SubQ-filtered pipeline 95/100. Against unfiltered, the paired cluster-bootstrap differences were Jev −1 point [−7, +5], Decisions −4 [−11, +2], and SubQ +1 [−3, +5]. These are model-judge outcomes, not independently verified correctness.

All 407 filter requests succeeded on their first attempt. They used 1,754,815 input tokens and cost an estimated $0.0877 at SubQ's published $0.05 per million input tokens. Median filter-page round trip was 0.307 s. The estimated answering-pipeline model cost was $1.60 for SubQ filtering, $1.65 for Jev filtering, and $5.06 unfiltered; these are list-price estimates rather than invoices.

The compact [aggregate projection](summary.json) excludes questions, email text, retrieved chunks, generated answers, and request/response receipts.
