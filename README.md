---
title: Decision Model Testing
type: public-release-documentation
---

# Decision Model Testing

## Intro

General-purpose decision models (TypeSafe Jev, and the *many* more to follow) are a game changer.  They work like a fuzzy switch or a fuzzy scoring system that can be driven by natural language prompts (hence "fuzzy"), which is a significant part of how we use LLMs today in the knowledge and context engineering world.  LLMs are quite expensive for this kind of stuff!

But my question was, how good is Jev really?  Where is it already useful?  How can we begin empirically proving where it should replace LLMs today, and where future versions should replace LLMs tomorrow?

I've been experimenting daily to answer these questions for myself.  Many of my experiments are on private data, but not all.  This repo is where I'll share the latter set.

## Get to it already.

This repo contains exports of my experiments with general-purpose decision models (i.e. TypeSafe Jev).  Mostly I'm comparing them to the performance and cost of LLMs, NLP models, and specialized decision models (rerankers, etc.).

I'm not packing all the data into this public repo due to licensing questions, but my goal is to make reproduction straightforward.  The datasets are all freely available and I've included links to them where possible.

Each experiment is a self-contained subfolder of `experiments/`:

- [Jev vs. LLM judges for evals](experiments/jev-vs-LLM-for-evals/README.md):  a sort of eval-of-evals, weighing Jev against a variety of open models for typical eval usecases.
- [Kimi RAG filtering](experiments/jev-search-result-narrower/README.md): can Jev reduce answering costs?
- [Claude RAG filtering](experiments/jev-search-result-narrower-claude-RAG/README.md): probability and categorical filtering before Claude.
- [Jev vs. rerankers](experiments/jev-vs-rerankers/README.md): passage ordering against human relevance judgments.
- [Jev to narrow web search results](https://github.com/dorkitude/webctl): have Jev trim search results based on the goal of the original query;  and have it trim the content too.  This one got out of hand, and I realized it was immediately useful to others anyway, so I made it into a reusable CLI (MIT-licensed)

This distribution contains code, original prompts, synthetic tests and aggregate measurements. Datasets, source-bearing answers/receipts, archives, private correspondence and prior Git history are excluded.

See [reproduction and limits](REPRODUCTION.md) and [third-party notices](THIRD_PARTY_NOTICES.md).
