package main

import (
	"bytes"
	"compress/zlib"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"sync"
)

type texRef struct {
	file  string
	entry int
	t     *texInfo
	major byte
}

// scanTexHeaders reads only the start of every entry and returns its texture header, if any.
func scanTexHeaders(path string) []texRef {
	m, err := openMinizip(path)
	if err != nil {
		return nil
	}
	defer m.f.Close()
	var mu sync.Mutex
	var out []texRef
	jobs := make(chan int, 256)
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, err := os.Open(path)
			if err != nil {
				return
			}
			defer f.Close()
			for i := range jobs {
				e := m.entries[i]
				n := e.csz
				if n > 0x800 {
					n = 0x800
				}
				raw := make([]byte, n)
				f.ReadAt(raw, e.abs)
				zr, err := zlib.NewReader(bytes.NewReader(raw))
				if err != nil {
					continue
				}
				h := make([]byte, 0x200)
				k, _ := io.ReadFull(zr, h)
				h = h[:k]
				if k < 0x60 || string(h[:4]) != "burG" || string(h[0x14:0x18]) != "BCXT" {
					continue
				}
				mo := int(le.Uint32(h[0x1c:]))
				if mo+8 > k {
					continue
				}
				sz := int(le.Uint16(h[mo+4:]) >> 4)
				if mo+8+sz > k {
					continue
				}
				p := h[mo+8 : mo+8+sz]
				var t *texInfo
				if h[0x18] == 2 {
					t, err = parseXboxHeader(p)
				} else {
					t, err = parsePCHeader(p)
				}
				if err != nil {
					continue
				}
				mu.Lock()
				out = append(out, texRef{path, i, t, h[0x18]})
				mu.Unlock()
			}
		}()
	}
	for i := range m.entries {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	return out
}

func readEntry(path string, i int) (*bundle, error) {
	m, err := openMinizip(path)
	if err != nil {
		return nil, err
	}
	defer m.f.Close()
	b, err := m.read(i)
	if err != nil {
		return nil, err
	}
	return parseBundle(b)
}

// cmdXCheck converts UWP track texture groups that also exist (same GUID and
// group shape) in Xbox track chunks and compares the bytes.
func cmdXCheck(uwp []string, baseDir string, maxSize int64) {
	type key struct {
		g          [16]byte
		w, h, mips uint32
	}
	um := map[key]texRef{}
	for _, f := range uwp {
		for _, r := range scanTexHeaders(f) {
			if r.major == 0 {
				um[key{r.t.guid, r.t.w, r.t.h, r.t.mips}] = r
			}
		}
	}
	fmt.Printf("UWP group bundles: %d\n", len(um))
	files, _ := filepath.Glob(filepath.Join(baseDir, "GeoChunk*.minizip"))
	st := map[string]int{}
	shown := 0
	for _, f := range files {
		fi, _ := os.Stat(f)
		if fi.Size() > maxSize {
			continue
		}
		for _, r := range scanTexHeaders(f) {
			if r.major != 2 {
				continue
			}
			u, ok := um[key{r.t.guid, r.t.w, r.t.h, r.t.mips}]
			if !ok {
				continue
			}
			xb, err1 := readEntry(r.file, r.entry)
			ub, err2 := readEntry(u.file, u.entry)
			if err1 != nil || err2 != nil {
				st["read error"]++
				continue
			}
			hdr, data, err := convertGroup(u.t, ub.blobs[0].data, gval{r.t.layW, r.t.layH, r.t.layMips, r.t.first})
			if err != nil {
				st["convert error: "+err.Error()]++
				continue
			}
			he := bytes.Equal(hdr, xb.blobs[0].metas[0].payload)
			de := bytes.Equal(data, xb.blobs[0].data)
			k := fmt.Sprintf("header equal %v, data equal %v", he, de)
			if !de && len(data) != len(xb.blobs[0].data) {
				k += " (sizes differ)"
			}
			st[k]++
			if (!he || !de) && shown < 6 {
				shown++
				ct, _ := parseXboxHeader(hdr)
				fmt.Printf("%s entry %d vs %s entry %d\n   conv %v data 0x%x\n   xbox %v data 0x%x\n   conv hdr % x\n   xbox hdr % x\n", filepath.Base(u.file), u.entry, filepath.Base(r.file), r.entry, ct, len(data), r.t, len(xb.blobs[0].data), hdr, xb.blobs[0].metas[0].payload)
				if !de && len(data) == len(xb.blobs[0].data) {
					first, n := -1, 0
					for i := range data {
						if data[i] != xb.blobs[0].data[i] {
							n++
							if first < 0 {
								first = i
							}
						}
					}
					fmt.Printf("   %d bytes differ, first at 0x%x\n", n, first)
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
