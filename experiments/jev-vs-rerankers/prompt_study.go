package main

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

//go:embed prompts.json
var promptBytes []byte
var promptVariants = func() map[string]M {
	var v map[string]M
	if e := json.Unmarshal(promptBytes, &v); e != nil {
		panic(e)
	}
	return v
}()
var baselineMethods = []string{"qwen", "jev-noul", "jev-grade", "jev-ordinal"}

func baseMethod(method string) string {
	if _, ok := promptVariants[method]; ok {
		return strings.Join(strings.Split(method, "-")[:2], "-")
	}
	return method
}
func selectStudy(study string) error {
	methods = append([]string{}, baselineMethods...)
	switch study {
	case "initial":
	case "prompts":
		names := []string{}
		for m := range promptVariants {
			names = append(names, m)
		}
		sort.Strings(names)
		methods = append(methods, names...)
	default:
		return fmt.Errorf("unknown study %q", study)
	}
	return nil
}
func seedBaseline(from, out string) error {
	if len(methods) != 13 {
		return fmt.Errorf("seed-baseline requires --study prompts")
	}
	unlock, e := lockDir(out)
	if e != nil {
		return e
	}
	defer unlock()
	if _, e := os.Stat(filepath.Join(out, "manifest.json")); !os.IsNotExist(e) {
		return fmt.Errorf("seed only a new output directory")
	}
	var manifest M
	if e := readJSON(filepath.Join(from, "manifest.json"), &manifest); e != nil {
		return e
	}
	var audit M
	if e := readJSON(filepath.Join(from, "audit.json"), &audit); e != nil {
		return e
	}
	if audit["passed"] != true || manifest["source_sha256"] != audit["source_sha256"] || manifest["dataset_revision"] != datasetRevision || manifest["trec_eval_revision"] != trecRevision {
		return fmt.Errorf("baseline provenance mismatch")
	}
	qs, e := loadQueries()
	if e != nil {
		return e
	}
	inventory := M{}
	files, e := filepath.Glob(filepath.Join(from, "receipts", "*.json"))
	if e != nil {
		return e
	}
	if len(files) == 0 {
		return fmt.Errorf("restore baseline receipts first")
	}
	for _, q := range qs {
		for _, m := range baselineMethods {
			files = append(files, filepath.Join(from, "jobs", m+"-"+q.key()+".json"))
		}
	}
	for _, path := range files {
		b, e := os.ReadFile(path)
		if e != nil {
			return e
		}
		rel, e := filepath.Rel(from, path)
		if e != nil {
			return e
		}
		dest := filepath.Join(out, rel)
		if old, e := os.ReadFile(dest); e == nil && string(old) != string(b) {
			return fmt.Errorf("existing evidence differs: %s", rel)
		}
		if e := atomicWrite(dest, b); e != nil {
			return e
		}
		inventory[rel] = digest(b)
	}
	h, e := fileHash(filepath.Join(from, "manifest.json"))
	if e != nil {
		return e
	}
	return writeJSON(filepath.Join(out, "baseline-import.json"), M{"source_directory": from, "source_manifest_sha256": h, "files_sha256": inventory, "new_http_requests": 0, "note": "Baseline scores, receipts, and timings are reused from the initial collection, not measured anew."})
}

func verifyBaselineImport(out string) error {
	if len(methods) <= 4 {
		return nil
	}
	var imported struct {
		Files map[string]string `json:"files_sha256"`
	}
	if e := readJSON(filepath.Join(out, "baseline-import.json"), &imported); e != nil {
		return fmt.Errorf("seed-baseline required: %w", e)
	}
	if len(imported.Files) == 0 {
		return fmt.Errorf("empty baseline inventory")
	}
	for name, want := range imported.Files {
		if filepath.IsAbs(name) || strings.HasPrefix(filepath.Clean(name), "..") {
			return fmt.Errorf("invalid import path")
		}
		got, e := fileHash(filepath.Join(out, name))
		if e != nil {
			return e
		}
		if got != want {
			return fmt.Errorf("imported evidence changed: %s", name)
		}
	}
	qs, e := loadQueries()
	if e != nil {
		return e
	}
	for _, q := range qs {
		for _, m := range baselineMethods {
			name := filepath.Join("jobs", m+"-"+q.key()+".json")
			if imported.Files[name] == "" {
				return fmt.Errorf("baseline job absent from inventory: %s", name)
			}
		}
	}
	return nil
}
