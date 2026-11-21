package main

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
)

// findInMinizip searches every decompressed entry for pat.
func findInMinizip(path string, pat []byte) {
	m, err := openMinizip(path)
	if err != nil {
		fmt.Println(err)
		return
	}
	jobs := make(chan int, 256)
	var mu sync.Mutex
	var wg sync.WaitGroup
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, _ := os.Open(path)
			defer f.Close()
			mm := *m
			mm.f = f
			for i := range jobs {
				b, err := mm.read(i)
				if err != nil {
					continue
				}
				for off := 0; ; {
					k := bytes.Index(b[off:], pat)
					if k < 0 {
						break
					}
					mu.Lock()
					kind := "record"
					if len(b) >= 4 && string(b[:4]) == "burG" {
						kind = "burG"
					}
					fmt.Printf("%s entry %d (%s, %d bytes, flags 0x%x) at 0x%x\n", filepath.Base(path), i, kind, len(b), m.entries[i].flags, off+k)
					mu.Unlock()
					off += k + 1
				}
			}
		}()
	}
	for i := range m.entries {
		jobs <- i
	}
	close(jobs)
	wg.Wait()
}

func findInFiles(dir string, pat []byte) {
	filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || filepath.Ext(p) == ".minizip" || info.Size() > 256<<20 {
			return nil
		}
		b, _ := os.ReadFile(p)
		for off := 0; ; {
			k := bytes.Index(b[off:], pat)
			if k < 0 {
				break
			}
			fmt.Printf("%s at 0x%x\n", p, off+k)
			off += k + 1
		}
		return nil
	})
}

func cmdFind(args []string) {
	pat, err := hex.DecodeString(args[1])
	if err != nil {
		fmt.Println(err)
		return
	}
	fi, _ := os.Stat(args[0])
	if fi != nil && fi.IsDir() {
		findInFiles(args[0], pat)
	} else {
		findInMinizip(args[0], pat)
	}
}
