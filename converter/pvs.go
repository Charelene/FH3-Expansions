package main

import (
	"errors"
	"fmt"
	"os"
)

// Track texture table in Ribbon_*\*.pvs: u32 count, then count records of 0x38 bytes:
//
//	+0x00 u32 id, +0x04 u32, +0x08 u16 width, +0x0a u16 height, +0x0c u16 depth,
//	+0x0e u8 mips, +0x0f u8, +0x10 u32 total bytes, +0x14 u32 flags (header bit layout),
//	+0x18 u32[8] streaming groups, lowest quality first: count << 28 | byte offset.
type pvsRec struct {
	off           int
	w, h, d, mips uint32
	total, flags  uint32
	groups        []uint32
}

type pvsGroup struct{ first, count, offset uint32 }

func (r *pvsRec) groupList() []pvsGroup {
	var gs []pvsGroup
	first := r.mips
	for _, g := range r.groups {
		n := g >> 28
		if n == 0 {
			break
		}
		first -= n
		gs = append(gs, pvsGroup{first: first, count: n, offset: g & 0xfffffff})
	}
	return gs
}

func recOK(b []byte, r int) bool {
	if r+0x38 > len(b) {
		return false
	}
	pow2 := func(v uint16) bool { return v != 0 && v&(v-1) == 0 && v <= 16384 }
	w, h, d := le.Uint16(b[r+8:]), le.Uint16(b[r+10:]), le.Uint16(b[r+12:])
	return pow2(w) && pow2(h) && d >= 1 && d <= 16 && b[r+14] >= 1 && b[r+14] <= 15 && le.Uint32(b[r+0x10:]) > 0 && le.Uint32(b[r+0x18:])>>28 != 0
}

// findPVSTable locates the texture table: a u32 count followed by that many valid records.
func findPVSTable(b []byte) (int, int, error) {
	for i := 4; i+0x38*3 < len(b); i++ {
		n := int(le.Uint32(b[i-4:]))
		if n < 1 || n > 200000 || i+n*0x38 > len(b) {
			continue
		}
		if !recOK(b, i) || !recOK(b, i+0x38) {
			continue
		}
		all := true
		for k := 0; k < n; k++ {
			if !recOK(b, i+k*0x38) {
				all = false
				break
			}
		}
		if all {
			return i, n, nil
		}
	}
	return 0, 0, errors.New("no texture table found")
}

func readPVS(path string) ([]byte, []pvsRec, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, err
	}
	b2, recs, err := readPVSBytes(b)
	if err != nil {
		return nil, nil, fmt.Errorf("%s: %w", path, err)
	}
	return b2, recs, nil
}

func readPVSBytes(b []byte) ([]byte, []pvsRec, error) {
	start, n, err := findPVSTable(b)
	if err != nil {
		return nil, nil, err
	}
	recs := make([]pvsRec, n)
	for k := range recs {
		r := start + k*0x38
		p := pvsRec{off: r, w: uint32(le.Uint16(b[r+8:])), h: uint32(le.Uint16(b[r+10:])), d: uint32(le.Uint16(b[r+12:])), mips: uint32(b[r+14]),
			total: le.Uint32(b[r+0x10:]), flags: le.Uint32(b[r+0x14:])}
		for g := 0; g < 8; g++ {
			p.groups = append(p.groups, le.Uint32(b[r+0x18+g*4:]))
		}
		recs[k] = p
	}
	return b, recs, nil
}

// bundle key: what a UWP group bundle tells about itself.
type gkey struct{ w, h, count, a, b, c uint32 }
type gval struct{ layW, layH, layMips, first uint32 }

func groupCandidates(recs []pvsRec) map[gkey]map[gval]int {
	m := map[gkey]map[gval]int{}
	for _, r := range recs {
		for _, g := range r.groupList() {
			k := gkey{mipDim(r.w, g.first), mipDim(r.h, g.first), g.count, r.flags >> 5 & 0x3f, r.flags >> 11 & 0x3f, r.flags >> 20 & 7}
			if m[k] == nil {
				m[k] = map[gval]int{}
			}
			m[k][gval{r.w, r.h, r.mips, g.first}]++
		}
	}
	return m
}

func cmdPVSCheck(pvsPath string, minizips []string) {
	_, recs, err := readPVS(pvsPath)
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("%d texture records\n", len(recs))
	cand := groupCandidates(recs)
	amb := 0
	for k, v := range cand {
		if len(v) > 1 {
			amb++
			if amb <= 8 {
				fmt.Printf("ambiguous key %+v -> %v\n", k, v)
			}
		}
	}
	fmt.Printf("group keys %d, ambiguous %d\n", len(cand), amb)
	st := map[string]int{}
	for _, mz := range minizips {
		m, err := openMinizip(mz)
		if err != nil {
			fmt.Println(err)
			continue
		}
		for i := range m.entries {
			b, err := m.read(i)
			if err != nil || len(b) < 4 || string(b[:4]) != "burG" {
				continue
			}
			bu, err := parseBundle(b)
			if err != nil || bu.blobs[0].tag != "BCXT" || bu.blobs[0].major != 0 {
				continue
			}
			t, err := parsePCHeader(bu.blobs[0].metas[0].payload)
			if err != nil {
				st["bad header"]++
				continue
			}
			c := cand[gkey{t.w, t.h, t.mips, t.a, t.b, t.c}]
			switch {
			case len(c) == 0:
				st["no record"]++
			case len(c) == 1:
				st["unique"]++
			default:
				st["ambiguous"]++
			}
		}
	}
	fmt.Println(st)
}
