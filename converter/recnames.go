package main

import (
	"fmt"
	"runtime"
	"sort"
	"strings"
	"sync"
)

// cmdRecNames tallies non-bundle minizip records by the extension of their
// leading length-prefixed name, and lists a few names per kind.
func cmdRecNames(path string, limit int) {
	m, err := openMinizip(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	var mu sync.Mutex
	kinds := map[string]int{}
	ex := map[string][]string{}
	jobs := make(chan int, 256)
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			mm := *m
			f, _ := openMinizip(path)
			mm.f = f.f
			for i := range jobs {
				b, err := mm.read(i)
				if err != nil || len(b) < 8 {
					continue
				}
				k, name := "", ""
				if string(b[:4]) == "burG" {
					bu, err := parseBundle(b)
					if err != nil || len(bu.blobs) == 0 {
						k = "burG (unparsed)"
					} else {
						k = "burG first blob " + bu.blobs[0].tag
					}
				} else {
					n := int(le.Uint32(b))
					if n > 0 && n < 300 && 4+n <= len(b) {
						name = string(b[4 : 4+n])
						dot := strings.Index(name, ".")
						if dot >= 0 {
							k = "named *" + strings.ToLower(name[dot:])
						} else {
							k = "named (no extension)"
						}
					} else {
						k = fmt.Sprintf("other, starts %x", b[:4])
					}
				}
				mu.Lock()
				kinds[k]++
				if name != "" && len(ex[k]) < 3 {
					ex[k] = append(ex[k], fmt.Sprintf("%s (entry %d, %d bytes)", name, i, len(b)))
				}
				mu.Unlock()
			}
		}()
	}
	for i := range m.entries {
		if i >= limit {
			break
		}
		jobs <- i
	}
	close(jobs)
	wg.Wait()
	ks := []string{}
	for k := range kinds {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("%7d  %s\n", kinds[k], k)
		for _, e := range ex[k] {
			fmt.Println("           ", e)
		}
	}
}
