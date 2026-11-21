package main

import (
	"archive/zip"
	"fmt"
	"io"
	"sort"
	"strings"
)

// shaderDecls lists resource declarations of a DXBC shader: "cb1[12]", "t3", "s0", "u0".
func shaderDecls(b []byte) (ver uint32, decls []string) {
	var code []byte
	for _, c := range dxbcChunks(b) {
		if c.tag == "SHEX" || c.tag == "SHDR" {
			code = c.data
		}
	}
	if len(code) < 8 {
		return 0, nil
	}
	ver = le.Uint32(code) & 0xff
	n := int(le.Uint32(code[4:]))
	tok := func(i int) uint32 { return le.Uint32(code[i*4:]) }
	for i := 2; i < n; {
		t := tok(i)
		op := t & 0x7ff
		ln := int(t >> 24 & 0x7f)
		if op == 0x35 { // custom data
			ln = int(tok(i + 1))
		}
		if ln == 0 {
			break
		}
		// operand starts after the opcode token and any extended opcode tokens
		p := i + 1
		for e := t; e&0x80000000 != 0; p++ {
			e = tok(p)
		}
		opnd := func() (typ uint32, idx []uint32) {
			o := tok(p)
			typ = o >> 12 & 0xff
			dim := o >> 20 & 3
			q := p + 1
			if o&0x80000000 != 0 {
				q++
			}
			for d := uint32(0); d < dim; d++ {
				idx = append(idx, tok(q))
				q++
			}
			return
		}
		switch op {
		case 0x59: // dcl_constantbuffer
			_, ix := opnd()
			if ver == 0x51 && len(ix) >= 3 {
				decls = append(decls, fmt.Sprintf("cb%d[%d]", ix[1], tok(i+ln-2)))
			} else if len(ix) >= 2 {
				decls = append(decls, fmt.Sprintf("cb%d[%d]", ix[0], ix[1]))
			}
		case 0x5a, 0x58, 0xa1, 0xa2, 0x9c, 0x9d, 0x9e:
			_, ix := opnd()
			pre := map[uint32]string{0x5a: "s", 0x58: "t", 0xa1: "t(raw)", 0xa2: "t(struct)", 0x9c: "u", 0x9d: "u(raw)", 0x9e: "u(struct)"}[op]
			r := uint32(0)
			extra := ""
			if ver == 0x51 && len(ix) >= 3 {
				r = ix[1]
				if ix[2] != ix[1] {
					extra = fmt.Sprintf("..%d", ix[2])
				}
				if sp := tok(i + ln - 1); sp != 0 {
					extra += fmt.Sprintf(" space%d", sp)
				}
			} else if len(ix) >= 1 {
				r = ix[0]
			}
			dimName := ""
			if op == 0x58 {
				dimName = fmt.Sprintf(":dim%d", t>>11&0x1f)
			}
			decls = append(decls, fmt.Sprintf("%s%d%s%s", pre, r, extra, dimName))
		}
		i += ln
	}
	sort.Strings(decls)
	return
}

func cmdShaderDecls(path string, limit int) {
	r, err := zip.OpenReader(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer r.Close()
	files := map[string][]byte{}
	for _, f := range r.File {
		l := strings.ToLower(f.Name)
		if strings.HasSuffix(l, ".vso") || strings.HasSuffix(l, ".pso") {
			rc, _ := f.Open()
			b, _ := io.ReadAll(rc)
			rc.Close()
			files[l] = b
		}
	}
	names := []string{}
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	same, diff, shown := 0, 0, 0
	for _, n := range names {
		if !strings.Contains(n, ".durango.") {
			continue
		}
		pc, ok := files[strings.Replace(n, ".durango.", ".pc.", 1)]
		if !ok {
			continue
		}
		_, dd := shaderDecls(files[n])
		_, pd := shaderDecls(pc)
		a, b := strings.Join(dd, " "), strings.Join(pd, " ")
		if a == b {
			same++
		} else {
			diff++
		}
		if shown < limit {
			shown++
			fmt.Printf("%s\n   durango: %s\n   pc:      %s\n", n[strings.LastIndex(n, "/")+1:], a, b)
		}
	}
	fmt.Printf("pairs with identical resource declarations: %d, different: %d\n", same, diff)
}
