package main

import (
	"archive/zip"
	"fmt"
	"path"
	"sort"
	"strings"
)

func zipNameSet(p string, decode bool) map[string]string {
	m := map[string]string{}
	r, err := zip.OpenReader(p)
	if err != nil {
		fmt.Println(p, err)
		return m
	}
	defer r.Close()
	for _, f := range r.File {
		n := f.Name
		if decode {
			n = realName(n)
		}
		m[strings.ToLower(path.Base(strings.ReplaceAll(n, "\\", "/")))] = n
	}
	return m
}

// cmdShaderCov: how many of a DLC archive's .pc shaders have a .durango twin in the Xbox archives.
func cmdShaderCov(dlc string, decode bool, xbox []string) {
	have := map[string]bool{}
	for _, x := range xbox {
		for k := range zipNameSet(x, false) {
			have[k] = true
		}
	}
	d := zipNameSet(dlc, decode)
	hit, miss := 0, 0
	missDirs := map[string]int{}
	hitBin, missBin := 0, 0
	for k, full := range d {
		switch {
		case strings.HasSuffix(k, ".pc.vso") || strings.HasSuffix(k, ".pc.pso"):
			if have[strings.Replace(k, ".pc.", ".durango.", 1)] {
				hit++
			} else {
				miss++
				missDirs[strings.ToLower(path.Base(path.Dir(strings.ReplaceAll(full, "\\", "/"))))]++
			}
		case strings.HasSuffix(k, ".shaderbin"):
			if have[k] {
				hitBin++
			} else {
				missBin++
			}
		}
	}
	fmt.Printf("%s: shader files with an Xbox twin %d, without %d; shaderbins known %d, unknown %d\n", dlc, hit, miss, hitBin, missBin)
	ks := []string{}
	for k := range missDirs {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for i, k := range ks {
		if i >= 40 {
			fmt.Printf("  ... %d more\n", len(ks)-40)
			break
		}
		fmt.Printf("  %4d  %s\n", missDirs[k], k)
	}
}
