package main

import (
	"archive/zip"
	"fmt"
	"os"
	"regexp"
	"sort"
	"strings"
)

// cmdXlateStats translates every PC shader of a zip and tallies the outcome.
func cmdXlateStats(p string, decode bool, show string) {
	r, err := zip.OpenReader(p)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer r.Close()
	tally := map[string]int{}
	examples := map[string]string{}
	for _, f := range r.File {
		name := f.Name
		if decode {
			name = realName(name)
		}
		l := strings.ToLower(name)
		kind := ""
		switch {
		case strings.HasSuffix(l, ".pc.pso"):
			kind = "pso"
		case strings.HasSuffix(l, ".pc.vso"):
			kind = "vso"
		default:
			continue
		}
		b, _ := readZipFile(f)
		_, notes, err := shaderPCToConsole(b)
		key := kind + " ok"
		if err != nil {
			e := err.Error()
			e = regexp.MustCompile(`instruction \d+ `).ReplaceAllString(e, "")
			key = kind + " FAIL " + e
		}
		tally[key]++
		if _, ok := examples[key]; !ok {
			examples[key] = l[strings.LastIndexAny(l, "/\\")+1:]
		}
		if show != "" && strings.Contains(l, show) {
			fmt.Println(l, err, notes)
		}
	}
	ks := make([]string, 0, len(tally))
	for k := range tally {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("%6d  %s   (e.g. %s)\n", tally[k], k, examples[k])
	}
}

// cmdXlateOne writes the PC disassembly and the translated disassembly of one shader.
func cmdXlateOne(p string, decode bool, want, outDir string) {
	b := findInZip(p, decode, want)
	if b == nil {
		fmt.Println("not found")
		return
	}
	s, _ := myDisasm(b)
	os.WriteFile(outDir+"/pc.txt", []byte(strings.Join(s, "\n")+"\n"), 0o644)
	nb, notes, err := shaderPCToConsole(b)
	if err != nil {
		fmt.Println("translate:", err)
		return
	}
	fmt.Println(notes)
	s2, err := myDisasm(nb)
	if err != nil {
		fmt.Println("disassembly of the result:", err)
	}
	os.WriteFile(outDir+"/xl.txt", []byte(strings.Join(s2, "\n")+"\n"), 0o644)
	os.WriteFile(outDir+"/xl.dxbc", nb, 0o644)
	if full, err := disasm(nb); err == nil {
		os.WriteFile(outDir+"/xl-ms.txt", []byte(full), 0o644)
	} else {
		fmt.Println("d3dcompiler:", err)
	}
}
