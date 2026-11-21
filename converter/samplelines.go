package main

import (
	"archive/zip"
	"fmt"
	"io"
	"strings"
)

func cmdSampleLines(path, a string) {
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
		if strings.Contains(l, ".pc.") {
			b, _ = shader51to50(b)
		}
		s, err := disasm(b)
		if err != nil {
			fmt.Println(err)
			continue
		}
		fmt.Println(f.Name)
		seen := map[string]bool{}
		for _, ln := range strings.Split(s, "\n") {
			ln = strings.TrimSpace(ln)
			w := strings.SplitN(ln, " ", 2)[0]
			if i := strings.Index(w, "("); i > 0 {
				w = w[:i]
			}
			if (strings.HasPrefix(w, "sample") || w == "ld" || w == "ld_structured_indexable" || w == "dcl_sampler" || w == "dcl_resource_texture2d" || w == "dcl_resource_structured" || strings.Contains(ln, "cb2[") || strings.Contains(ln, "CB2[")) && !seen[w] {
				seen[w] = true
				fmt.Println("   ", ln)
			}
		}
	}
}
