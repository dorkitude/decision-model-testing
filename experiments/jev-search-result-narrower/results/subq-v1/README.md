---
title: SubQ Snap aggregate results for Kimi RAG filtering
type: results-documentation
---

# SubQ Snap aggregate results

SubQ Snap filtered the same 2,020 chunk decisions used by the Jev arm across 100 EnronQA questions. It kept 267 chunks; Jev kept 307. The two filters agreed on 96.0% of keep/drop decisions (Cohen's κ 0.838), with 60 chunks kept only by Jev and 20 only by SubQ.

Under the Jev answer judge shared by all three arms, the unfiltered pipeline scored 90/100, the Jev-filtered pipeline 94/100, and the SubQ-filtered pipeline 94/100. SubQ minus unfiltered was +4 points with a paired cluster-bootstrap 95% interval of [+1, +8]; SubQ minus Jev was 0 [−4, +4]. These are model-judge outcomes, not independently verified correctness.

All 407 filter requests succeeded on their first attempt. They used 1,754,815 input tokens and cost an estimated $0.0877 at SubQ's published $0.05 per million input tokens. Median filter-page round trip was 0.307 s. The estimated answering-pipeline model cost was $1.60 for SubQ filtering, $1.65 for Jev filtering, and $5.06 unfiltered; these are list-price estimates rather than invoices.

The compact [aggregate projection](summary.json) excludes questions, email text, retrieved chunks, generated answers, and request/response receipts.
