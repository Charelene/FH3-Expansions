package main

import (
	"archive/zip"
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// cmdWhichShader finds zip entries whose content equals the given file.
func cmdWhichShader(file string, roots []string) {
	want, err := os.ReadFile(file)
	if err != nil {
		fmt.Println(err)
		return
	}
	ver, decls := shaderDecls(want)
	fmt.Printf("shader model 5.%d, %d bytes, declarations: %s\n", ver&0xf, len(want), strings.Join(decls, " "))
	for _, root := range roots {
		filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
			if err != nil || info.IsDir() || !strings.EqualFold(filepath.Ext(p), ".zip") {
				return nil
			}
			r, err := zip.OpenReader(p)
			if err != nil {
				return nil
			}
			defer r.Close()
			for _, f := range r.File {
				if f.UncompressedSize64 != uint64(len(want)) {
					continue
				}
				rc, _ := f.Open()
				b, _ := io.ReadAll(rc)
				rc.Close()
				if bytes.Equal(b, want) {
					fmt.Printf("  %s :: %s\n", p, f.Name)
				}
			}
			return nil
		})
	}
}
