package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
)

func readShaderBins(p string, decode bool) map[string][]byte {
	m := map[string][]byte{}
	r, err := zip.OpenReader(p)
	if err != nil {
		return m
	}
	defer r.Close()
	for _, f := range r.File {
		n := f.Name
		if decode {
			n = realName(n)
		}
		b := strings.ToLower(path.Base(strings.ReplaceAll(n, "\\", "/")))
		if strings.HasSuffix(b, ".shaderbin") {
			rc, _ := f.Open()
			d, _ := io.ReadAll(rc)
			rc.Close()
			m[b] = d
		}
	}
	return m
}

// cmdShaderBinBlobs compares same-named shaderbins blob by blob.
func cmdShaderBinBlobs(dlc string, decode bool, xbox []string) {
	x := map[string][]byte{}
	for _, p := range xbox {
		for k, v := range readShaderBins(p, false) {
			x[k] = v
		}
	}
	st := map[string]int{}
	shown := 0
	for k, v := range readShaderBins(dlc, decode) {
		xv, ok := x[k]
		if !ok {
			continue
		}
		a, err1 := parseBundle(v)
		b, err2 := parseBundle(xv)
		if err1 != nil || err2 != nil {
			st["unparsed"]++
			continue
		}
		var diffs []string
		if len(a.blobs) != len(b.blobs) {
			diffs = append(diffs, "blob count")
		} else {
			for i := range a.blobs {
				if a.blobs[i].tag != b.blobs[i].tag {
					diffs = append(diffs, "order")
					break
				}
				if !bytes.Equal(a.blobs[i].data, b.blobs[i].data) {
					diffs = append(diffs, fmt.Sprintf("%s(%d/%d)", a.blobs[i].tag, len(a.blobs[i].data), len(b.blobs[i].data)))
				}
				for mi := range a.blobs[i].metas {
					if mi < len(b.blobs[i].metas) && !bytes.Equal(a.blobs[i].metas[mi].payload, b.blobs[i].metas[mi].payload) {
						diffs = append(diffs, a.blobs[i].tag+" meta")
					}
				}
			}
		}
		key := strings.Join(diffs, " ")
		// generalise sizes away for the tally
		gen := []string{}
		for _, d := range diffs {
			if i := strings.Index(d, "("); i > 0 {
				d = d[:i]
			}
			gen = append(gen, d)
		}
		sort.Strings(gen)
		st[strings.Join(gen, "+")]++
		if shown < 5 {
			shown++
			fmt.Printf("%s: %s\n", k, key)
		}
	}
	for k, n := range st {
		fmt.Printf("%5d  differing blobs: %s\n", n, k)
	}
}
