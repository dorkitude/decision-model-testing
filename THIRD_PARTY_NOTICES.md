---
title: Third-party notices
type: public-release-documentation
---

# Third-party notices

The MIT license covers this project's original code, not datasets, model outputs or third-party templates. No EnronQA email, benchmark question, reference answer, retrieved passage or source-derived model answer is distributed here. Access original material from its upstream host under its terms.

- EnronQA: [dataset](https://huggingface.co/datasets/MichaelR207/enron_qa_0922), revision `c0b3a9190fd970e83cfbe7d399a08860e43e221e`; [EnronQA CLI](https://github.com/dorkitude/EnronQA-cli), revision `7e817f722d74e008f58b7ad4dcf13ff5c56b4215`.
- LLMBar: Princeton NLP; bundled prompt/config adapters preserve the upstream MIT license.
- RewardBench: AllenAI; bundled prompt/reference adapters preserve the upstream Apache-2.0 license.
- JudgeBench: ScalerLab. The source lock records the upstream revision and download hashes; no JudgeBench runner or dataset is bundled. Limited permission for static rendered template evidence does not grant a general code or dataset license.
- TREC DL: NIST relevance judgments and Castorini RankLLM candidates are downloaded locally; `data.go` records URLs, revisions and hashes. NIST trec_eval is acquired separately.

Third-party prompt/config files retain their original extensions and bytes; they are runtime source artifacts, not maintained prose documentation. No upstream project or author endorses this study.
