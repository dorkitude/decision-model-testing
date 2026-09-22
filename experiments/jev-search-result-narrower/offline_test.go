package main

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"math/big"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"
)

func TestPriceTokens(t *testing.T) {
	ri := big.NewRat(3, 1)
	rc := big.NewRat(3, 10)
	ro := big.NewRat(15, 1)
	if got := decimal(priceTokens(1000000, 100000, 1000, ri, rc, ro)); got != "2.745" {
		t.Fatal(got)
	}
	if got := decimal(priceTokens(1392983, 0, 0, big.NewRat(42, 1000), big.NewRat(42, 1000), new(big.Rat))); got != "0.058505286" {
		t.Fatal(got)
	}
}
func TestTokenUsageRejectsMissingAndInvalid(t *testing.T) {
	for _, u := range []any{nil, M{}, M{"prompt_tokens": 10.0, "completion_tokens": 1.0, "prompt_tokens_details": M{"cached_tokens": "oops"}}, M{"prompt_tokens": 10.0, "completion_tokens": 1.0, "completion_tokens_details": M{"reasoning_tokens": "oops"}}, M{"prompt_tokens": 10.0, "completion_tokens": 1.0, "prompt_tokens_details": M{"cached_tokens": 11.0}}, M{"input_tokens": 2.5, "output_tokens": 0.0}, M{"input_tokens": 2.0, "output_tokens": 1.0, "completion_tokens_details": M{"reasoning_tokens": 2.0}}} {
		if _, e := tokenUsage(M{"id": "test", "response": M{"usage": u}}); e == nil {
			t.Fatalf("accepted invalid usage %#v", u)
		}
	}
	u, e := tokenUsage(M{"response": M{"usage": M{"prompt_tokens": 10.0, "completion_tokens": 3.0, "completion_tokens_details": M{"reasoning_tokens": 2.0}}}})
	if e != nil || u["output_tokens"] != 3.0 {
		t.Fatal(u, e)
	}
}
func TestReadKeyedRejectsDuplicateAndNull(t *testing.T) {
	for _, s := range []string{"{\"id\":\"x\"}\n{\"id\":\"x\"}\n", "null\n"} {
		p := filepath.Join(t.TempDir(), "records.jsonl")
		os.WriteFile(p, []byte(s), 0600)
		if _, e := readKeyed(p, "id"); e == nil {
			t.Fatal("accepted invalid records")
		}
	}
}
func TestArchiveValidation(t *testing.T) {
	for _, name := range []string{"../escape", "/absolute", "root/run/../../../../escape"} {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			archive := filepath.Join(dir, "input.tar.gz")
			f, e := os.Create(archive)
			if e != nil {
				t.Fatal(e)
			}
			gz := gzip.NewWriter(f)
			tw := tar.NewWriter(gz)
			if e = tw.WriteHeader(&tar.Header{Name: name, Mode: 0600, Size: 1}); e != nil {
				t.Fatal(e)
			}
			tw.Write([]byte("x"))
			tw.Close()
			gz.Close()
			f.Close()
			h, _ := fileHash(archive)
			meta := filepath.Join(dir, "meta.jsonl")
			jsonFile(meta, M{"sha256": h})
			_, clean, _, e := openArchive(archive, meta)
			clean()
			if e == nil {
				t.Fatal("accepted unsafe archive")
			}
		})
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "archive")
	os.WriteFile(p, []byte("not tar"), 0600)
	meta := filepath.Join(dir, "meta.jsonl")
	jsonFile(meta, M{"sha256": "wrong"})
	_, _, _, e := openArchive(p, meta)
	if e == nil || !strings.Contains(e.Error(), "SHA-256") {
		t.Fatal(e)
	}
}
func TestDerivedOutputNeverOverwrites(t *testing.T) {
	dir := t.TempDir()
	sentinel := filepath.Join(dir, "summary.jsonl")
	os.WriteFile(sentinel, []byte("frozen"), 0600)
	if e := writeDerived(dir, map[string][]M{"summary": {{"id": "new"}}}); e == nil {
		t.Fatal("overwrote output")
	}
	b, _ := os.ReadFile(sentinel)
	if string(b) != "frozen" {
		t.Fatal("mutated existing file")
	}
}
func TestOfflineCommandsDoNotInitializeRun(t *testing.T) {
	dir := t.TempDir()
	run := filepath.Join(dir, "must-not-exist")
	root := &cobra.Command{Use: "test", SilenceUsage: true, SilenceErrors: true}
	root.PersistentFlags().StringVar(&run, "run", run, "")
	addOfflineCommands(root, &run)
	root.SetOut(new(bytes.Buffer))
	root.SetArgs([]string{"audit", "--data", filepath.Join(dir, "absent")})
	if e := root.Execute(); e == nil || !strings.Contains(e.Error(), "--source") {
		t.Fatal(e)
	}
	if _, e := os.Stat(run); !os.IsNotExist(e) {
		t.Fatalf("created run directory: %v", e)
	}
}

