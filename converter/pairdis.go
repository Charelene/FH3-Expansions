package main

import (
	"archive/zip"
	"fmt"
	"os"
	"strings"
)

// cmdPairDis writes the disassembly of a PC shader and of its console twin from the base game.
func cmdPairDis(path string, decode bool, want, outDir string) {
	r, err := zip.OpenReader(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer r.Close()
	for _, f := range r.File {
		n := f.Name
		if decode {
			n = realName(n)
		}
		l := strings.ToLower(n)
		base := l[strings.LastIndex(l, "/")+1:]
		if base != want {
			continue
		}
		pb, _ := readZipFile(f)
		s, err := myDisasm(pb)
		if err != nil {
			fmt.Println("pc:", err)
		}
		os.WriteFile(outDir+"/pc.txt", []byte(strings.Join(s, "\n")+"\n"), 0o644)
		if full, err := disasm(pb); err == nil {
			os.WriteFile(outDir+"/pc-ms.txt", []byte(full), 0o644)
		}
		if tw, ok := zipOpts.base[strings.Replace(base, ".pc.", ".durango.", 1)]; ok {
			cb, _ := readZipFile(tw)
			s2, err := myDisasm(cb)
			if err != nil {
				fmt.Println("console:", err)
			}
			os.WriteFile(outDir+"/console.txt", []byte(strings.Join(s2, "\n")+"\n"), 0o644)
		}
		fmt.Println("written")
		return
	}
	fmt.Println("not found")
}
