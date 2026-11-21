package main

import (
	"archive/zip"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

var reDecl = regexp.MustCompile(`^dcl_(constantbuffer|resource|resource_structured|resource_raw|sampler) (cb|t|s)(\d+):(\d+)(?:\.\.0x[0-9a-f]+)?(?:\[(\d+)\])?`)
var reCBUse = regexp.MustCompile(`cb0:(\d+)\[(?:[^\]\[]*\+)?(\d+)\]`)

// cmdCBScan tallies declarations and constant-buffer element reads over the
// shaders of a zip whose names end in suffix.
func cmdCBScan(p string, decode bool, suffix string) {
	r, err := zip.OpenReader(p)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer r.Close()
	decl := map[string]int{}
	maxUse := map[string]int{} // "cbN" -> highest element read
	high := map[string]int{}   // reads of high elements
	n := 0
	for _, f := range r.File {
		name := f.Name
		if decode {
			name = realName(name)
		}
		if !strings.HasSuffix(strings.ToLower(name), suffix) {
			continue
		}
		b, _ := readZipFile(f)
		lines, _ := myDisasm(b)
		n++
		seen := map[string]bool{}
		for _, l := range lines {
			if m := reDecl.FindStringSubmatch(l); m != nil {
				k := fmt.Sprintf("%s%s:%s", m[2], m[3], m[4])
				if m[2] == "cb" {
					k += "[" + m[5] + "]"
				}
				decl[k]++
				continue
			}
			for _, m := range reCBUse.FindAllStringSubmatch(l, -1) {
				var e int
				fmt.Sscan(m[2], &e)
				k := "cb" + m[1]
				if e > maxUse[k] {
					maxUse[k] = e
				}
				if (m[1] == "2" && e >= 69) || (m[1] == "3" && e >= 107) {
					hk := fmt.Sprintf("cb%s[%d]", m[1], e)
					if !seen[hk] {
						seen[hk] = true
						high[hk]++
					}
				}
			}
		}
	}
	fmt.Println(n, "shaders")
	pr := func(title string, m map[string]int) {
		ks := make([]string, 0, len(m))
		for k := range m {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		fmt.Print(title, ":")
		for _, k := range ks {
			fmt.Printf(" %s=%d", k, m[k])
		}
		fmt.Println()
	}
	pr("declarations", decl)
	pr("highest element read", maxUse)
	pr("shaders reading high elements", high)
}

// cmdShGrep prints lines matching a pattern (with context) from the first
// few shaders of a zip that have any.
func cmdShGrep(p string, decode bool, suffix, pat string, max, ctx int) {
	re := regexp.MustCompile(pat)
	r, err := zip.OpenReader(p)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer r.Close()
	shown := 0
	for _, f := range r.File {
		name := f.Name
		if decode {
			name = realName(name)
		}
		if !strings.HasSuffix(strings.ToLower(name), suffix) {
			continue
		}
		b, _ := readZipFile(f)
		lines, _ := myDisasm(b)
		hit := false
		for i, l := range lines {
			if re.MatchString(l) {
				if !hit {
					fmt.Println("==", name)
					hit = true
				}
				lo, hi := i-ctx, i+ctx
				if lo < 0 {
					lo = 0
				}
				if hi >= len(lines) {
					hi = len(lines) - 1
				}
				for k := lo; k <= hi; k++ {
					fmt.Printf("%5d %s\n", k, lines[k])
				}
				fmt.Println("  --")
			}
		}
		if hit {
			shown++
			if shown >= max {
				return
			}
		}
	}
}
