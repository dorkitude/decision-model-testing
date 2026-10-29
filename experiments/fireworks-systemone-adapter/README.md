---
title: Fireworks System One adapter
type: public-release-documentation
author: Kyle Wild
---

# Fireworks System One adapter

[Rune](https://fireworks.ai/models/fireworks/rune) (`accounts/fireworks/routers/rune`) accepts Jev-compatible `state` and typed `questions`. The client preserves their bytes and order, sets `model`, validates limits, records preparation edits and hashes, normalizes answers and records usage and failures. The fake server and tests run offline.

```sh
go test -race ./...
go vet ./...
```

For a live probe (one paid request), set `FIREWORKS_SYSTEMONE_MODEL=accounts/fireworks/routers/rune` and run `go run ./cmd/fwsystemone-probe --smoke`. On exe.dev the default endpoint is the keyless Fireworks integration. Elsewhere set `FIREWORKS_SYSTEMONE_URL=https://api.fireworks.ai/inference/v1/systemone` and `FIREWORKS_API_KEY` in the environment. No credential file is loaded.

The study's dated accounting rate is $0.042 per million input tokens, including cached input, with zero reported output tokens. Set `FIREWORKS_USD_PER_MTOK_INPUT=0.042` and `FIREWORKS_USD_PER_MTOK_CACHED_INPUT=0.042`. The client's $1/Mtok default is a labeled placeholder, not a current price. Configured rates must be positive; the output-rate setting has no effect on these runs' zero output usage. Context packing encountered a 16,384-token service limit; do not infer model limits from pre-model schema validation.

To reproduce the comparison, acquire each study's upstream inputs under its terms, use its frozen sample and prompt, and send the planned Jev body through `fwsystemone.Prepare`, changing only `model`. Each study owns its scoring; this shared adapter carries no dataset or experiment-specific orchestration. Full original receipts and source-bearing answers are excluded.
