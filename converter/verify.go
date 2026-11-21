package main

import (
	"bytes"
	"fmt"
	"path/filepath"
)

// cmdTestMinizip converts one minizip with its track's pvs, then checks the result.
func cmdTestMinizip(src, dst string) error {
	dir := filepath.Dir(src)
	pv, _ := filepath.Glob(filepath.Join(dir, "Ribbon_*", "*.pvs"))
	c := map[gkey]map[gval]int{}
	var spans = map[[3]uint32]bool{} // top w, h, data size of converted groups
	for _, f := range pv {
		b, recs, err := readPVS(f)
		if err != nil {
			return err
		}
		for k, v := range groupCandidates(recs) {
			if c[k] == nil {
				c[k] = map[gval]int{}
			}
			for vv, n := range v {
				c[k][vv] += n
			}
		}
		if err := convertPVS(b, recs); err != nil {
			return err
		}
		_, nrecs, _ := readPVSBytes(b)
		for _, r := range nrecs {
			gs := r.groupList()
			for gi, g := range gs {
				var span uint32
				if gi == 0 {
					span = r.total - g.offset
				} else {
					span = gs[gi-1].offset - g.offset
				}
				spans[[3]uint32{mipDim(r.w, g.first), mipDim(r.h, g.first), span}] = true
			}
		}
	}
	st := &stats{}
	if err := convertMinizip(src, dst, c, st); err != nil {
		return err
	}
	st.print()
	a, err := openMinizip(src)
	if err != nil {
		return err
	}
	b, err := openMinizip(dst)
	if err != nil {
		return err
	}
	same, conv, bad, spanOK, spanBad := 0, 0, 0, 0, 0
	for i := range a.entries {
		x, err1 := a.read(i)
		y, err2 := b.read(i)
		if err1 != nil || err2 != nil {
			bad++
			fmt.Println("read error", i, err1, err2)
			continue
		}
		if bytes.Equal(x, y) {
			same++
			continue
		}
		bu, err := parseBundle(y)
		if err != nil || bu.blobs[0].tag != "BCXT" || bu.blobs[0].major != 2 {
			bad++
			continue
		}
		conv++
		t, _ := parseXboxHeader(bu.blobs[0].metas[0].payload)
		if spans[[3]uint32{t.w, t.h, uint32(len(bu.blobs[0].data))}] {
			spanOK++
		} else {
			spanBad++
			if spanBad <= 5 {
				fmt.Printf("no pvs group with %dx%d span 0x%x (entry %d, %v)\n", t.w, t.h, len(bu.blobs[0].data), i, t)
			}
		}
	}
	fmt.Printf("entries %d: identical %d, converted %d (span matches a pvs group %d, no match %d), bad %d\n", len(a.entries), same, conv, spanOK, spanBad, bad)
	return nil
}
