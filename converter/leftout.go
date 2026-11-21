package main

import (
	"archive/zip"
	"fmt"
	"sort"
	"strings"
)

// cmdLeftOut explains, for each PC-only shader file, what could fill it.
func cmdLeftOut(path string, decode bool, list bool) {
	r, err := zip.OpenReader(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer r.Close()
	dirShader := map[string]string{}
	for _, f := range r.File {
		n := f.Name
		if decode {
			n = realName(n)
		}
		l := strings.ToLower(n)
		if strings.HasSuffix(l, ".shaderbin") {
			k := strings.LastIndex(l, "/")
			dirShader[l[:k+1]] = strings.TrimSuffix(l[k+1:], ".shaderbin")
		}
	}
	st := map[string]int{}
	var lines []string
	for _, f := range r.File {
		n := f.Name
		if decode {
			n = realName(n)
		}
		l := strings.ToLower(n)
		if !strings.HasSuffix(l, ".pc.vso") && !strings.HasSuffix(l, ".pc.pso") {
			continue
		}
		k := strings.LastIndex(l, "/")
		base := strings.Replace(l[k+1:], ".pc.", ".durango.", 1)
		if _, ok := zipOpts.base[base]; ok {
			continue
		}
		sh := dirShader[l[:k+1]]
		suffix := strings.TrimPrefix(base, sh)
		b, _ := readZipFile(f)
		kind := "vso"
		if strings.HasSuffix(base, ".pso") {
			kind = "pso"
		}
		_, terr := shader51to50(b)
		_, from := standIn(sh, suffix, shaderSig(b))
		res := kind + ": "
		switch {
		case terr == nil:
			res += "translates cleanly"
		case from != "":
			res += "exact-signature stand-in"
		case len(zipOpts.bySuffix[suffix]) > 0:
			res += "only a stand-in with another signature"
		default:
			res += "scenario unknown to the base game"
		}
		st[res]++
		if list && terr != nil && from == "" {
			lines = append(lines, fmt.Sprintf("  %s | %s | %s", sh, suffix, res))
		}
	}
	sort.Strings(lines)
	for _, s := range lines {
		fmt.Println(s)
	}
	ks := []string{}
	for k := range st {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("%6d  %s\n", st[k], k)
	}
}
