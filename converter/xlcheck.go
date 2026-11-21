package main

import (
	"archive/zip"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

// d3dDevice is a D3D11 device used only to ask the runtime whether it
// accepts a shader.
type d3dDevice struct{ p uintptr }

func newD3DDevice() (*d3dDevice, error) {
	dll := syscall.NewLazyDLL("d3d11.dll")
	create := dll.NewProc("D3D11CreateDevice")
	var dev uintptr
	for _, driver := range []uintptr{1, 5} { // hardware, then WARP
		hr, _, _ := create.Call(0, driver, 0, 0, 0, 0, 7, uintptr(unsafe.Pointer(&dev)), 0, 0)
		if int32(hr) >= 0 && dev != 0 {
			return &d3dDevice{dev}, nil
		}
	}
	return nil, fmt.Errorf("no D3D11 device")
}

func (d *d3dDevice) method(i int) uintptr {
	vt := *(*uintptr)(unsafe.Pointer(d.p))
	return *(*uintptr)(unsafe.Pointer(vt + uintptr(i)*unsafe.Sizeof(uintptr(0))))
}

// accepts creates and releases a shader object; pixel selects the stage.
func (d *d3dDevice) accepts(b []byte, pixel bool) error {
	slot := 12
	if pixel {
		slot = 15
	}
	var sh uintptr
	hr, _, _ := syscall.SyscallN(d.method(slot), d.p, uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0, uintptr(unsafe.Pointer(&sh)))
	if int32(hr) < 0 {
		return fmt.Errorf("hresult %#x", uint32(hr))
	}
	if sh != 0 {
		vt := *(*uintptr)(unsafe.Pointer(sh))
		rel := *(*uintptr)(unsafe.Pointer(vt + 2*unsafe.Sizeof(uintptr(0))))
		syscall.SyscallN(rel, sh)
	}
	return nil
}

var (
	reTexDecl = regexp.MustCompile(`^dcl_resource t0:(\d+) `)
	reCB0Decl = regexp.MustCompile(`^dcl_constantbuffer cb0:0\[(\d+)\]`)
	reCB0Use  = regexp.MustCompile(`cb0:0\[(\d+)\]\.([xyzw]+)`)
	reSampler = regexp.MustCompile(`^dcl_sampler s0:(\d+)`)
)

// materialUse describes how a console-form shader reaches its material:
// texture registers 16-95, material constant components, samplers 12-15.
func materialUse(lines []string) (tex, consts, samp string) {
	ts, cs, ss := map[int]bool{}, map[string]bool{}, map[int]bool{}
	for _, l := range lines {
		if m := reTexDecl.FindStringSubmatch(l); m != nil {
			var n int
			fmt.Sscan(m[1], &n)
			if n >= 16 && n < 96 {
				ts[n] = true
			}
			continue
		}
		if m := reSampler.FindStringSubmatch(l); m != nil {
			var n int
			fmt.Sscan(m[1], &n)
			if n >= 12 {
				ss[n] = true
			}
			continue
		}
		if reCB0Decl.MatchString(l) {
			continue
		}
		for _, m := range reCB0Use.FindAllStringSubmatch(l, -1) {
			for _, c := range m[2] {
				cs[m[1]+"."+string(c)] = true
			}
		}
	}
	ints := func(m map[int]bool) string {
		k := make([]int, 0, len(m))
		for v := range m {
			k = append(k, v)
		}
		sort.Ints(k)
		return fmt.Sprint(k)
	}
	k := make([]string, 0, len(cs))
	for v := range cs {
		k = append(k, v)
	}
	sort.Strings(k)
	return ints(ts), strings.Join(k, " "), ints(ss)
}

// cmdXlateCheck translates every PC shader of a zip, asks D3D11 whether it
// accepts the result, and compares the material bindings with the console
// build of the same name where the base game has one.
func cmdXlateCheck(p string, decode bool, show int) {
	dev, err := newD3DDevice()
	if err != nil {
		fmt.Println(err)
	}
	r, err := zip.OpenReader(p)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer r.Close()
	tally := map[string]int{}
	shown := map[string]int{}
	note := func(key, detail string) {
		tally[key]++
		if shown[key] < show && detail != "" {
			shown[key]++
			fmt.Printf("[%s] %s\n", key, detail)
		}
	}
	for _, f := range r.File {
		name := f.Name
		if decode {
			name = realName(name)
		}
		l := strings.ToLower(name)
		base := l[strings.LastIndexAny(l, "/\\")+1:]
		pixel := strings.HasSuffix(l, ".pc.pso")
		if !pixel && !strings.HasSuffix(l, ".pc.vso") {
			continue
		}
		kind := "vso"
		if pixel {
			kind = "pso"
		}
		b, _ := readZipFile(f)
		nb, _, err := shaderPCToConsole(b)
		if err != nil {
			note(kind+" translation failed", base+": "+err.Error())
			continue
		}
		lines, derr := myDisasm(nb)
		if derr != nil || strings.Contains(strings.Join(lines, "\n"), "parse mismatch") {
			note(kind+" result does not disassemble", base)
			continue
		}
		if dev != nil {
			if err := dev.accepts(nb, pixel); err != nil {
				note(kind+" refused by D3D11", base+": "+err.Error())
			} else {
				note(kind+" accepted by D3D11", "")
			}
		}
		tw, ok := zipOpts.base[strings.Replace(base, ".pc.", ".durango.", 1)]
		if !ok {
			note(kind+" no console build to compare", "")
			continue
		}
		cb, _ := readZipFile(tw)
		cl, _ := myDisasm(cb)
		t1, c1, s1 := materialUse(lines)
		t2, c2, s2 := materialUse(cl)
		switch {
		case t1 == t2 && c1 == c2 && s1 == s2:
			note(kind+" material bindings equal to the console build", "")
		case t1 != t2:
			note(kind+" textures differ from the console build", fmt.Sprintf("%s: ours %s console %s", base, t1, t2))
		case c1 != c2:
			note(kind+" constants differ from the console build", fmt.Sprintf("%s: ours {%s} console {%s}", base, c1, c2))
		default:
			note(kind+" samplers differ from the console build", fmt.Sprintf("%s: ours %s console %s", base, s1, s2))
		}
	}
	ks := make([]string, 0, len(tally))
	for k := range tally {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("%6d  %s\n", tally[k], k)
	}
}
