package main

import (
	"archive/zip"
	"bytes"
	"encoding/hex"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
)

// cmdNested summarises nested zips and DDS records of a minizip.
func cmdNested(path string, limit int) {
	m, err := openMinizip(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	exts := map[string]int{}
	tex := map[string]int{}
	dds := map[string]int{}
	shownZip, shownDDS := 0, 0
	for i := range m.entries {
		if i >= limit {
			break
		}
		b, err := m.read(i)
		if err != nil || len(b) < 0x80 {
			continue
		}
		switch string(b[:4]) {
		case "PK\x03\x04":
			zr, err := zip.NewReader(bytes.NewReader(b), int64(len(b)))
			if err != nil {
				exts["(unreadable zip)"]++
				continue
			}
			if shownZip < 2 {
				shownZip++
				fmt.Printf("zip entry %d (%d bytes):\n", i, len(b))
				for k, f := range zr.File {
					if k < 12 {
						fmt.Printf("    %s (%d)\n", f.Name, f.UncompressedSize64)
					}
				}
			}
			for _, f := range zr.File {
				e := strings.ToLower(filepath.Ext(f.Name))
				if strings.Contains(strings.ToLower(f.Name), ".pc.") {
					e = ".pc" + e
				} else if strings.Contains(strings.ToLower(f.Name), ".durango.") {
					e = ".durango" + e
				}
				exts[e]++
				if e == ".swatchbin" {
					rc, err := f.Open()
					if err != nil {
						continue
					}
					h := make([]byte, 0x20)
					io.ReadFull(rc, h)
					rc.Close()
					tex[fmt.Sprintf("BCXT v%d.%d", h[0x18], h[0x19])]++
				}
			}
		case "DDS ":
			fourcc := string(b[0x54:0x58])
			k := fmt.Sprintf("%dx%d mips %d fourcc %q flags %08x", le.Uint32(b[0x10:]), le.Uint32(b[0xc:]), le.Uint32(b[0x1c:]), fourcc, le.Uint32(b[0x50:]))
			if fourcc == "DX10" {
				k += fmt.Sprintf(" dxgi %d", le.Uint32(b[0x80:]))
			}
			dds[k]++
			if shownDDS < 2 {
				shownDDS++
				fmt.Printf("dds entry %d (%d bytes): %s\n    %s\n", i, len(b), k, hex.EncodeToString(b[:0x94]))
			}
		}
	}
	pr := func(name string, mm map[string]int) {
		ks := []string{}
		for k := range mm {
			ks = append(ks, k)
		}
		sort.Strings(ks)
		fmt.Println("--", name)
		for _, k := range ks {
			fmt.Printf("  %7d  %s\n", mm[k], k)
		}
	}
	pr("files inside nested zips", exts)
	pr("textures inside nested zips", tex)
	pr("DDS kinds", dds)
}
