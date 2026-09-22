package eval

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

type M = map[string]any

func obj(v any) M {
	if v == nil {
		return M{}
	}
	return v.(map[string]any)
}
func arr(v any) []any {
	if v == nil {
		return nil
	}
	return v.([]any)
}
func str(v any) string {
	if v == nil {
		return ""
	}
	return fmt.Sprint(v)
}
func num(v any) float64 {
	switch n := v.(type) {
	case float64:
		return n
	case int:
		return float64(n)
	case int64:
		return float64(n)
	case json.Number:
		f, _ := n.Float64()
		return f
	}
	return 0
}
func yes(v any) bool { b, _ := v.(bool); return b }
func check(err error) {
	if err != nil {
		panic(err)
	}
}
func clone(m M) M {
	r := M{}
	for k, v := range m {
		r[k] = v
	}
	return r
}
func Canon(v any) []byte {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	check(e.Encode(v))
	return bytes.TrimSuffix(b.Bytes(), []byte("\n"))
}
func Hash(b []byte) string { h := sha256.Sum256(b); return hex.EncodeToString(h[:]) }
func Read(path string) []byte {
	r, e := openStored(path)
	check(e)
	defer r.Close()
	b, e := io.ReadAll(r)
	check(e)
	return b
}
func ReadJSON(path string) any { var v any; check(json.Unmarshal(Read(path), &v)); return v }
func WriteJSON(path string, v any) {
	b, e := json.MarshalIndent(v, "", "  ")
	check(e)
	check(os.MkdirAll(filepath.Dir(path), 0755))
	tmp := path + ".tmp"
	check(os.WriteFile(tmp, append(b, '\n'), 0644))
	check(os.Rename(tmp, path))
}
func Lines(path string) []M {
	rows := []M{}
	ForEachLine(path, func(r M) { rows = append(rows, r) })
	return rows
}

func Append(path string, v any) {
	f, e := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	check(e)
	defer f.Close()
	_, e = f.Write(append(Canon(v), '\n'))
	check(e)
	check(f.Sync())
}
func Keys(m M) []string {
	k := make([]string, 0, len(m))
	for s := range m {
		k = append(k, s)
	}
	sort.Strings(k)
	return k
}
func Recover(err *error) {
	if r := recover(); r != nil {
		if e, ok := r.(error); ok {
			*err = e
		} else {
			*err = fmt.Errorf("%v", r)
		}
	}
}
func decode(b []byte) any { var v any; check(json.Unmarshal(b, &v)); return v }
