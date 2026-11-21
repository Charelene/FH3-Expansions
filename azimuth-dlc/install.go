package main

// Install mode: everything one Azimuth folder needs to load the converted
// expansions. Besides the patch of azimuth.exe that is one line in
// data\azimuth.ini, the FH3 title's
//
//	content=<folder>*<sha256 of its AppxManifest.xml>|<folder>*<sha256>
//
// The ini is UTF-16 LE with a byte order mark and CRLF line ends, and is
// written back the same way. The ini from before the first run is kept as
// azimuth.ini.bak-fh3dlc; -revert puts that file's content= value back.

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"unicode/utf16"
)

const fh3Section = "[title-4ce1dfab]"

func readIni(path string) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if len(b) < 2 || b[0] != 0xff || b[1] != 0xfe || len(b)%2 != 0 {
		return nil, fmt.Errorf("%s is not UTF-16 text", path)
	}
	u := make([]uint16, (len(b)-2)/2)
	for i := range u {
		u[i] = uint16(b[2+2*i]) | uint16(b[3+2*i])<<8
	}
	s := strings.ReplaceAll(string(utf16.Decode(u)), "\r\n", "\n")
	s = strings.TrimSuffix(s, "\n")
	return strings.Split(s, "\n"), nil
}

func writeIni(path string, lines []string) error {
	u := utf16.Encode([]rune(strings.Join(lines, "\r\n") + "\r\n"))
	b := make([]byte, 2+2*len(u))
	b[0], b[1] = 0xff, 0xfe
	for i, v := range u {
		b[2+2*i], b[3+2*i] = byte(v), byte(v>>8)
	}
	tmp := path + ".tmp-fh3dlc"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// section returns the line range (first, last) of the FH3 title's keys.
func section(lines []string) (int, int, bool) {
	start := -1
	for i, l := range lines {
		if strings.EqualFold(strings.TrimSpace(l), fh3Section) {
			start = i
			break
		}
	}
	if start < 0 {
		return 0, 0, false
	}
	end := len(lines) - 1
	for i := start + 1; i < len(lines); i++ {
		if strings.HasPrefix(strings.TrimSpace(lines[i]), "[") {
			end = i - 1
			break
		}
	}
	return start + 1, end, true
}

// key finds "name=value" inside a line range; index -1 when absent.
func key(lines []string, first, last int, name string) (int, string) {
	for i := first; i <= last && i < len(lines); i++ {
		k, v, ok := strings.Cut(lines[i], "=")
		if ok && strings.EqualFold(strings.TrimSpace(k), name) {
			return i, strings.TrimSpace(v)
		}
	}
	return -1, ""
}

// modIni is what the fh3-expansions dinput8.dll (mod/fh3exp.c) reads next to azimuth.exe: per package identity
// the SHA-256 of its AppxManifest.xml, its tree digest and its version. The DLL puts them into FH3's
// content-package list in memory, so azimuth.exe on disk stays the original (Extra Options and other mods check
// its hash).
const modIni = "fh3-expansions.ini"

func writeModIni(path string, pk []pkg) error {
	var b strings.Builder
	b.WriteString("; Written by azimuth-dlc for fh3-expansions (dinput8.dll next to azimuth.exe).\r\n")
	b.WriteString("; One section per converted expansion: SHA-256 of its AppxManifest.xml, tree digest, version.\r\n")
	b.WriteString("; Run the expansions installer again after the packages change.\r\n")
	for _, p := range pk {
		fmt.Fprintf(&b, "\r\n[%s]\r\nmanifest=%s\r\ntree=%s\r\nversion=%s\r\n", p.identity, p.manifestHash, p.treeDigest, p.version)
	}
	tmp := path + ".tmp-fh3dlc"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// applyMod is install mode's step for -mod: the packages are read and checked as for the patch, azimuth.exe is put
// back to the original if this tool had patched it, and fh3-expansions.ini is written.
func applyMod(dir, exe, bak string, dirs []string, gameExe string) error {
	pk := make([]pkg, len(packages))
	for i, k := range packages {
		p, err := readPackage(k, dirs[i])
		if err != nil {
			return err
		}
		pk[i] = p
	}
	if err := verifyGame(gameExe); err != nil {
		return err
	}
	raw, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	if sha(raw) != origSHA256 {
		b, err := os.ReadFile(bak)
		if !isOurs(raw) || err != nil || sha(b) != origSHA256 {
			return errors.New("this is not Azimuth 1.0.4, the version these expansions are prepared for")
		}
		if err := doRevert(exe, bak); err != nil {
			return err
		}
	}
	if _, err := os.Stat(filepath.Join(dir, "dinput8.dll")); err != nil {
		fmt.Println("note: no dinput8.dll (fh3-expansions) next to azimuth.exe yet; the expansions need it")
	}
	if err := writeModIni(filepath.Join(dir, modIni), pk); err != nil {
		return err
	}
	fmt.Printf("%s written for %d packages\n", modIni, len(pk))
	return nil
}

func installMode(dir, blizzard, hotwheels string, revert, check, mod bool) error {
	dir, err := filepath.Abs(dir)
	if err != nil {
		return err
	}
	exe := filepath.Join(dir, "azimuth.exe")
	bak := exe + ".bak-fh3dlc"
	ini := filepath.Join(dir, "data", "azimuth.ini")
	iniBak := ini + ".bak-fh3dlc"

	if _, err := os.Stat(exe); err != nil {
		return fmt.Errorf("no azimuth.exe in %s", dir)
	}
	// A running program cannot be opened for writing.
	f, err := os.OpenFile(exe, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("Azimuth is running from %s, or the folder is read-only. Close Azimuth and try again", dir)
	}
	f.Close()

	lines, err := readIni(ini)
	if errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("no %s. Start Azimuth once, add Forza Horizon 3, and try again", ini)
	} else if err != nil {
		return err
	}
	first, last, ok := section(lines)
	if !ok {
		return errors.New("Forza Horizon 3 is not added in this Azimuth. Add it in the launcher and try again")
	}
	contentAt, content := key(lines, first, last, "content")
	folderAt, folder := key(lines, first, last, "folder")

	if check {
		// Is this an azimuth.exe the patch can be applied to?
		raw, err := os.ReadFile(exe)
		if err != nil {
			return err
		}
		if sha(raw) != origSHA256 {
			b, err := os.ReadFile(bak)
			if !isOurs(raw) || err != nil || sha(b) != origSHA256 {
				return errors.New("this is not Azimuth 1.0.4, the version these expansions are prepared for")
			}
		}
		fmt.Println("ok")
		return nil
	}

	if revert {
		if err := doRevert(exe, bak); err != nil {
			return err
		}
		if err := os.Remove(filepath.Join(dir, modIni)); err == nil {
			fmt.Println(modIni + " removed")
		} else if !errors.Is(err, fs.ErrNotExist) {
			return err
		}
		// the fh3-expansions dinput8.dll; another mod's file of that name stays
		dll := filepath.Join(dir, "dinput8.dll")
		if b, err := os.ReadFile(dll); err == nil && strings.Contains(string(b), "fh3-expansions ") && strings.Contains(string(b), "(dinput8.dll)") {
			if err := os.Remove(dll); err != nil {
				return fmt.Errorf("%s cannot be removed (is Azimuth running?): %w", dll, err)
			}
			fmt.Println("dinput8.dll (fh3-expansions) removed")
		}
		// another mod's dinput8.dll that the expansions setup had moved aside goes back
		next := filepath.Join(dir, "dinput8.next.dll")
		if _, err := os.Stat(dll); errors.Is(err, fs.ErrNotExist) {
			if _, err := os.Stat(next); err == nil {
				if err := os.Rename(next, dll); err != nil {
					return fmt.Errorf("dinput8.next.dll cannot be put back as dinput8.dll: %w", err)
				}
				fmt.Println("the other mod's dinput8.dll is back in place")
			}
		}
		old, err := readIni(iniBak)
		if err != nil {
			return nil // no ini backup: this tool never changed the ini
		}
		oldValue := ""
		if f, l, ok := section(old); ok {
			_, oldValue = key(old, f, l, "content")
		}
		if contentAt >= 0 && content != oldValue {
			lines[contentAt] = "content=" + oldValue
			if err := writeIni(ini, lines); err != nil {
				return err
			}
		}
		fmt.Println("content= of Forza Horizon 3 restored")
		return nil
	}

	dirs := []string{blizzard, hotwheels}
	hashes := make([]string, len(dirs))
	for i, d := range dirs {
		if dirs[i], err = filepath.Abs(d); err != nil {
			return err
		}
		m, err := os.ReadFile(filepath.Join(dirs[i], "AppxManifest.xml"))
		if err != nil {
			return fmt.Errorf("no AppxManifest.xml in %s", dirs[i])
		}
		hashes[i] = sha(m)
	}
	gameExe := ""
	if folder != "" {
		gameExe = filepath.Join(folder, "Mount", "forza_durango_release_final.exe")
	}
	quiet = true
	if mod {
		if err := applyMod(dir, exe, bak, dirs, gameExe); err != nil {
			return err
		}
	} else if err := doApply(exe, bak, dirs, gameExe, false); err != nil {
		return err
	}

	// Keep entries that are not ours, then add ours.
	var entries []string
	for _, e := range strings.Split(content, "|") {
		e = strings.TrimSpace(e)
		if e == "" {
			continue
		}
		p, h, _ := strings.Cut(e, "*")
		ours := false
		for i := range dirs {
			if strings.EqualFold(h, hashes[i]) || strings.EqualFold(strings.TrimRight(p, `\`), dirs[i]) {
				ours = true
			}
		}
		if !ours {
			entries = append(entries, e)
		}
	}
	for i := range dirs {
		entries = append(entries, dirs[i]+"*"+hashes[i])
	}
	line := "content=" + strings.Join(entries, "|")
	if contentAt >= 0 && lines[contentAt] == line {
		fmt.Println("azimuth.ini: content= already set")
		return nil
	}
	if _, err := os.Stat(iniBak); errors.Is(err, fs.ErrNotExist) {
		b, err := os.ReadFile(ini)
		if err != nil {
			return err
		}
		if err := os.WriteFile(iniBak, b, 0o644); err != nil {
			return err
		}
	}
	if contentAt >= 0 {
		lines[contentAt] = line
	} else {
		at := first
		if folderAt >= 0 {
			at = folderAt + 1
		}
		lines = append(lines[:at], append([]string{line}, lines[at:]...)...)
	}
	if err := writeIni(ini, lines); err != nil {
		return err
	}
	fmt.Println("azimuth.ini: content= set for Forza Horizon 3")
	return nil
}
