package main

import (
	"archive/zip"
	"fmt"
	"sort"
	"strings"
)

// cmdMissingShaders lists shaders (by .shaderbin name) with PC builds that have no console build of the same name in the base game.
func cmdMissingShaders(path string, decode bool) {
	r, err := zip.OpenReader(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer r.Close()
	dirBin := map[string]string{}
	miss := map[string]int{}
	tot := map[string]int{}
	for _, f := range r.File {
		n := f.Name
		if decode {
			n = realName(n)
		}
		l := strings.ToLower(n)
		k := strings.LastIndex(l, "/")
		dir, base := l[:k+1], l[k+1:]
		if strings.HasSuffix(base, ".shaderbin") {
			dirBin[dir] = strings.TrimSuffix(base, ".shaderbin")
		}
		if strings.HasSuffix(base, ".pc.vso") || strings.HasSuffix(base, ".pc.pso") {
			tot[dir]++
			if _, ok := zipOpts.base[strings.Replace(base, ".pc.", ".durango.", 1)]; !ok {
				miss[dir]++
			}
		}
	}
	var out []string
	for d, n := range miss {
		out = append(out, fmt.Sprintf("%-60s %3d of %3d files without a console build", dirBin[d], n, tot[d]))
	}
	sort.Strings(out)
	for _, s := range out {
		fmt.Println(" ", s)
	}
	fmt.Printf("%d shaders affected of %d\n", len(miss), len(tot))
}