// Opt in to a full saved-evidence replay; no credentials or provider calls.
func TestFrozenOfflineReplay(t *testing.T) {
	if os.Getenv("JEV_TEST_ARCHIVE") == "" {
		t.Skip("set JEV_TEST_ARCHIVE to the frozen evidence tar.gz")
	}
	before := map[string]string{}
	for _, dir := range []string{"results/run-v1", "results/cost-estimate-v1"} {
		e := filepath.WalkDir(dir, func(p string, d os.DirEntry, e error) error {
			if e != nil {
				return e
			}
			if !d.IsDir() {
				h, e := fileHash(p)
				if e != nil {
					return e
				}
				before[p] = h
			}
			return nil
		})
		if e != nil {
			t.Fatal(e)
		}
	}
	base, clean, digest, e := openArchive(os.Getenv("JEV_TEST_ARCHIVE"), "results/run-v1/archive.jsonl")
	if e != nil {
		t.Fatal(e)
	}
	defer clean()
	run := filepath.Join(base, "run")
	result, e := auditEvidence(run, filepath.Join(base, "data"), filepath.Join(base, "source"))
	if e != nil || result["passed"] != true {
		t.Fatal(result, e)
	}
	_, files, e := estimateCost(run, "results/cost-estimate-v1/rates.jsonl", "results/run-v1/summary.jsonl", digest)
	if e != nil {
		t.Fatal(e)
	}
	for _, name := range []string{"model_costs", "summary", "request_usage"} {
		expected, e := readKeyed("results/cost-estimate-v1/"+name+".jsonl", "id")
		if e != nil {
			t.Fatal(e)
		}
		if len(expected) != len(files[name]) {
			t.Fatalf("%s count", name)
		}
		for _, row := range files[name] {
			old := expected[str(row["id"])]
			if old == nil {
				t.Fatalf("missing %v", row["id"])
			}
			for field, want := range old {
				got := row[field]
				if strings.Contains(field, "usd") || field == "savings_percent" {
					a, oka := new(big.Rat).SetString(str(got))
					b, okb := new(big.Rat).SetString(str(want))
					if !oka || !okb || a.Cmp(b) != 0 {
						t.Fatalf("cost mismatch %s/%v/%s: %v != %v", name, row["id"], field, got, want)
					}
				} else if !same(got, want) {
					t.Fatalf("usage mismatch %s/%v/%s: %v != %v", name, row["id"], field, got, want)
				}
			}
		}
	}
	// Corrupt the frozen source after a successful audit to prove it is checked.
	source := filepath.Join(base, "source", "main.go")
	f, e := os.OpenFile(source, os.O_APPEND|os.O_WRONLY, 0600)
	if e != nil {
		t.Fatal(e)
	}
	f.WriteString("\n// corruption\n")
	f.Close()
	if _, e = auditEvidence(run, filepath.Join(base, "data"), filepath.Join(base, "source")); e == nil || !strings.Contains(e.Error(), "source hash") {
		t.Fatalf("accepted changed source: %v", e)
	}
	for p, h := range before {
		after, e := fileHash(p)
		if e != nil || after != h {
			t.Fatalf("mutated frozen file %s", p)
		}
	}
}
