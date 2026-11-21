package main

import (
	"bytes"
	"fmt"
	"runtime"
	"sync"
)

// cmdVerifyMinizip compares a converted minizip with its source and checks
// converted texture sizes against the converted pvs group spans.
func cmdVerifyMinizip(orig, conv, convPVS string) error {
	_, recs, err := readPVS(convPVS)
	if err != nil {
		return err
	}
	spans := map[[3]uint32]bool{}
	for _, r := range recs {
		gs := r.groupList()
		for gi, g := range gs {
			span := r.total - g.offset
			if gi > 0 {
				span = gs[gi-1].offset - g.offset
			}
			spans[[3]uint32{mipDim(r.w, g.first), mipDim(r.h, g.first), span}] = true
		}
		if r.flags&0x1f != 13 {
			return fmt.Errorf("pvs record at 0x%x still has tile mode %d", r.off, r.flags&0x1f)
		}
	}
	a, err := openMinizip(orig)
	if err != nil {
		return err
	}
	b, err := openMinizip(conv)
	if err != nil {
		return err
	}
	if len(a.entries) != len(b.entries) {
		return fmt.Errorf("entry count %d vs %d", len(a.entries), len(b.entries))
	}
	var mu sync.Mutex
	st := map[string]int{}
	jobs := make(chan int, 256)
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				x, err1 := a.read(i)
				y, err2 := b.read(i)
				k := ""
				switch {
				case err1 != nil || err2 != nil:
					k = fmt.Sprintf("read error %v %v", err1, err2)
				case bytes.Equal(x, y):
					k = "identical"
				default:
					bu, err := parseBundle(y)
					if err != nil || bu.blobs[0].tag != "BCXT" || bu.blobs[0].major != 2 {
						k = "changed but not an Xbox texture"
						break
					}
					t, _ := parseXboxHeader(bu.blobs[0].metas[0].payload)
					if spans[[3]uint32{t.w, t.h, uint32(len(bu.blobs[0].data))}] {
						k = "texture, size matches a pvs group"
					} else {
						k = "texture, size matches no pvs group"
					}
				}
				mu.Lock()
				st[k]++
				mu.Unlock()
			}
		}()
	}
	for i := range a.entries {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	fmt.Println(st)
	return nil
}
