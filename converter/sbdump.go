package main

import (
	"archive/zip"
	"encoding/hex"
	"fmt"
	"path"
	"strings"
)

// findInZip returns the first entry whose lower-case base name is want.
func findInZip(p string, decode bool, want string) []byte {
	r, err := zip.OpenReader(p)
	if err != nil {
		return nil
	}
	defer r.Close()
	for _, f := range r.File {
		n := f.Name
		if decode {
			n = realName(n)
		}
		b := strings.ToLower(path.Base(strings.ReplaceAll(n, "\\", "/")))
		if b == want {
			d, _ := readZipFile(f)
			return d
		}
	}
	return nil
}

// cmdSBDump prints the blobs of a shaderbin: tags, metadata, and a hex dump.
func cmdSBDump(p string, decode bool, want string, max int) {
	d := findInZip(p, decode, want)
	if d == nil {
		fmt.Println("not found")
		return
	}
	bu, err := parseBundle(d)
	if err != nil {
		fmt.Println(err)
		return
	}
	for i, bl := range bu.blobs {
		fmt.Printf("blob %d %s %d.%d size %d\n", i, bl.tag, bl.major, bl.minor, len(bl.data))
		for _, m := range bl.metas {
			fmt.Printf("  meta %s lo4=%d w2=%d: %s | %q\n", m.tag, m.lo4, m.word2, hex.EncodeToString(m.payload), printable(m.payload))
		}
		n := len(bl.data)
		if n > max {
			n = max
		}
		fmt.Print(hex.Dump(bl.data[:n]))
	}
}

func printable(b []byte) string {
	o := make([]byte, len(b))
	for i, c := range b {
		if c < 0x20 || c > 0x7e {
			c = '.'
		}
		o[i] = c
	}
	return string(o)
}

// paramMap is one of the shaderbin's PMBC/PMXT/PMPS tables: name hash, slot
// (byte offset for PMBC, register for the others) and parameter GUID.
type paramEntry struct {
	hash uint32
	slot uint16
	guid [16]byte
}

func parseParamMap(d []byte) []paramEntry {
	if len(d) < 1 || len(d) != 1+22*int(d[0]) {
		return nil
	}
	out := make([]paramEntry, d[0])
	for i := range out {
		e := d[1+22*i:]
		out[i].hash = le.Uint32(e)
		out[i].slot = le.Uint16(e[4:])
		copy(out[i].guid[:], e[6:22])
	}
	return out
}

// cmdSBMaps lists the parameter maps of every shaderbin in a zip.
func cmdSBMaps(p string, decode bool) {
	for name, d := range readShaderBins(p, decode) {
		bu, err := parseBundle(d)
		if err != nil {
			continue
		}
		line := name
		for _, bl := range bu.blobs {
			if bl.tag == "PMBC" || bl.tag == "PMXT" || bl.tag == "PMPS" {
				line += " " + bl.tag + "["
				for _, e := range parseParamMap(bl.data) {
					line += fmt.Sprintf("%x ", e.slot)
				}
				line += "]"
			}
		}
		fmt.Println(line)
	}
}

// cmdMyDis prints our own disassembly of one shader file in a zip.
func cmdMyDis(p string, decode bool, want string) {
	d := findInZip(p, decode, want)
	if d == nil {
		fmt.Println("not found")
		return
	}
	s, err := myDisasm(d)
	fmt.Println(strings.Join(s, "\n"))
	if err != nil {
		fmt.Println("error:", err)
	}
}
