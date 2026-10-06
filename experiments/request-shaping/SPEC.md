---
title: Jev best practices specification
type: research-specification
experiment: jev-best-practices
created: 2026-09-25
author: Kyle Wild
status: protocol-v1-run
---

# Jev best practices

How should Jev requests be shaped? This experiment measures request-shaping choices that practitioners make every day but that no one has benchmarked. It has three studies:

1. **State packing:** one item per state compared with several items sharing one state.
2. **Question fan-out:** one question per request compared with several questions about the same state in one request.
3. **Position in state:** whether content near the beginning, middle, or end of the `state` value gets more or less weight.

> Origin: Kyle Wild proposed this study on 2026-09-25 to find the best ways to shape Jev requests (one item per state versus several), and added a third study on ordering the same day.

Implementation and live runs began on 2026-09-25. The parameters actually run are frozen in [Protocol v1 as run](#protocol-v1-as-run); the sections above them record the design intent.

## Why it matters

TypeSafe's documentation points both ways:

- The [Jev 1.13 jaggedness page](https://docs.typesafe.ai/model-jaggedness/jev-1.13) recommends packing items into one state and asking one question per item (`items[i]`) for counting.
- The same page warns that accuracy falls when the state carries irrelevant detail (failure mode 5) and when questions need indirection (failure mode 4). Every neighbor in a packed state is irrelevant to the other items' questions.
- The [State page](https://docs.typesafe.ai/concepts/state) says questions sharing a state "are evaluated independently". That makes Study 2 a direct test of a documented claim.

Our own experiments already use both shapes. [`jev-vs-rerankers`](../jev-vs-rerankers/) sends one passage per state. [`jev-search-result-narrower`](../jev-search-result-narrower/) packs up to six chunks per page and lists page-local context effects as a limitation. The [reranker protocol](../jev-vs-rerankers/PROTOCOL-v1.md) set shared-state scoring aside as a separate method to evaluate later.

## Shared benchmark

Both studies reuse the frozen `jev-vs-rerankers` inputs:

- all 43 TREC DL19 and 54 DL20 judged queries;
- the published RankLLM BM25 top-100 candidates for each query (9,700 query–passage items);
- NIST graded qrels (0–3);
- the pinned `castorini/rank_llm_data` revision and hashes from [`PROTOCOL-v1.md`](../jev-vs-rerankers/PROTOCOL-v1.md).

The data loader is copied with its provenance rather than imported, so the two experiments stay independent.

**Why this benchmark:**
- The labels come from humans, not model judges.
- We already have an atomic baseline under a pinned model (`trec-v1`, `trec-prompts-v1`).
- Passages are short, so large packs fit well under Jev's limits.

All arms use `jev-1.13.0` and the frozen Noul relevance prompt from `jev-vs-rerankers` (`jev-noul`). The only new text is a per-item reference wrapper. It is frozen after synthetic smoke tests only; no benchmark label informs prompt or wrapper choices.

**What labels may do.** Qrels are used only to measure outcomes and to build Study 1's controlled-neighbor packs, which are a deliberate manipulation. They never enter the text Jev sees.

## Study 1: state packing

**Question:** When K query–passage items share one state, each with its own Noul question, does per-item decision quality change relative to K = 1?

Packing mixes two effects: a larger state (distraction) and more questions (fan-out). To separate them, Study 1 includes an arm that packs K passages but asks about only one of them.

### Arms

| Arm | State | Questions | Purpose |
|---|---|---|---|
| `atomic` | 1 query + 1 passage | 1 | Control: fresh K = 1 |
| `pack-K` | 1 query + K passages from that query | K, one per passage | Realistic packed filtering |
| `pack-K-single` | 1 query + K passages | 1, for a target passage | Isolates state distraction from fan-out |
| `pack-all` | 1 query + all 100 candidates | 100 | Maximal realistic pack (whole candidate list) |
| `cross-K` | K independent (query, passage) pairs from different queries | K | Packing unrelated tasks together |

- **Grid:** K ∈ {2, 4, 8, 16, 32}.
- **Pack assignment:** each query's 100 candidates are shuffled with a seed and split into groups of K. We use two independent permutations, so each item appears in each K in two different positions with two different neighbor sets. Packs follow neither BM25 rank order nor qrels.
- **`pack-K-single`:** runs on a seeded subset of queries at K ∈ {8, 32}. Every target in the subset is covered.
- **`cross-K`:** runs at K ∈ {4, 16}, with each item's query stored beside its passage.

**Estimated size, before the budget check:** 9,700 items × 5 K values × 2 permutations = 97,000 packed decisions, plus about 29,000 for the other arms and the noise floor. The subset arms are sized at protocol freeze.

### Secondary manipulations (K = 8, seeded subset of judged targets)

- **Neighbor relevance:** each target is packed with seven judged neighbors from its own query, either all relevant (grade ≥ 2) or all non-relevant (grade 0).
- **Reference style:**
  - index: `passages[3]`;
  - named key: `passages.p04` (the primary style, following the docs' "identify the relevant parts of state by name");
  - quoted ID: "the passage whose `id` is `8412345`".
- **Swap test:** the same pack in reverse order. The score should follow the item, not the slot.

### Noise floor

Before comparing arms, measure how much a single decision varies. There are three measurements:

- **Fresh atomic baseline:** run `atomic` fresh for all 9,700 items, interleaved with the packed arms. This is the primary control.
- **Test–retest:** run `atomic` a second time on a seeded subset.
- **Drift check:** compare the fresh atomic run with the frozen `trec-v1` `jev-noul` scores.

Packed-minus-atomic differences are read against the test–retest difference, not against zero.

## Study 2: question fan-out

**Question:** Does a question's answer change when other questions are asked in the same request? The docs claim independent evaluation, so the null hypothesis is equality up to test–retest noise.

The state is always 1 query + 1 passage.

| Arm | Questions per request | Contents |
|---|---|---|
| `solo` | 1 | Each question alone |
| `ordinal-joint` | 3 | Frozen `jev-ordinal`: related, useful, direct |
| `fan-N-related` | N | The target `useful` question plus N−1 other relevance-flavored questions |
| `fan-N-unrelated` | N | The target plus N−1 speculative questions unrelated to relevance (for example tone, language, or whether the passage lists a date) |

- **Grid:** N ∈ {3, 8, 16}.
- **Question pools:** the related and unrelated pools are written and frozen from documentation and synthetic examples only.
- **Order:** within a request, questions go into the JSON object in two seeded key orders. This checks for position effects in case key order is not actually irrelevant.
- **Coverage:** `solo` runs every question in every pool, so each co-asked answer has an atomic counterpart.

**Estimated size, before the budget check:** 9,700 items for `solo` on the three ordinal questions, plus the fan-out arms on a seeded query subset sized at protocol freeze.

**Study-specific measurement: logical consistency.** Rates at which the ordinal chain is violated (`direct > useful` or `useful > related`) in the solo versus the joint arm. Does asking the questions together make the answers more coherent?

## Study 3: position in state

**Question:** Does the same evidence get more or less weight depending on where it sits in the `state` value?

**Setup:**
- Every judged BM25 candidate of the 20-query subset is a target (1,202 targets).
- Each target gets 39 fixed filler passages, drawn with a seed from *other* queries' candidates, so they are off-topic for the target's question. The 10-passage haystack uses the first 9 fillers.
- The target moves through five relative positions (start, 25%, middle, 75%, end) of a `passages` array of `{id, text}` objects. Fillers and opaque ids stay fixed, so only the target's slot changes.

**Arms:**

| Arm | Question | Purpose |
|---|---|---|
| `pointed-M-pos` | Frozen relevance prompt about the passage with the target's opaque id | Does a pointed question still depend on position? |
| `pointed-qfirst-40-pos` | Same, with the `question` field before `passages` | Separates position within `passages` from distance to the question |
| `needle-M-pos` | "Does any passage in `passages` contain useful evidence…" | Unpointed search: does the model find the evidence equally well everywhere? |
| `single-passage-first` / `single-question-first` | Frozen one-passage request, and the same with field order reversed | Field order at the smallest scale |

M ∈ {10, 40}.

**Measurements:**
- AUC and ECE per position, plus mean p for relevant and non-relevant targets.
- Δ AUC against the middle position.
- Paired shift in the same relevant target's probability between the middle and each other position.
- Δp against the single-passage request.

Everything is reported with 95% query-cluster bootstrap intervals.

## Measurements

These apply to both studies unless noted. Item-level analyses use judged items only. Ranking metrics treat unjudged items as zero gain, following `trec_eval`.

- **Ranking quality:** per-query nDCG@10 (linear gains, verified with pinned `trec_eval`) using each item's score from its arm. In packed arms, scores from different packs are merged into one ranking, which is how packing would be used in practice. Also MRR@10 and recall at grade ≥ 2.
- **Discrimination and calibration:** AUC, Brier score, and ECE (10 equal-mass bins) against qrels binarized at grade ≥ 2. Calibration matters because calibrated probabilities are Jev's main claimed advantage.
- **Item drift:** the distribution of |p_arm − p_atomic| compared with test–retest |p_atomic − p_atomic′|.
- **Context effects (Study 1):** a mixed model `logit(p) ~ K + position + mean_neighbor_grade + (1 | item) + (1 | query)`. Also the neighbor-relevance contrast, the swap-test agreement rate, and the accuracy of each reference style.
- **Resources:** billed input tokens per decision, requests per decision, request latency p50/p95, and whole-query wall time at a declared concurrency (16 workers). Also sustained decisions per minute under the documented 1,200 requests-per-minute and 250k tokens-per-second limits.
  - Prediction: each question carries its own instructions, so packing should cut requests by about K× while leaving tokens per decision roughly flat. We measure this rather than assume it.

## Statistics

- Primary contrasts per study:
  - Study 1: nDCG@10 of each `pack-K` arm minus `atomic`.
  - Study 2: each fan-out arm minus `solo`, on the `useful` question.
- Differences are paired per query, with 95% percentile bootstrap intervals: 10,000 replicates, seed 20260918, stratified by year. Queries, not passages, are the resampling unit.
- Holm correction applies across each study's primary family if any significance claim is made. Everything else is labeled exploratory.
- No non-inferiority margin is claimed unless it is frozen in the protocol before results.

## Execution

- **CLI:** a Go CLI in the style of `jev-vs-rerankers`, with separate `prepare`, `smoke`, `run`, `status`, `report`, and `audit` commands. Only `smoke` and `run` call the API.
- **Before benchmark inference:** a synthetic smoke test for each study, in its own output directory.
- **Scheduling:** work is interleaved by query, and the order of arms rotates by query, to spread out time-of-day and drift effects.
- **Retries:** at most two, and only after transport, 408, 429, or 5xx failures. Every attempt is kept. Malformed successful responses stop the run.
- **Evidence:** reservation-before-dispatch, atomic receipts, and resume that makes no duplicate calls. The audit replays every score from the receipts.
- **Cost:** at $0.042 per million input tokens, both studies together are expected to cost a few dollars at most. The proposed conservative reservation ceiling is $20, with smoke costs reported separately. Rate limits, not money, set the pace.

## Publication boundary

Work happens in `decision-model-testing-private`. The public export may include:
- code and prompts;
- the wrapper and question pools;
- synthetic smoke payloads;
- query and passage IDs;
- aggregate and per-query metrics;
- per-item Jev probabilities keyed by passage ID, if their redistribution is confirmed.

Passage text, full request bodies, and receipts containing source text stay private under MS MARCO terms, the same as in `jev-vs-rerankers`.

## Protocol v1 as run

Frozen on 2026-09-25, before any benchmark outcome was seen. Source: `cmd/*/main.go` and `internal/bench`.

**Where this differs from the design above:**
- **Pack sizes:** Study 1 uses K ∈ {2, 4, 10, 20, 50, 100}. These divide 100, so every pack is full; 8, 16 and 32 would have left a short remainder pack.
- **Retries:** up to four attempts per request, for transport errors, 408, 429 and 5xx, with Retry-After honored and every attempt kept.
- **Context-effect analysis:** reported descriptively, by pack fifth and by the controlled neighbor contrast. The mixed model was not fitted.
- **Bootstrap replicates:** nDCG uses 10,000. Item-level AUC and paired-shift intervals use 2,000 to keep reporting fast.
- **Subset:** the 20-query subset (10 per year, seed `20260925`) is shared by Study 1's retest and `single-K` arms and by Studies 2 and 3.

**Study 1 arms:**
- `atomic`: all 9,700 items, byte-identical to the frozen `jev-noul` request. This is verified by test against the frozen receipts.
- `retest`: the subset, repeated.
- `pack-K-p1` and `pack-K-p2`: two seeded permutations, named keys `passages.pNNN`.
- `swap-10`: `pack-10-p1` reversed.
- `ref-index-10` and `ref-id-10`: index and id reference styles.
- `single-10` and `single-50`: subset, one question per request.
- `cross-4` and `cross-20`: distinct queries per pack.
- `neighbor-rel` and `neighbor-non`: K = 5, at most 10 relevant and 10 non-relevant targets per eligible query, four same-query neighbors each, same seeded slot.

**Study 2 arms:** 2,000 items.
- `solo-useful`: byte-identical to frozen `jev-noul`.
- `retest-useful`, `solo-related`, `solo-direct`.
- `ordinal-joint`: byte-identical to frozen `jev-ordinal`.
- `fan-N-rel-first` and `fan-N-unrel-first` for N ∈ {3, 8, 16}, plus `fan-16-rel-last` and `fan-16-unrel-last`.
- The 15-question pools are in `cmd/question-fanout/main.go`.

**Study 3 arms:** as in the table above.

**Execution:**
- `jev-1.13.0`, 16 workers, a client-side limit of 1,000 requests per minute, jobs in seeded interleaved order across arms, run one study at a time.
- Conservative budget ceilings: $15, $8 and $25. These bound reservations; they are not expected costs.

**Reproduce:** `go run ./cmd/<study> prepare|plan|smoke|run|status|report`.

## Hypotheses

These are predictions, not results.

- **H1:** For `pack-K`, quality falls as K grows. The drop is within the noise floor at K ≤ 8 and measurable at K ≥ 32 and in `pack-all`.
- **H2:** `pack-K-single` explains most of any `pack-K` loss. That is, distraction from a larger state matters more than question fan-out.
- **H3:** Neighbor relevance shifts target scores. The direction is not predicted in advance: packing could inflate targets toward their neighbors or push them away by contrast.
- **H4:** Named keys match or beat quoted IDs, and both beat bare indices.
- **H5:** `cross-K` degrades more than `pack-K` at the same K, because each question has to find its own query as well as its own passage.
- **H6:** Study 2's fan-out answers match `solo` within noise, as the docs claim. If a difference shows up, it is larger with unrelated questions than with related ones.

## Hypotheses for Study 3

- **H7:** Pointed questions are insensitive to position within the noise floor.
- **H8:** Needle questions lose sensitivity toward the middle of the long (40-passage) state, a "lost in the middle" pattern.

## Remaining decisions

1. Whether to add a second task, such as LLMBar pairwise judging from `jev-vs-LLM-for-evals`, to test whether the findings generalize.
2. Whether Kyle wants to review or replace Study 2's question pools (drafted by Claude from the documentation) for a v2 run.

## Limitations

- One model version and one task family at first.
- TREC DL passages are short, so the results say little about long-chunk states like the EnronQA ones.
- Hosted-model drift, rounded probabilities, possible training contamination, and small query sets.
- Treating unjudged items as zero gain affects the ranking metrics.
- Controlled-neighbor packs are artificial by design.
