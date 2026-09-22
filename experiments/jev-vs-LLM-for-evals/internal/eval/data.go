package eval

import (
	"fmt"
	"github.com/parquet-go/parquet-go"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

func Prepare(root string) (err error) {
	defer Recover(&err)
	base := filepath.Join(root, "methodology")
	lock := obj(ReadJSON(filepath.Join(base, "sources.lock.json")))
	client := &http.Client{Timeout: 90 * time.Second}
	files := obj(lock["files"])
	for _, name := range Keys(files) {
		spec := obj(files[name])
		path := filepath.Join(base, ".cache", name)
		if b, e := os.ReadFile(path); e == nil && Hash(b) == spec["sha256"] {
			continue
		}
		response, e := client.Get(str(spec["url"]))
		check(e)
		if response.StatusCode != 200 {
			response.Body.Close()
			panic(fmt.Errorf("source download HTTP %d", response.StatusCode))
		}
		b, e := io.ReadAll(response.Body)
		response.Body.Close()
		check(e)
		if Hash(b) != spec["sha256"] {
			panic(fmt.Errorf("source hash mismatch %s", name))
		}
		check(os.MkdirAll(filepath.Dir(path), 0755))
		check(os.WriteFile(path+".tmp", b, 0644))
		check(os.Rename(path+".tmp", path))
	}
	for name, wanted := range obj(lock["vendor_sha256"]) {
		if Hash(Read(filepath.Join(base, name))) != wanted {
			panic(fmt.Errorf("vendor hash mismatch %s", name))
		}
	}
	return
}

type rewardRow struct {
	ID               string   `parquet:"id"`
	Prompt           string   `parquet:"prompt"`
	Chosen           []string `parquet:"chosen,list"`
	Rejected         []string `parquet:"rejected,list"`
	Subset           string   `parquet:"subset"`
	NumCorrect       int64    `parquet:"num_correct"`
	TotalCompletions int64    `parquet:"total_completions"`
}

func Load(root, benchmark string) []M {
	cache := filepath.Join(root, "methodology/.cache")
	rows := []M{}
	switch benchmark {
	case "llmbar":
		base := filepath.Join(cache, "llmbar/Dataset/LLMBar")
		check(filepath.WalkDir(base, func(path string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if d.IsDir() || d.Name() != "dataset.json" {
				return nil
			}
			subset, e := filepath.Rel(base, filepath.Dir(path))
			check(e)
			for i, v := range arr(ReadJSON(path)) {
				r := obj(v)
				rows = append(rows, M{"id": fmt.Sprintf("%s:%d", subset, i), "subset": subset, "input": r["input"], "output_1": r["output_1"], "output_2": r["output_2"], "label": str(r["label"])})
			}
			return nil
		}))
	case "judgebench":
		files, e := filepath.Glob(filepath.Join(cache, "judgebench/data/*.jsonl"))
		check(e)
		for _, file := range files {
			split := "claude"
			if strings.Contains(file, "gpt-4o") {
				split = "gpt"
			}
			for _, r := range Lines(file) {
				source := str(r["source"])
				category := source
				for _, c := range []string{"mmlu-pro", "livebench-reasoning", "livebench-math", "livecodebench"} {
					if strings.HasPrefix(source, c) {
						category = c
						break
					}
				}
				label := map[string]string{"A>B": "1", "B>A": "2"}[str(r["label"])]
				if label == "" {
					panic("unknown gold label")
				}
				rows = append(rows, M{"id": split + ":" + str(r["pair_id"]), "subset": split + "/" + category, "split": split, "source": source, "input": r["question"], "output_1": r["response_A"], "output_2": r["response_B"], "label": label})
			}
		}
	case "rewardbench2":
		rs, e := parquet.ReadFile[rewardRow](filepath.Join(cache, "rewardbench2_data/test.parquet"))
		check(e)
		for _, r := range rs {
			chosen, rejected := []any{}, []any{}
			for _, s := range r.Chosen {
				chosen = append(chosen, s)
			}
			for _, s := range r.Rejected {
				rejected = append(rejected, s)
			}
			if len(chosen) != int(r.NumCorrect) || len(chosen)+len(rejected) != int(r.TotalCompletions) {
				panic("invalid reward row")
			}
			rows = append(rows, M{"id": r.ID, "prompt": r.Prompt, "chosen": chosen, "rejected": rejected, "subset": r.Subset, "num_correct": float64(r.NumCorrect), "total_completions": float64(r.TotalCompletions)})
		}
	default:
		panic(fmt.Errorf("unknown benchmark %s", benchmark))
	}
	if len(rows) != map[string]int{"llmbar": 419, "judgebench": 620, "rewardbench2": 1865}[benchmark] {
		panic(fmt.Errorf("wrong %s row count %d", benchmark, len(rows)))
	}
	return rows
}

// Versioned SHA-256 sampling avoids runtime-dependent PRNG sequences. Frozen jobs can
// be supplied to reproduce historical Python selections exactly.
func Select(rows []M, per int, seed int) []M {
	if per == 0 {
		return rows
	}
	if per < 0 {
		panic("per_subset must be nonnegative")
	}
	groups := map[string][]M{}
	for _, r := range rows {
		groups[str(r["subset"])] = append(groups[str(r["subset"])], r)
	}
	names := []string{}
	for k := range groups {
		names = append(names, k)
	}
	sort.Strings(names)
	selected := []M{}
	for _, subset := range names {
		group := groups[subset]
		units := map[string][]M{}
		for _, r := range group {
			id := str(r["id"])
			if subset == "Ties" {
				_, id, _ = strings.Cut(id, ":")
			}
			units[id] = append(units[id], r)
		}
		ids := []string{}
		for id := range units {
			ids = append(ids, id)
		}
		rank := func(id string) string { return Hash(Canon([]any{"sha256-v1", seed, subset, id})) }
		sort.Slice(ids, func(i, j int) bool { return rank(ids[i]) < rank(ids[j]) })
		if per < len(ids) {
			ids = ids[:per]
		}
		for _, id := range ids {
			selected = append(selected, units[id]...)
		}
	}
	return selected
}
func Plan(root string, config M) []M {
	jobs := []M{}
	benchmarks := obj(config["benchmarks"])
	for _, b := range Keys(benchmarks) {
		rows := Select(Load(root, b), int(num(config["per_subset"])), int(num(config["seed"])))
		for _, row := range rows {
			var position any
			if b == "rewardbench2" {
				position = int(ReadHashByte(Canon([]any{config["seed"], row["subset"], row["id"]}))) % 4
			}
			for _, v := range arr(benchmarks[b]) {
				m := str(v)
				if b == "llmbar" {
					if !nativeMethod(m) {
						Read(filepath.Join(root, "methodology/vendor/llmbar/configs", m+".json"))
					}
				} else if !(b == "judgebench" && (m == "vanilla" || m == "arena_hard") || b == "rewardbench2" && (m == "fourway" || m == "ratings")) {
					panic(fmt.Errorf("invalid method %s", m))
				}
				for _, model := range arr(config["models"]) {
					id := []any{b, m, model, row["subset"], row["id"]}
					jobs = append(jobs, M{"key": Hash(Canon(id))[:24], "benchmark": b, "method": m, "model": model, "row": row, "position": position})
				}
			}
		}
	}
	return jobs
}
func ReadHashByte(b []byte) byte {
	h := Hash(b)
	var n byte
	_, e := fmt.Sscanf(h[:2], "%02x", &n)
	check(e)
	return n
}
