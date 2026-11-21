package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

var procDisasm *syscall.Proc

// disasm runs Microsoft's D3DDisassemble on a shader.
func disasm(b []byte) (string, error) {
	if procDisasm == nil {
		d, err := syscall.LoadDLL("d3dcompiler_47.dll")
		if err != nil {
			return "", err
		}
		if procDisasm, err = d.FindProc("D3DDisassemble"); err != nil {
			return "", err
		}
	}
	var blob unsafe.Pointer
	r, _, _ := procDisasm.Call(uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0, 0, uintptr(unsafe.Pointer(&blob)))
	if int32(r) < 0 || blob == nil {
		return "", fmt.Errorf("D3DDisassemble hr=0x%08x", uint32(r))
	}
	vt := *(*unsafe.Pointer)(blob)
	fn := func(slot int) uintptr {
		f := *(*uintptr)(unsafe.Add(vt, slot*8))
		x, _, _ := syscall.SyscallN(f, uintptr(blob))
		return x
	}
	p, n := fn(3), fn(4)
	s := string(unsafe.Slice((*byte)(unsafe.Pointer(p)), n))
	fn(2)
	return s, nil
}

// code lines of a disassembly: no comments, no declarations.
func codeLines(s string) (decl, code []string) {
	for _, l := range strings.Split(s, "\n") {
		l = strings.TrimSpace(l)
		if l == "" || strings.HasPrefix(l, "//") {
			continue
		}
		if strings.HasPrefix(l, "dcl_") {
			decl = append(decl, l)
		} else {
			code = append(code, l)
		}
	}
	return
}

func cmdShaderTest(path string, decode bool, show int) {
	r, err := zip.OpenReader(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer r.Close()
	files := map[string][]byte{}
	for _, f := range r.File {
		n := f.Name
		if decode {
			n = realName(n)
		}
		l := strings.ToLower(n)
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
	st := map[string]int{}
	shown := 0
	for _, n := range names {
		b := files[n]
		sum := dxbcChecksum(b)
		if bytes.Equal(sum[:], b[4:20]) {
			st["checksum reproduced"]++
		} else {
			st["checksum NOT reproduced"]++
		}
		if !strings.Contains(n, ".pc.") {
			continue
		}
		c, err := shader51to50(b)
		if err != nil {
			st["convert error: "+err.Error()]++
			continue
		}
		da, err1 := disasm(b)
		db, err2 := disasm(c)
		if err1 != nil || err2 != nil {
			st[fmt.Sprintf("disassembler refused: original %v, converted %v", err1, err2)]++
			continue
		}
		_, ca := codeLines(da)
		dcl, cb := codeLines(db)
		if len(ca) != len(cb) {
			st["instruction count changed"]++
			continue
		}
		st["converted, disassembles, same instruction count"]++
		if tw, ok := files[strings.Replace(n, ".pc.", ".durango.", 1)]; ok {
			_, d1 := shaderDecls(c)
			_, d2 := shaderDecls(tw)
			// compare register sets ignoring constant buffer sizes
			strip := func(x []string) string {
				y := []string{}
				for _, s := range x {
					if i := strings.Index(s, "["); i > 0 {
						s = s[:i]
					}
					y = append(y, s)
				}
				return strings.Join(y, " ")
			}
			if strip(d1) == strip(d2) {
				st["  registers equal to the console twin's"]++
			} else {
				st["  registers differ from the console twin's"]++
			}
		}
		if shown < show {
			shown++
			fmt.Printf("== %s (%d -> %d bytes)\n", n[strings.LastIndex(n, "/")+1:], len(b), len(c))
			for i, l := range dcl {
				if i < 14 {
					fmt.Println("   ", l)
				}
			}
			for i := range ca {
				if ca[i] != cb[i] && i < len(ca) {
					fmt.Printf("    5.1: %s\n    5.0: %s\n", ca[i], cb[i])
					break
				}
			}
		}
	}
	ks := []string{}
	for k := range st {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("%6d  %s\n", st[k], k)
	}
}
