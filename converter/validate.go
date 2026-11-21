package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

var guidName = regexp.MustCompile(`[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}\.swatchbin$`)

type zref struct{ zip, name string }

func indexZips(root string) map[string]zref {
	m := map[string]zref{}
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.EqualFold(filepath.Ext(p), ".zip") {
			return nil
		}
		r, err := zip.OpenReader(p)
		if err != nil {
			return nil
		}
		defer r.Close()
		for _, f := range r.File {
			k := strings.ToLower(filepath.Base(f.Name))
			if guidName.MatchString(k) {
				m[k] = zref{p, f.Name}
			}
		}
		return nil
	})
	return m
}

func readZ(z zref) ([]byte, error) {
	r, err := zip.OpenReader(z.zip)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	for _, f := range r.File {
		if f.Name == z.name {
			rc, err := f.Open()
			if err != nil {
				return nil, err
			}
			defer rc.Close()
			return io.ReadAll(rc)
		}
	}
	return nil, fmt.Errorf("%s not in %s", z.name, z.zip)
}

// cmdValidate converts every UWP texture that also exists in the Xbox base
// game and compares header and data.
func cmdValidate(xboxRoot string, uwpRoots []string) {
	base := indexZips(xboxRoot)
	stats := map[string]int{}
	var fails []string
	for _, ur := range uwpRoots {
		for k, z := range indexZips(ur) {
			bz, ok := base[k]
			if !ok {
				continue
			}
			pcB, err1 := readZ(z)
			xbB, err2 := readZ(bz)
			if err1 != nil || err2 != nil {
				stats["read error"]++
				continue
			}
			pb, err1 := parseBundle(pcB)
			xb, err2 := parseBundle(xbB)
			if err1 != nil || err2 != nil || pb.blobs[0].major != 0 || xb.blobs[0].major != 2 {
				stats["not a UWP/Xbox pair"]++
				continue
			}
			pt, err := parsePCHeader(pb.blobs[0].metas[0].payload)
			if err != nil {
				stats["UWP header: "+err.Error()]++
				continue
			}
			hdr, data, err := convertFull(pt, pb.blobs[0].data)
			if err != nil {
				stats["convert: "+err.Error()]++
				continue
			}
			he := bytes.Equal(hdr, xb.blobs[0].metas[0].payload)
			de := bytes.Equal(data, xb.blobs[0].data)
			key := fmt.Sprintf("header %v data %v", he, de)
			stats[key]++
			if he && !de {
				eq, n := detileCompare(pt, pb.blobs[0].data, xb.blobs[0].data)
				if eq == n {
					stats["  of which pixels equal (padding differs)"]++
				} else if eq == 0 {
					stats["  of which no mip equal (re-encoded)"]++
				} else {
					stats[fmt.Sprintf("  of which %d of %d mips equal", eq, n)]++
				}
			}
			if !he || !de {
				xt, _ := parseXboxHeader(xb.blobs[0].metas[0].payload)
				ct, _ := parseXboxHeader(hdr)
				fails = append(fails, fmt.Sprintf("%s\n    conv %v\n    xbox %v", k, ct, xt))
			}
		}
	}
	ks := []string{}
	for k := range stats {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("%-40s %d\n", k, stats[k])
	}
	for i, f := range fails {
		if i >= 25 {
			break
		}
		fmt.Println(f)
	}
}
