package main

import (
	"archive/zip"
	"fmt"
	"io"
	"sort"
	"strings"
)

// cbDecls lists constant buffer declarations as "space:reg[size]" (space 0 for 5.0).
func cbDecls(b []byte) []string {
	var out []string
	for _, c := range dxbcChunks(b) {
		if c.tag != "SHEX" {
			continue
		}
		ver := le.Uint32(c.data) & 0xff
		n := len(c.data) / 4
		tok := func(i int) uint32 { return le.Uint32(c.data[i*4:]) }
		for i := 2; i < n; {
			t := tok(i)
			op := t & 0x7ff
			ln := int(t >> 24 & 0x7f)
			if op == 0x35 {
				ln = int(tok(i + 1))
			}
			if ln == 0 {
				break
			}
			if ver == 0x51 {
				switch op {
				case 0x59:
					out = append(out, fmt.Sprintf("cb sp%d:%d[%d]", tok(i+6), tok(i+3), tok(i+5)))
				case 0x5a:
					if tok(i+5) != 0 {
						out = append(out, fmt.Sprintf("s sp%d:%d", tok(i+5), tok(i+3)))
					}
				case 0x58, 0xa2:
					if tok(i+6) != 0 {
						out = append(out, fmt.Sprintf("t sp%d:%d", tok(i+6), tok(i+3)))
					}
				case 0xa1:
					if tok(i+5) != 0 {
						out = append(out, fmt.Sprintf("t sp%d:%d", tok(i+5), tok(i+3)))
					}
				}
			} else if op == 0x59 {
				out = append(out, fmt.Sprintf("cb %d[%d]", tok(i+2), tok(i+3)))
			}
			i += ln
		}
	}
	sort.Strings(out)
	return out
}

// cmdSpaces compares constant buffer declarations of PC builds with console builds of the same name.
func cmdSpaces(path string, decode bool, show int) {
	r, err := zip.OpenReader(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer r.Close()
	shown := 0
	pat := map[string]int{}
	for _, f := range r.File {
		n := f.Name
		if decode {
			n = realName(n)
		}
		l := strings.ToLower(n)
		if !strings.HasSuffix(l, ".pc.vso") && !strings.HasSuffix(l, ".pc.pso") {
			continue
		}
		base := strings.Replace(l[strings.LastIndex(l, "/")+1:], ".pc.", ".durango.", 1)
		tw, ok := zipOpts.base[base]
		if !ok {
			continue
		}
		rc, _ := f.Open()
		pb, _ := io.ReadAll(rc)
		rc.Close()
		rc, _ = tw.Open()
		cb, _ := io.ReadAll(rc)
		rc.Close()
		p, c := cbDecls(pb), cbDecls(cb)
		// map: for each PC cb "sp:reg[size]" is there a console cb with the same reg?
		creg := map[string]string{}
		for _, x := range c {
			var reg, size int
			fmt.Sscanf(x, "cb %d[%d]", &reg, &size)
			creg[fmt.Sprint(reg)] = fmt.Sprint(size)
		}
		for _, x := range p {
			var sp, reg, size int
			if _, err := fmt.Sscanf(x, "cb sp%d:%d[%d]", &sp, &reg, &size); err != nil {
				pat["non-cb in a space: "+strings.SplitN(x, ":", 2)[0]]++
				continue
			}
			cs, ok := creg[fmt.Sprint(reg)]
			k := fmt.Sprintf("space %d register %d: ", sp, reg)
			switch {
			case !ok:
				k += "console has no cb at that register"
			case cs == fmt.Sprint(size):
				k += "console cb at the same register, same size"
			default:
				k += "console cb at the same register, other size"
			}
			pat[k]++
		}
		if shown < show && strings.Contains(strings.Join(p, " "), "sp1") {
			shown++
			fmt.Printf("%s\n   pc:      %s\n   console: %s\n", base, strings.Join(p, "  "), strings.Join(c, "  "))
		}
	}
	ks := []string{}
	for k := range pat {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("%6d  %s\n", pat[k], k)
	}
}
