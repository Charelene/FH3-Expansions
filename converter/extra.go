package main

import (
	"bytes"
	"fmt"
	"os"
)

// cmdZipExtra prints the central-directory 0x1123 extra value next to the
// entry's local header offset and data offset.
func cmdZipExtra(path string, n int) error {
	raw, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	eocd := bytes.LastIndex(raw, []byte("PK\x05\x06"))
	cnt := int(le.Uint16(raw[eocd+10:]))
	p := int(le.Uint32(raw[eocd+16:]))
	bad := 0
	for i := 0; i < cnt; i++ {
		nl, el, cl := int(le.Uint16(raw[p+28:])), int(le.Uint16(raw[p+30:])), int(le.Uint16(raw[p+32:]))
		lo := int(le.Uint32(raw[p+42:]))
		lnl, lel := int(le.Uint16(raw[lo+26:])), int(le.Uint16(raw[lo+28:]))
		dataOff := lo + 30 + lnl + lel
		ex := raw[p+46+nl : p+46+nl+el]
		var v uint32
		var id uint16
		if len(ex) >= 8 {
			id, v = le.Uint16(ex), le.Uint32(ex[4:])
		}
		if int(v) != dataOff {
			bad++
		}
		if i < n || (int(v) != dataOff && bad <= 5) {
			fmt.Printf("%4d local 0x%08x data 0x%08x extra id 0x%04x value 0x%08x  %s\n", i, lo, dataOff, id, v, string(raw[p+46:p+46+nl]))
		}
		p += 46 + nl + el + cl
	}
	fmt.Printf("%d entries, %d where the extra value is not the data offset\n", cnt, bad)
	return nil
}

// zipExtraCount returns the number of entries of a zip and how many of them
// have a 0x1123 extra value that is not their data offset.
func zipExtraCount(path string) (int, int, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		return 0, 0, err
	}
	eocd := bytes.LastIndex(raw, []byte("PK\x05\x06"))
	if eocd < 0 || eocd+22 > len(raw) {
		return 0, 0, fmt.Errorf("%s: no central directory", path)
	}
	cnt := int(le.Uint16(raw[eocd+10:]))
	p := int(le.Uint32(raw[eocd+16:]))
	bad := 0
	for i := 0; i < cnt; i++ {
		if p+46 > len(raw) {
			return cnt, bad, fmt.Errorf("%s: central directory cut short", path)
		}
		nl, el, cl := int(le.Uint16(raw[p+28:])), int(le.Uint16(raw[p+30:])), int(le.Uint16(raw[p+32:]))
		lo := int(le.Uint32(raw[p+42:]))
		if lo+30 > len(raw) {
			return cnt, bad, fmt.Errorf("%s: entry %d outside the file", path, i)
		}
		dataOff := lo + 30 + int(le.Uint16(raw[lo+26:])) + int(le.Uint16(raw[lo+28:]))
		ex := raw[p+46+nl : p+46+nl+el]
		var v uint32
		if len(ex) >= 8 {
			v = le.Uint32(ex[4:])
		}
		if int(v) != dataOff {
			bad++
		}
		p += 46 + nl + el + cl
	}
	return cnt, bad, nil
}
