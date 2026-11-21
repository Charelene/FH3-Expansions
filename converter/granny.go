package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

var grannyMagic = []byte{0xe5, 0x9b, 0x49, 0x5e, 0x6f, 0x63, 0x1f, 0x14, 0x1e, 0x13, 0xeb, 0xa9, 0x90, 0xbe, 0xed, 0xc4}

func grannyKey(b []byte) string {
	if len(b) < 0x48 {
		return "short"
	}
	if !bytes.Equal(b[:16], grannyMagic) {
		return fmt.Sprintf("other magic % x", b[:8])
	}
	ver, tag := le.Uint32(b[0x20:]), le.Uint32(b[0x44:])
	so, sc := le.Uint32(b[0x2c:]), le.Uint32(b[0x30:])
	_ = so
	comps := map[uint32]int{}
	base := 0x20 + int(le.Uint32(b[0x20+0xc:]))
	for i := 0; i < int(sc) && base+i*44+4 <= len(b); i++ {
		comps[le.Uint32(b[base+i*44:])]++
	}
	return fmt.Sprintf("ver %d tag 0x%x hdrfmt %d sections %d comp %v", ver, tag, le.Uint32(b[0x14:]), sc, comps)
}

func cmdGrannyScan(roots []string) {
	st := map[string]int{}
	ex := map[string]string{}
	add := func(k, where string) {
		st[k]++
		if _, ok := ex[k]; !ok {
			ex[k] = where
		}
	}
	for _, root := range roots {
		filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() {
				return nil
			}
			switch strings.ToLower(filepath.Ext(p)) {
			case ".zip":
				r, err := zip.OpenReader(p)
				if err != nil {
					return nil
				}
				defer r.Close()
				for _, f := range r.File {
					rc, err := f.Open()
					if err != nil {
						continue
					}
					b := make([]byte, 0x400)
					n, _ := io.ReadFull(rc, b)
					rc.Close()
					if n >= 16 && bytes.Equal(b[:16], grannyMagic) {
						add(grannyKey(b[:n]), p+" :: "+f.Name)
					} else if strings.HasSuffix(strings.ToLower(f.Name), ".gr2") {
						add("gr2 without magic", p+" :: "+f.Name)
					}
				}
			case ".minizip":
				m, err := openMinizip(p)
				if err != nil {
					return nil
				}
				defer m.f.Close()
				for i := range m.entries {
					b, err := m.read(i)
					if err != nil {
						continue
					}
					if k := bytes.Index(b, grannyMagic); k >= 0 {
						add("in minizip: "+grannyKey(b[k:]), fmt.Sprintf("%s entry %d at 0x%x", p, i, k))
					}
				}
			default:
				b, _ := os.ReadFile(p)
				if k := bytes.Index(b, grannyMagic); k >= 0 {
					add("loose: "+grannyKey(b[k:]), p)
				}
			}
			return nil
		})
	}
	ks := []string{}
	for k := range st {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("%6d  %s\n        e.g. %s\n", st[k], k, ex[k])
	}
}
