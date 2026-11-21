package main

import (
	"archive/zip"
	"fmt"
	"io"
	"strings"
)

func declTokens(b []byte) map[uint32]string {
	out := map[uint32]string{}
	for _, c := range dxbcChunks(b) {
		if c.tag != "SHEX" {
			continue
		}
		n := len(c.data) / 4
		tok := func(i int) uint32 { return le.Uint32(c.data[i*4:]) }
		for i := 2; i < n; {
			t := tok(i)
			op := t & 0x7ff
			ln := int(t >> 24 & 0x7f)
			if op == 0x35 {
				ln = int(tok(i + 1))
			}
			if ln == 0 {
				break
			}
			switch op {
			case 0x58, 0x59, 0x5a, 0xa1, 0xa2:
				if _, ok := out[op]; !ok {
					s := ""
					for k := 0; k < ln; k++ {
						s += fmt.Sprintf("%08x ", tok(i+k))
					}
					out[op] = s
				}
			}
			i += ln
		}
	}
	return out
}

func cmdDeclTok(path, a string) {
	r, _ := zip.OpenReader(path)
	defer r.Close()
	for _, f := range r.File {
		l := strings.ToLower(f.Name)
		if !strings.Contains(l, a) {
			continue
		}
		rc, _ := f.Open()
		b, _ := io.ReadAll(rc)
		rc.Close()
		fmt.Println(f.Name)
		if strings.Contains(l, ".pc.") {
			c, err := shader51to50(b)
			if err != nil {
				fmt.Println(err)
				continue
			}
			for op, s := range declTokens(c) {
				fmt.Printf("  converted %#x: %s\n", op, s)
			}
		} else {
			for op, s := range declTokens(b) {
				fmt.Printf("  console   %#x: %s\n", op, s)
			}
		}
	}
}
