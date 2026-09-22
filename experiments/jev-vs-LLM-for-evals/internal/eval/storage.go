package eval

import (
	"bufio"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func openStored(path string) (io.ReadCloser, error) {
	f, e := os.Open(path)
	if os.IsNotExist(e) {
		f, e = os.Open(path + ".gz")
		if e != nil {
			return nil, e
		}
		gz, e := gzip.NewReader(f)
		if e != nil {
			f.Close()
			return nil, e
		}
		return &storedGzip{Reader: gz, file: f}, nil
	}
	if e == nil && strings.HasSuffix(path, ".gz") {
		gz, err := gzip.NewReader(f)
		if err != nil {
			f.Close()
			return nil, err
		}
		return &storedGzip{Reader: gz, file: f}, nil
	}
	return f, e
}

type storedGzip struct {
	*gzip.Reader
	file *os.File
}

func (r *storedGzip) Close() error {
	e := r.Reader.Close()
	e2 := r.file.Close()
	if e != nil {
		return e
	}
	return e2
}
func ForEachLine(path string, fn func(M)) {
	r, e := openStored(path)
	if os.IsNotExist(e) {
		return
	}
	check(e)
	defer r.Close()
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 64*1024), 64<<20)
	line := 0
	for scanner.Scan() {
		line++
		b := scanner.Bytes()
		if len(b) == 0 {
			continue
		}
		var row M
		if e := json.Unmarshal(b, &row); e != nil {
			panic(fmt.Errorf("invalid JSONL %s line %d", path, line))
		}
		fn(row)
	}
	check(scanner.Err())
}

// Recover only a torn final append, preserving its bytes for audit. Any malformed
// complete line is a hard error; it must not silently disappear from accounting.
func RepairTail(path string) {
	f, e := os.OpenFile(path, os.O_RDWR, 0644)
	if os.IsNotExist(e) {
		return
	}
	check(e)
	defer f.Close()
	info, e := f.Stat()
	check(e)
	if info.Size() == 0 {
		return
	}
	last := []byte{0}
	_, e = f.ReadAt(last, info.Size()-1)
	check(e)
	if last[0] == '\n' {
		return
	}
	start := info.Size()
	chunk := make([]byte, 4096)
	found := false
	for start > 0 && !found {
		n := int64(len(chunk))
		if start < n {
			n = start
		}
		start -= n
		_, e = f.ReadAt(chunk[:n], start)
		check(e)
		for i := int(n) - 1; i >= 0; i-- {
			if chunk[i] == '\n' {
				start += int64(i) + 1
				found = true
				break
			}
		}
	}
	tail := make([]byte, info.Size()-start)
	_, e = f.ReadAt(tail, start)
	check(e)
	var row M
	if json.Unmarshal(tail, &row) == nil {
		_, e = f.WriteAt([]byte{'\n'}, info.Size())
		check(e)
		check(f.Sync())
		return
	}
	backup := fmt.Sprintf("%s.torn-%d", path, time.Now().UnixNano())
	check(os.WriteFile(backup, tail, 0600))
	check(f.Truncate(start))
	check(f.Sync())
}

// Seal large finished-shard artifacts losslessly. Readers transparently accept
// .gz files. Hash round-trip is checked before removing the redundant raw file.
func SealShard(out string) M {
	index := M{}
	names, e := filepath.Glob(filepath.Join(out, "*"))
	check(e)
	for _, path := range names {
		info, e := os.Stat(path)
		check(e)
		if info.IsDir() || info.Size() < 1<<20 || filepath.Ext(path) == ".gz" {
			continue
		}
		if filepath.Ext(path) != ".json" && filepath.Ext(path) != ".jsonl" {
			continue
		}
		source, e := os.Open(path)
		check(e)
		dest, e := os.Create(path + ".gz.tmp")
		check(e)
		z, e := gzip.NewWriterLevel(dest, gzip.BestSpeed)
		check(e)
		h := sha256.New()
		n, e := io.Copy(io.MultiWriter(z, h), source)
		source.Close()
		check(e)
		check(z.Close())
		check(dest.Sync())
		check(dest.Close())
		want := hex.EncodeToString(h.Sum(nil))
		f, e := os.Open(path + ".gz.tmp")
		check(e)
		zr, e := gzip.NewReader(f)
		check(e)
		h2 := sha256.New()
		n2, e := io.Copy(h2, zr)
		zr.Close()
		f.Close()
		check(e)
		if n != n2 || want != hex.EncodeToString(h2.Sum(nil)) {
			panic("archive round-trip mismatch")
		}
		check(os.Rename(path+".gz.tmp", path+".gz"))
		check(os.Remove(path))
		index[filepath.Base(path)] = M{"file": filepath.Base(path) + ".gz", "uncompressed_sha256": want, "uncompressed_bytes": n}
	}
	if len(index) > 0 {
		WriteJSON(filepath.Join(out, "archive.json"), index)
	}
	return index
}
