package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
)

type dxChunk struct {
	tag  string
	data []byte
}

func dxbcChunks(b []byte) []dxChunk {
	if len(b) < 0x20 || string(b[:4]) != "DXBC" {
		return nil
	}
	n := int(le.Uint32(b[0x1c:]))
	var out []dxChunk
	for i := 0; i < n; i++ {
		o := int(le.Uint32(b[0x20+4*i:]))
		if o+8 > len(b) {
			return out
		}
		sz := int(le.Uint32(b[o+4:]))
		if o+8+sz > len(b) {
			return out
		}
		out = append(out, dxChunk{string(b[o : o+4]), b[o+8 : o+8+sz]})
	}
	return out
}

// cmdShaderPairs compares the .durango and .pc builds of each shader in a zip.
func cmdShaderPairs(path string) {
	r, err := zip.OpenReader(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer r.Close()
	files := map[string][]byte{}
	for _, f := range r.File {
		l := strings.ToLower(f.Name)
		if strings.HasSuffix(l, ".vso") || strings.HasSuffix(l, ".pso") {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			files[l] = b
		}
	}
	st := map[string]int{}
	shown := 0
	names := []string{}
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, n := range names {
		if !strings.Contains(n, ".durango.") {
			continue
		}
		pc, ok := files[strings.Replace(n, ".durango.", ".pc.", 1)]
		if !ok {
			st["no pc twin"]++
			continue
		}
		dc, pcc := dxbcChunks(files[n]), dxbcChunks(pc)
		tags := func(c []dxChunk) string {
			s := []string{}
			for _, x := range c {
				s = append(s, fmt.Sprintf("%s:%d", x.tag, len(x.data)))
			}
			return strings.Join(s, " ")
		}
		get := func(c []dxChunk, t ...string) []byte {
			for _, x := range c {
				for _, tt := range t {
					if x.tag == tt {
						return x.data
					}
				}
			}
			return nil
		}
		ds, ps := get(dc, "SHEX", "SHDR"), get(pcc, "SHEX", "SHDR")
		k := "code differs"
		if bytes.Equal(ds, ps) {
			k = "code identical"
		}
		sig := bytes.Equal(get(dc, "ISGN"), get(pcc, "ISGN")) && bytes.Equal(get(dc, "OSGN"), get(pcc, "OSGN"))
		st[fmt.Sprintf("%s, signatures equal %v", k, sig)]++
		if shown < 4 {
			shown++
			fmt.Printf("%s\n   durango: %s\n   pc:      %s\n", n[strings.LastIndex(n, "/")+1:], tags(dc), tags(pcc))
			if len(ds) >= 8 && len(ps) >= 8 {
				fmt.Printf("   code version durango %08x len %d words, pc %08x len %d words\n", le.Uint32(ds), le.Uint32(ds[4:]), le.Uint32(ps), le.Uint32(ps[4:]))
			}
		}
	}
	for k, v := range st {
		fmt.Printf("%5d  %s\n", v, k)
	}
}
