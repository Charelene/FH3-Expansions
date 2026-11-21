package main

import (
	"encoding/hex"
	"fmt"
)

// cmdPeek prints a converted bundle's header and the entropy picture of its data.
func cmdPeek(path string, idx int) {
	m, err := openMinizip(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	b, err := m.read(idx)
	if err != nil {
		fmt.Println(err)
		return
	}
	bu, err := parseBundle(b)
	if err != nil {
		fmt.Println(err)
		return
	}
	bl := bu.blobs[0]
	fmt.Printf("entry %d %s v%d data 0x%x\n", idx, bl.tag, bl.major, len(bl.data))
	d := bl.data
	zero := 0
	for _, c := range d {
		if c == 0 {
			zero++
		}
	}
	fmt.Printf("  zero bytes %d of %d\n", zero, len(d))
	for _, o := range []int{0, len(d) / 2, len(d) - 0x400, len(d) - 0x200, len(d) - 0x40} {
		if o < 0 {
			continue
		}
		e := o + 0x20
		if e > len(d) {
			e = len(d)
		}
		fmt.Printf("  +0x%06x %s\n", o, hex.EncodeToString(d[o:e]))
	}
}
