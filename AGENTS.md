---
title: Repository instructions
type: agent-instructions
---

# Repository instructions

## Private workspace, public release

This research uses two repositories:

- [`dorkitude/decision-model-testing-private`](https://github.com/dorkitude/decision-model-testing-private) (private) is the working archive. Do all experiment design, code, live runs, iterative commits, raw receipts, datasets, and source-bearing evidence here. Start new experiments here.
- [`dorkitude/decision-model-testing`](https://github.com/dorkitude/decision-model-testing) (public) is the curated release. It receives only reviewed exports of completed experiments, committed with clean, fresh history.

Rules:

- Never push, merge, or rebase private history into the public repository, add one as a remote of the other, or change either repository's visibility.
- Publish an experiment only when it is complete: add its files to [`publication/allowlist.json`](publication/allowlist.json), add reviewed overlays for anything that needs rewriting, run the exporter and audit described in [`PUBLIC_EXPORT.md`](PUBLIC_EXPORT.md), review the candidate, and commit it to the public repository as a clean commit.
- Exclude underlying data unless redistribution rights are confirmed in writing: datasets, source text, source-derived answers or chunks, full request/response receipts, and third-party templates or provider outputs with unclear terms. Publish code, original prompts, synthetic tests, aggregate measurements, and acquisition instructions so readers can reproduce the work from upstream sources.
- Record redistribution questions in this repository's issues before export.
- If the public repository is edited directly, bring those edits back into the private source or its overlay before the next export so they are not overwritten.

## Structure and navigation

- Put every experiment in its own descriptive folder under `experiments/`.
- Keep the root `README.md` short. Link every experiment from it, with a one-line research question. Update the index whenever an experiment is added, renamed, or removed.
- Each experiment owns its README, code, configuration, tests, analysis, and results. Run its commands from that experiment directory unless its documentation says otherwise.
- Keep repository-wide instructions, licensing, contribution guidance, and CI at the root. Do not introduce cross-experiment dependencies without documenting them.

## Documentation

- Write all maintained documentation as `.md` files with YAML frontmatter. Include at least `title` and `type`; add dates, authors, sources, or experiment identifiers when useful. Update any existing maintained document to this convention when editing it.
- READMEs explain the research question, results, methods, reproduction, and limitations to a first-time reader. Keep progress logs and task status in linked GitHub issues, not README diaries.
- Frozen study outputs, upstream source artifacts, and their provenance manifests are immutable evidence. Preserve their original bytes and document their location instead of adding frontmatter that invalidates hashes. Newly authored documentation must follow the rule above; do not use the evidence exception for ordinary docs.
- HTML tables, plots, JSON/CSV data, source code, standard `LICENSE` files, and other executable or machine-readable artifacts are not prose documentation. Explain them in an adjacent Markdown document with frontmatter. Keep HTML table designs self-contained and use the Tokyo Night palette.
- Use **Kyle Wild** in research authorship and professional materials.

## Research and evidence

- Separate hypotheses, benchmark agreement, model confidence, and independently verified correctness. Report negative results and service failures.
- State the benchmark, method, sample, aggregation, retry policy, and resource units beside reported results. Distinguish list-price accounting estimates from invoices, and measured latency from simulated routing time.
- Preserve source revisions, licenses, frozen plans, raw receipts, hashes, and original scores. New analyses go into new output paths; never silently rewrite a published experiment.
- Keep held-out outcomes out of prompt/threshold selection. Make live API runs explicit, bounded, and resumable; do not launch paid inference for documentation or CI work.

## Maintenance

- Use this repository's GitHub issues for actionable work, including the source request or decision context. Link status summaries to those issues and close them when complete.
- Keep the research ideas log in GitHub issues labeled `idea`, one distinct experiment idea per issue. Include the source request, hypothesis, candidate approaches, and evaluation questions; link related ideas and search for duplicates first. Ideas are proposals, not scheduled runs or spending commitments. Do not maintain a separate unlinked ideas document or create experiment folders until work starts.
- Keep credentials in environment variables or ignored local files. Do not commit tokens, authorization headers, personal coordination, or unrelated private materials. Inspect compressed evidence as well as plain files before publication.
- Preserve upstream attribution. A project license does not relicense datasets, provider outputs, or third-party templates. Record unresolved redistribution questions before a public release.
- For Go changes, run the experiment's tests and `go vet ./...`; run `prepare` first when verifying official-data/replay integration. CI must not use model-provider credentials or perform inference.
- Keep CI paths, module imports, relative links, and root index entries aligned with directory changes. Check Markdown frontmatter and internal links after documentation changes.
- Commit scoped completed work and push to the available remote branch. Do not change repository visibility merely as a side effect of preparing a release.
