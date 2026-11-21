package main

import (
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"sync"
)

// cmdTreeCmp compares two folders file by file (names, sizes, SHA-256).
// Files that are the same file on disk (hard links) are skipped.
func cmdTreeCmp(a, b string) {
	list := func(root string) map[string]os.FileInfo {
		m := map[string]os.FileInfo{}
		filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err == nil && !info.IsDir() {
				rel, _ := filepath.Rel(root, p)
				m[rel] = info
			}
			return nil
		})
		return m
	}
	la, lb := list(a), list(b)
	var names []string
	for k := range la {
		names = append(names, k)
	}
	sort.Strings(names)
	var mu sync.Mutex
	var wg sync.WaitGroup
	sem := make(chan struct{}, 8)
	same, linked, diff, missing := 0, 0, 0, 0
	for k := range lb {
		if _, ok := la[k]; !ok {
			fmt.Println("only in second:", k)
			missing++
		}
	}
	sum := func(p string) [32]byte {
		var out [32]byte
		f, err := os.Open(p)
		if err != nil {
			return out
		}
		defer f.Close()
		h := sha256.New()
		io.Copy(h, f)
		copy(out[:], h.Sum(nil))
		return out
	}
	for _, k := range names {
		ia := la[k]
		ib, ok := lb[k]
		if !ok {
			fmt.Println("only in first:", k)
			missing++
			continue
		}
		if os.SameFile(ia, ib) {
			linked++
			continue
		}
		if ia.Size() != ib.Size() {
			fmt.Printf("size differs: %s (%d, %d)\n", k, ia.Size(), ib.Size())
			diff++
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(k string) {
			defer wg.Done()
			defer func() { <-sem }()
			eq := sum(filepath.Join(a, k)) == sum(filepath.Join(b, k))
			mu.Lock()
			if eq {
				same++
			} else {
				fmt.Println("content differs:", k)
				diff++
			}
			mu.Unlock()
		}(k)
	}
	wg.Wait()
	fmt.Printf("%d files compared by content and equal, %d the same file (hard link), %d different, %d missing on one side\n", same, linked, diff, missing)
}
