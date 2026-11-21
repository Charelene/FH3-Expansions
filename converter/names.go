package main

import "strings"

// nameTable maps each character of an encoded file name to the real one. Release
// builds have none and read real names only; developer builds load a table that
// namesolve learned (names_dev.go).
var nameTable map[rune]rune

// realName decodes a name with nameTable; a character the table lacks comes out
// as "?" followed by itself. Without a table the name is returned unchanged.
func realName(s string) string {
	if nameTable == nil {
		return s
	}
	var b strings.Builder
	for _, c := range s {
		if p, ok := nameTable[c]; ok {
			b.WriteRune(p)
		} else {
			b.WriteString("?" + string(c))
		}
	}
	return b.String()
}
