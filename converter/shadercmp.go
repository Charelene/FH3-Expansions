package main

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"io"
	"path"
	"strings"
)

type zent struct {
	zip  string
	name string
	sum  [32]byte
}

// indexShaders maps lower-case base names of shader files to their content.
func indexShaders(paths []string, decode bool, want func(string) bool) map[string]zent {
	m := map[string]zent{}
	for _, p := range paths {
		r, err := zip.OpenReader(p)
		if err != nil {
			fmt.Println(p, err)
			continue
		}
		for _, f := range r.File {
			n := f.Name
			if decode {
				n = realName(n)
			}
			b := strings.ToLower(path.Base(strings.ReplaceAll(n, "\\", "/")))
			if !want(b) {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				continue
			}
			data, _ := io.ReadAll(rc)
			rc.Close()
			m[b] = zent{p, f.Name, sha256.Sum256(data)}
		}
		r.Close()
	}
	return m
}

func cmdShaderBinCmp(dlc string, decode bool, xbox []string) {
	isBin := func(b string) bool { return strings.HasSuffix(b, ".shaderbin") }
	x := indexShaders(xbox, false, isBin)
	d := indexShaders([]string{dlc}, decode, isBin)
	same, diff, missing := 0, 0, 0
	for k, v := range d {
		if xv, ok := x[k]; !ok {
			missing++
		} else if xv.sum == v.sum {
			same++
		} else {
			diff++
		}
	}
	fmt.Printf("%s: shaderbins identical to the Xbox base's %d, same name but different %d, not in base %d\n", dlc, same, diff, missing)
}
