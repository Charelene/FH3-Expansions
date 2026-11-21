package main

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
)

// cmdMzDiff compares two minizips entry by entry, decompressed.
func cmdMzDiff(pa, pb string, show int) {
	a, err := openMinizip(pa)
	if err != nil {
		fmt.Println(err)
		return
	}
	b, err := openMinizip(pb)
	if err != nil {
		fmt.Println(err)
		return
	}
	if len(a.entries) != len(b.entries) {
		fmt.Println("entry counts differ", len(a.entries), len(b.entries))
		return
	}
	rawDiff, dataDiff, shown := 0, 0, 0
	for i := range a.entries {
		ra, _ := a.raw(i)
		rb, _ := b.raw(i)
		if bytes.Equal(ra, rb) && a.entries[i].flags == b.entries[i].flags {
			continue
		}
		rawDiff++
		da, ea := inflate(ra)
		db, eb := inflate(rb)
		if ea != nil || eb != nil || !bytes.Equal(da, db) {
			dataDiff++
			if shown < show {
				shown++
				first := -1
				for k := 0; k < len(da) && k < len(db); k++ {
					if da[k] != db[k] {
						first = k
						break
					}
				}
				fmt.Printf("entry %d: usize %d/%d, first difference at %d, errors %v %v\n", i, len(da), len(db), first, ea, eb)
				cnt := 0
				var pos []string
				for k := 0; k < len(da) && k < len(db); k++ {
					if da[k] != db[k] {
						cnt++
						if len(pos) < 12 {
							pos = append(pos, fmt.Sprintf("%d:%02x/%02x", k, da[k], db[k]))
						}
					}
				}
				fmt.Println("    differing bytes:", cnt, pos)
				if ba, e := parseBundle(da); e == nil {
					for _, bl := range ba.blobs {
						fmt.Printf("    blob %s %d.%d %d bytes, %d metas\n", bl.tag, bl.major, bl.minor, len(bl.data), len(bl.metas))
						for _, m := range bl.metas {
							fmt.Printf("      meta %s %x\n", m.tag, m.payload)
						}
					}
				}
			}
		}
	}
	fmt.Printf("%d entries, %d differ in the stored bytes, %d differ once decompressed\n", len(a.entries), rawDiff, dataDiff)
}

func inflate(b []byte) ([]byte, error) {
	r, err := zlib.NewReader(bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	return io.ReadAll(r)
}
