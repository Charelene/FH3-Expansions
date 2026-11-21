package main

import (
	"fmt"
	"sort"
)

func cmdMzStats(path string, limit int) {
	m, err := openMinizip(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	st := map[string]int{}
	ex := map[string]string{}
	for i := range m.entries {
		if i >= limit {
			break
		}
		b, err := m.read(i)
		if err != nil || len(b) < 4 || string(b[:4]) != "burG" {
			continue
		}
		bu, err := parseBundle(b)
		if err != nil || len(bu.blobs) == 0 || bu.blobs[0].tag != "BCXT" {
			continue
		}
		bl := bu.blobs[0]
		if bl.major != 2 {
			st["UWP bundle"]++
			continue
		}
		t, err := parseXboxHeader(bl.metas[0].payload)
		if err != nil {
			continue
		}
		k := fmt.Sprintf("W==layW %v H==layH %v first>0 %v partial %v tile %d", t.w == t.layW, t.h == t.layH, t.first > 0, t.mips < t.layMips, t.tile)
		st[k]++
		if _, ok := ex[k]; !ok {
			ex[k] = fmt.Sprintf("e%d %v data 0x%x", i, t, len(bl.data))
		}
	}
	ks := []string{}
	for k := range st {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("%-60s %6d  %s\n", k, st[k], ex[k])
	}
}
