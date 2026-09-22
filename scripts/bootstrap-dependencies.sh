#!/bin/sh
set -eu
clone_at() {
  url=$1; revision=$2; destination=$3
  if [ ! -e "$destination" ]; then
    mkdir -p "$(dirname "$destination")"
    git clone --no-checkout "$url" "$destination"
    git -C "$destination" checkout --detach "$revision"
  fi
  test "$(git -C "$destination" rev-parse HEAD)" = "$revision"
}
clone_at https://github.com/dorkitude/EnronQA-cli.git 7e817f722d74e008f58b7ad4dcf13ff5c56b4215 experiments/jev-search-result-narrower/EnronQA-cli
clone_at https://github.com/dorkitude/EnronQA-cli.git 7e817f722d74e008f58b7ad4dcf13ff5c56b4215 experiments/jev-search-result-narrower-claude-RAG/EnronQA-cli
clone_at https://github.com/usnistgov/trec_eval.git ba38899cbd4de0fb699b47f39b64ef1c107e4a5c experiments/jev-vs-rerankers/third_party/trec_eval
