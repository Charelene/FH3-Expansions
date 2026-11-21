package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
)

// cmdZipCheck compares two zips entry by entry and checks that every local
// header matches the original's shape (no extra field added).
func cmdZipCheck(orig, conv string) error {
	a, err := zip.OpenReader(orig)
	if err != nil {
		return err
	}
	defer a.Close()
	b, err := zip.OpenReader(conv)
	if err != nil {
		return err
	}
	defer b.Close()
	if len(a.File) != len(b.File) {
		return fmt.Errorf("%d vs %d entries", len(a.File), len(b.File))
	}
	raw, _ := os.ReadFile(conv)
	same, changed, extra := 0, 0, 0
	for i := range a.File {
		fa, fb := a.File[i], b.File[i]
		if fa.Name != fb.Name {
			return fmt.Errorf("entry %d name %s vs %s", i, fa.Name, fb.Name)
		}
		off, _ := fb.DataOffset()
		lo := off - 30 - int64(len(fb.Name))
		if lo < 0 || string(raw[lo:lo+4]) != "PK\x03\x04" {
			extra++
		}
		ra, _ := fa.Open()
		da, _ := io.ReadAll(ra)
		ra.Close()
		rb, _ := fb.Open()
		db, err := io.ReadAll(rb)
		rb.Close()
		if err != nil {
			return fmt.Errorf("%s: %w", fb.Name, err)
		}
		if bytes.Equal(da, db) {
			same++
		} else {
			changed++
		}
	}
	fmt.Printf("entries %d: identical %d, converted %d, local headers with extra data %d\n", len(a.File), same, changed, extra)
	return nil
}
