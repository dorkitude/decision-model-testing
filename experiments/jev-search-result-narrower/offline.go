package main

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"

	"github.com/spf13/cobra"
)

const evidenceArchive = "runs/evidence/jev-search-result-narrower-run-v1.tar.gz"

func fileHash(path string) (string, error) {
	f, e := os.Open(path)
	if e != nil {
		return "", e
	}
	defer f.Close()
	h := sha256.New()
	_, e = io.Copy(h, f)
	return hex.EncodeToString(h.Sum(nil)), e
}
func readRows(path string) ([]M, error) {
	var rows []M
	e := scan(path, func(b []byte) error {
		if len(strings.TrimSpace(string(b))) == 0 {
			return nil
		}
		var r M
		if e := json.Unmarshal(b, &r); e != nil {
			return e
		}
		if r == nil {
			return fmt.Errorf("non-object record")
		}
		rows = append(rows, r)
		return nil
	})
	if e != nil {
		return nil, fmt.Errorf("%s: %w", path, e)
	}
	return rows, nil
}
func readKeyed(path, field string) (map[string]M, error) {
	rows, e := readRows(path)
	if e != nil {
		return nil, e
	}
	m := map[string]M{}
	for _, r := range rows {
		id := str(r[field])
		if id == "" || m[id] != nil {
			return nil, fmt.Errorf("%s: missing or duplicate %s %q", path, field, id)
		}
		m[id] = r
	}
	return m, nil
}
func oneRow(path string) (M, error) {
	r, e := readRows(path)
	if e != nil {
		return nil, e
	}
	if len(r) != 1 {
		return nil, fmt.Errorf("%s: expected one record", path)
	}
	return r[0], nil
}
func same(a, b any) bool { return reflect.DeepEqual(a, b) }

// Extract only regular evidence files into a disposable directory, never the working tree.
func openArchive(path, metadata string) (root string, cleanup func(), digest string, err error) {
	cleanup = func() {}
	digest, err = fileHash(path)
	if err != nil {
		return
	}
	var meta M
	meta, err = oneRow(metadata)
	if err != nil {
		return
	}
	if digest != str(meta["sha256"]) {
		err = fmt.Errorf("archive SHA-256 mismatch")
		return
	}
	var tmp string
	tmp, err = os.MkdirTemp("", "jev-evidence-")
	if err != nil {
		return
	}
	cleanup = func() { _ = os.RemoveAll(tmp) }
	var f *os.File
	f, err = os.Open(path)
	if err != nil {
		cleanup()
		return
	}
	defer f.Close()
	var gz *gzip.Reader
	gz, err = gzip.NewReader(f)
	if err != nil {
		cleanup()
		return
	}
	defer gz.Close()
	tr := tar.NewReader(gz)
	for {
		var hdr *tar.Header
		hdr, err = tr.Next()
		if err == io.EOF {
			err = nil
			break
		}
		if err != nil {
			break
		}
		name := filepath.Clean(hdr.Name)
		if filepath.IsAbs(name) || name == ".." || strings.HasPrefix(name, "../") {
			err = fmt.Errorf("unsafe archive path %q", hdr.Name)
			break
		}
		parts := strings.Split(filepath.ToSlash(name), "/")
		if len(parts) < 2 {
			continue
		}
		if root == "" {
			root = filepath.Join(tmp, parts[0])
		}
		if filepath.Join(tmp, parts[0]) != root {
			err = fmt.Errorf("multiple archive roots")
			break
		}
		if parts[1] != "run" && parts[1] != "data" && parts[1] != "source" {
			continue
		}
		if hdr.Typeflag == tar.TypeDir {
			continue
		}
		if hdr.Typeflag != tar.TypeReg {
			err = fmt.Errorf("non-regular evidence member %s", name)
			break
		}
		target := filepath.Join(tmp, name)
		if err = os.MkdirAll(filepath.Dir(target), 0700); err != nil {
			break
		}
		var out *os.File
		out, err = os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			break
		}
		_, err = io.Copy(out, tr)
		ce := out.Close()
		if err == nil {
			err = ce
		}
		if err != nil {
			break
		}
	}
	if err != nil {
		cleanup()
	}
	return
}
func writeDerived(dir string, files map[string][]M) error {
	if e := os.Mkdir(dir, 0700); e != nil {
		return fmt.Errorf("output must be a new directory: %w", e)
	}
	for name, rows := range files {
		f, e := os.OpenFile(filepath.Join(dir, name+".jsonl"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if e != nil {
			return e
		}
		w := bufio.NewWriter(f)
		for _, r := range rows {
			if _, e = w.Write(append(canon(r), '\n')); e != nil {
				f.Close()
				return e
			}
		}
		e = w.Flush()
		ce := f.Close()
		if e != nil {
			return e
		}
		if ce != nil {
			return ce
		}
	}
	return nil
}

func addOfflineCommands(root *cobra.Command, run *string) {
	var archive, metadata, data, source string
	audit := &cobra.Command{Use: "audit", Short: "Verify frozen evidence offline without changing it", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		r, d, s := *run, data, source
		if archive != "" {
			if cmd.Flags().Changed("run") || cmd.Flags().Changed("data") || cmd.Flags().Changed("source") {
				return fmt.Errorf("--archive cannot be combined with --run, --data, or --source")
			}
			base, clean, _, e := openArchive(archive, metadata)
			if e != nil {
				return e
			}
			defer clean()
			r, d, s = filepath.Join(base, "run"), filepath.Join(base, "data"), filepath.Join(base, "source")
		}
		if s == "" {
			return fmt.Errorf("supply --source with the frozen harness directory, or --archive")
		}
		result, e := auditEvidence(r, d, s)
		if e != nil {
			return e
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	}}
	audit.Flags().StringVar(&archive, "archive", "", "Read run, data, and frozen source from this verified tar.gz")
	audit.Flags().StringVar(&metadata, "archive-metadata", "results/run-v1/archive.jsonl", "Archive SHA-256 manifest")
	audit.Flags().StringVar(&data, "data", "data", "Frozen question and corpus directory")
	audit.Flags().StringVar(&source, "source", "", "Frozen Go harness source directory (required without --archive)")
	root.AddCommand(audit)
	var ca, cm, rates, summary, out string
	cost := &cobra.Command{Use: "estimate-cost", Short: "Price saved receipts offline using frozen rate cards", Args: cobra.NoArgs, RunE: func(cmd *cobra.Command, args []string) error {
		base, clean, digest, e := openArchive(ca, cm)
		if e != nil {
			return e
		}
		defer clean()
		result, files, e := estimateCost(filepath.Join(base, "run"), rates, summary, digest)
		if e != nil {
			return e
		}
		if out != "" {
			if e = writeDerived(out, files); e != nil {
				return e
			}
		}
		return json.NewEncoder(cmd.OutOrStdout()).Encode(result)
	}}
	cost.Flags().StringVar(&ca, "archive", evidenceArchive, "Frozen evidence tar.gz")
	cost.Flags().StringVar(&cm, "archive-metadata", "results/run-v1/archive.jsonl", "Archive SHA-256 manifest")
	cost.Flags().StringVar(&rates, "rates", "results/cost-estimate-v1/rates.jsonl", "Per-model USD per million token rates")
	cost.Flags().StringVar(&summary, "summary", "results/run-v1/summary.jsonl", "Frozen resource summary to reconcile")
	cost.Flags().StringVar(&out, "out", "", "Write derived JSONL files to a new directory (default: stdout only)")
	root.AddCommand(cost)
}
