package main

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
)

// cmdPadCheck tests whether flag bits 14-15 equal the bytes left over after
// each entry's zlib stream within its slot.
func cmdPadCheck(path string, limit int) {
	m, err := openMinizip(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	match, miss := 0, 0
	for i := range m.entries {
		if i >= limit {
			break
		}
		raw, err := m.raw(i)
		if err != nil {
			continue
		}
		br := bytes.NewReader(raw)
		zr, err := zlib.NewReader(br)
		if err != nil {
			continue
		}
		io.Copy(io.Discard, zr)
		zr.Close()
		left := br.Len()
		bits := int(m.entries[i].flags >> 14 & 3)
		if left == bits {
			match++
		} else {
			miss++
			if miss <= 5 {
				fmt.Printf("entry %d: slot 0x%x, %d bytes after the stream, flag bits %d\n", i, m.entries[i].csz, left, bits)
			}
		}
	}
	fmt.Printf("%s: flag bits = trailing bytes for %d entries, not for %d\n", path, match, miss)
}

// cmdMzOrder reports whether entries lie in index order and how the file ends.
func cmdMzOrder(path string) {
	m, err := openMinizip(path)
	if err != nil {
		fmt.Println(path, err)
		return
	}
	defer m.f.Close()
	bad := 0
	for i := 1; i < len(m.entries); i++ {
		if m.entries[i].abs <= m.entries[i-1].abs {
			bad++
		}
	}
	unaligned := 0
	for _, e := range m.entries {
		if e.abs%4 != 0 {
			unaligned++
		}
	}
	st, _ := m.f.Stat()
	fmt.Printf("%s: %d entries, out of order %d, offsets not 4-aligned %d, data end 0x%x, file size 0x%x (4 KiB multiple: %v, tail %d bytes)\n",
		path, len(m.entries), bad, unaligned, m.end, st.Size(), st.Size()%0x1000 == 0, st.Size()-m.end)
}
