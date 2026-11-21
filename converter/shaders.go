package main

import (
	"archive/zip"
	"encoding/hex"
	"fmt"
	"io"
	"sort"
	"strings"
)

// cmdZipNames tallies a zip's entries by kind after decoding scrambled names.
func cmdZipNames(path string, decode bool, show string) {
	r, err := zip.OpenReader(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer r.Close()
	m := map[string]int{}
	shown := 0
	for _, f := range r.File {
		n := f.Name
		if decode {
			n = realName(n)
		}
		l := strings.ToLower(n)
		k := l[strings.LastIndex(l, ".")+1:]
		if strings.Contains(l, ".pc.") {
			k = "pc." + k
		} else if strings.Contains(l, ".durango.") {
			k = "durango." + k
		}
		m[k]++
		if show != "" && strings.Contains(l, show) && shown < 6 {
			shown++
			rc, _ := f.Open()
			h := make([]byte, 0x40)
			io.ReadFull(rc, h)
			rc.Close()
			fmt.Printf("  %s (%d)\n    %s\n", n, f.UncompressedSize64, hex.EncodeToString(h))
		}
	}
	ks := []string{}
	for k := range m {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("  %6d %s\n", m[k], k)
	}
}
