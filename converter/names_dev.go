//go:build dev

package main

// Developer builds only: a name table for media copies with encoded file names,
// read from the file that the environment variable UWP2XBOX_NAMES names (lines
// "encoded<TAB>real", as namesolve -write writes them). Nothing is built in.

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode/utf8"
)

func init() {
	devCommands["namediff"] = cmdNameDiff
	path := os.Getenv("UWP2XBOX_NAMES")
	if path == "" {
		return
	}
	t, err := loadNameTable(path)
	if err != nil {
		fmt.Fprintln(os.Stderr, "UWP2XBOX_NAMES:", err)
		os.Exit(1)
	}
	nameTable = t
	// Path separators are never encoded.
	nameTable['/'] = '/'
}

func loadNameTable(path string) (map[rune]rune, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	t := map[rune]rune{}
	sc := bufio.NewScanner(f)
	for n := 1; sc.Scan(); n++ {
		line := sc.Text()
		if line == "" {
			continue
		}
		a, b, ok := strings.Cut(line, "\t")
		if !ok || utf8.RuneCountInString(a) != 1 || utf8.RuneCountInString(b) != 1 {
			return nil, fmt.Errorf("%s line %d: want one character, a tab, one character", path, n)
		}
		ra, _ := utf8.DecodeRuneInString(a)
		rb, _ := utf8.DecodeRuneInString(b)
		t[ra] = rb
	}
	return t, sc.Err()
}

func listRel(root string, decode bool) map[string]int64 {
	m := map[string]int64{}
	filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(root, p)
		parts := strings.Split(filepath.ToSlash(rel), "/")
		if decode {
			for i := range parts {
				parts[i] = realName(parts[i])
			}
		}
		m[strings.ToLower(strings.Join(parts, "/"))] = info.Size()
		return nil
	})
	return m
}

// cmdNameDiff lists files of an encoded UWP media copy that the Xbox media lacks:
// namediff <encoded media> <xbox media> [filter].
func cmdNameDiff(args []string) error {
	if len(args) < 2 {
		return fmt.Errorf("usage: namediff <encoded media> <xbox media> [filter]")
	}
	if nameTable == nil {
		return fmt.Errorf("no name table: set UWP2XBOX_NAMES to a file written by namesolve -write")
	}
	filter := ""
	if len(args) > 2 {
		filter = args[2]
	}
	u := listRel(args[0], true)
	x := listRel(args[1], false)
	var only []string
	unknown := 0
	for k := range u {
		if strings.Contains(k, "?") {
			unknown++
		}
		if _, ok := x[k]; !ok && strings.Contains(k, filter) {
			only = append(only, k)
		}
	}
	sort.Strings(only)
	fmt.Printf("UWP files %d (names with undecoded characters %d), Xbox files %d, only in UWP (filter %q): %d\n", len(u), unknown, len(x), filter, len(only))
	for i, k := range only {
		if i >= 900 {
			fmt.Println("...")
			break
		}
		fmt.Printf("  %10d  %s\n", u[k], k)
	}
	return nil
}
