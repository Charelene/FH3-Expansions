// azimuth-dlc gives Azimuth 1.0.4 a content-package list for Forza Horizon 3,
// so it accepts and mounts the Blizzard Mountain and Hot Wheels expansions.
//
// How Azimuth recognises a content package (azimuth.exe 1.0.4, SHA-256
// 56d2c9cb6ccf3ae1836dee9676b64285f3a247efa2d12cf02c6c742696b3b092, preferred
// base 0x140000000; all addresses below are VAs for that base):
//
//   - Five title records, pointer table at 0x1406b98e0: FH2 0x1406e19e0,
//     FH2 Fast & Furious 0x1406e4ba0, FM5 0x1406bf910, FM6 0x1406dde00,
//     FH3 0x1406c4220. Record +0x00 title id (UTF-16), +0x08 game version,
//     +0x10 short name, +0x7f8 pointer to the content-package list,
//     +0x800 its count. FH2 lists Storm Island (0x1406e1680, count 1);
//     FH3's list is empty (null, 0).
//   - A list entry is 0x20 bytes: +0x00 char* SHA-256 (lower hex) of the
//     package's AppxManifest.xml, +0x08 char* tree digest, +0x10 RVA in the
//     game image of the package's product id (UTF-16 GUID, read as 0x4a
//     bytes for the "offer ownership query" hook, 0x140040418), +0x18 char*
//     version string.
//   - The validator 0x1401a1ba0 hashes AppxManifest.xml, looks the hash up
//     in the record's list (0x1401a1fe9..0x1401a2062), and on a miss returns
//     reason 2 "Another version" (manifest declares a content package,
//     0x1401a1150) or 3 "Not a content package". On a hit it walks the
//     folder (0x140558fa0), computes the tree digest (0x1401a1260) and
//     compares it with entry+0x08: reason 1 "Incomplete or changed" on a
//     mismatch, 0 on a match. Reason texts: builder 0x1401a2e20.
//   - Tree digest = SHA-256 over the sorted lines
//     "<relative path, '/' separators, A-Z lowered>\t<decimal size>\n",
//     one per file. Checked against Storm Island's pinned value.
//
// The patch is data only. It appends a section ".fh3dlc" holding two list
// entries and their strings, points the FH3 record's +0x7f8/+0x800 at it,
// and appends base relocations for the seven new pointers (azimuth.exe is
// ASLR-relocated at run time; the game image takes 0x140000000).
package main

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
)

const (
	origSHA256 = "56d2c9cb6ccf3ae1836dee9676b64285f3a247efa2d12cf02c6c742696b3b092"
	gameSHA256 = "e6a362a2fde118577ab3018c8e042e62a3bb42f83a63ace41145c0864527a661"

	imageBase  = 0x140000000
	fh3Allowed = "ef520a86-ebbe-4a01-8384-7b0eef90ab50"

	// azimuth.exe 1.0.4 layout (verified before patching).
	peOff         = 0x120
	optOff        = peOff + 24
	secHdrOff     = 0x228 // optOff + SizeOfOptionalHeader (0xf0)
	origSections  = 6
	origSizeImage = 0x8a2000
	origFileSize  = 0x836e00
	relocHdrIndex = 5 // .reloc is section header 5
	relocVA       = 0x89d000
	relocRaw      = 0x832400
	relocRawSize  = 0x4a00
	origRelocSize = 0x4978
	fh3RecordRVA  = 0x6c4220
	fh3TitleIDRVA = 0x6c1460 // L"4ce1dfab"
	listPtrOff    = 0x7f8
	listCountOff  = 0x800
	newSecVA      = 0x8a2000
	newSecRaw     = origFileSize
	newSecRawSize = 0x200
	newSecName    = ".fh3dlc"
	newSecChars   = 0x40000040 // initialised data, readable
	relDir64      = 0xa
)

// The packages this tool knows. productRVA is the UTF-16 product id inside
// forza_durango_release_final.exe 1.0.125.0 (one occurrence each, the pooled
// literal behind the game's own offer queries, e.g. FUN_140329a10).
type known struct {
	identity   string
	productID  string
	productRVA uint32
	defaultDir string
}

var packages = []known{
	{"Microsoft.ForzaHorizon3BlizzardMountain", "6f5caf90-c2bd-43b6-bfff-6b03212ba77a", 0x2d30d30, ""},
	{"Microsoft.ForzaHorizon3HotWheels", "c87ee9a8-fe89-4211-9a60-d974b4454afe", 0x2d30d80, ""},
}

type pkg struct {
	known
	dir          string
	version      string
	displayName  string
	manifestHash string
	treeDigest   string
	files        int
}

func main() {
	revert := flag.Bool("revert", false, "restore azimuth.exe from the backup this tool wrote")
	check := flag.Bool("check", false, "report the state of azimuth.exe and the packages, change nothing")
	blizzard := flag.String("blizzard", packages[0].defaultDir, "Blizzard Mountain package folder")
	hotwheels := flag.String("hotwheels", packages[1].defaultDir, "Hot Wheels package folder")
	gameExe := flag.String("game-exe", "", "FH3 exe used to verify the product id addresses (skipped if not given)")
	azimuth := flag.String("azimuth", "", "Azimuth folder: patch its azimuth.exe and set content= in its data\\azimuth.ini (with -revert: undo both)")
	mod := flag.Bool("mod", false, "with -azimuth: leave azimuth.exe as it is (restore it if this tool patched it) and write fh3-expansions.ini for the fh3-expansions dinput8.dll instead")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: azimuth-dlc [flags] <path to azimuth.exe>\n       azimuth-dlc -azimuth <folder> -blizzard <folder> -hotwheels <folder>\n       azimuth-dlc -azimuth <folder> -revert\n")
		flag.PrintDefaults()
	}
	flag.Parse()
	if *azimuth != "" {
		if err := installMode(*azimuth, *blizzard, *hotwheels, *revert, *check, *mod); err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
			os.Exit(1)
		}
		return
	}
	if flag.NArg() != 1 {
		flag.Usage()
		os.Exit(2)
	}
	exe := flag.Arg(0)
	bak := exe + ".bak-fh3dlc"

	var err error
	switch {
	case *revert:
		err = doRevert(exe, bak)
	default:
		err = doApply(exe, bak, []string{*blizzard, *hotwheels}, *gameExe, *check)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}

func sha(b []byte) string { s := sha256.Sum256(b); return hex.EncodeToString(s[:]) }

func doRevert(exe, bak string) error {
	cur, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	if sha(cur) == origSHA256 {
		fmt.Println("azimuth.exe is already the original; nothing to do")
		return nil
	}
	b, err := os.ReadFile(bak)
	if err != nil {
		return fmt.Errorf("no backup at %s: %w", bak, err)
	}
	if sha(b) != origSHA256 {
		return fmt.Errorf("backup %s is not the original azimuth.exe 1.0.4 (sha256 %s)", bak, sha(b))
	}
	if _, err := patchImage(b, nil, true); err != nil {
		return err
	}
	if err := writeAtomic(exe, b); err != nil {
		return err
	}
	fmt.Printf("restored %s from %s (sha256 %s)\n", exe, bak, origSHA256)
	return nil
}

func doApply(exe, bak string, dirs []string, gameExe string, checkOnly bool) error {
	raw, err := os.ReadFile(exe)
	if err != nil {
		return err
	}
	pk := make([]pkg, len(packages))
	for i, k := range packages {
		p, err := readPackage(k, dirs[i])
		if err != nil {
			return err
		}
		pk[i] = p
		fmt.Printf("%s %s\n  folder   %s\n  manifest %s\n  tree     %s (%d files)\n  product  %s at game RVA 0x%x\n",
			p.identity, p.version, p.dir, p.manifestHash, p.treeDigest, p.files, p.productID, p.productRVA)
	}
	if err := verifyGame(gameExe); err != nil {
		return err
	}

	state := sha(raw)
	switch {
	case state == origSHA256:
		fmt.Println("azimuth.exe: original 1.0.4")
	default:
		// Patched by us? Rebuild from the backup and compare.
		if b, err := os.ReadFile(bak); err == nil && sha(b) == origSHA256 {
			wasQuiet := quiet
			quiet = true
			want, err := patchImage(append([]byte(nil), b...), pk, false)
			quiet = wasQuiet
			if err != nil {
				return err
			}
			if bytes.Equal(want, raw) {
				fmt.Printf("azimuth.exe: already patched for these packages (sha256 %s)\n", state)
				return nil
			}
			if isOurs(raw) {
				fmt.Println("azimuth.exe: patched for other package contents; re-patching from the backup")
				raw = b
				state = origSHA256
				break
			}
		}
		return fmt.Errorf("azimuth.exe sha256 %s is not 1.0.4 (%s) and not a patch from this tool", state, origSHA256)
	}
	if checkOnly {
		return nil
	}

	out, err := patchImage(append([]byte(nil), raw...), pk, false)
	if err != nil {
		return err
	}
	if _, err := os.Stat(bak); errors.Is(err, fs.ErrNotExist) {
		if err := os.WriteFile(bak, raw, 0o644); err != nil {
			return fmt.Errorf("writing backup: %w", err)
		}
		fmt.Printf("backup   %s\n", bak)
	} else if err == nil {
		b, _ := os.ReadFile(bak)
		if sha(b) != origSHA256 {
			return fmt.Errorf("%s exists and is not the original; move it away first", bak)
		}
	}
	if err := writeAtomic(exe, out); err != nil {
		return err
	}
	fmt.Printf("patched  %s (sha256 %s)\n", exe, sha(out))
	return nil
}

func isOurs(b []byte) bool {
	if len(b) < secHdrOff+40*(origSections+1) {
		return false
	}
	h := b[secHdrOff+40*origSections:]
	return bytes.Equal(bytes.TrimRight(h[:8], "\x00"), []byte(newSecName))
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".tmp-fh3dlc"
	if err := os.WriteFile(tmp, b, 0o755); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// ---- package reading -------------------------------------------------------

type manifest struct {
	Identity struct {
		Name    string `xml:"Name,attr"`
		Version string `xml:"Version,attr"`
	} `xml:"Identity"`
	Properties struct {
		DisplayName    string `xml:"DisplayName"`
		ContentPackage string `xml:"ContentPackage"`
	} `xml:"Properties"`
	Extensions struct {
		PackageExtension []struct {
			Category       string `xml:"Category,attr"`
			ContentPackage struct {
				AllowedProduct []struct {
					ID string `xml:"Id,attr"`
				} `xml:"AllowedProduct"`
			} `xml:"ContentPackage"`
		} `xml:"PackageExtension"`
	} `xml:"Extensions"`
	Dependencies struct {
		MainPackageDependency []struct {
			Name string `xml:"Name,attr"`
		} `xml:"MainPackageDependency"`
	} `xml:"Dependencies"`
}

const fh3MainPackage = "Microsoft.OpusPG" // Forza Horizon 3 for Windows

// forFH3 says whether a manifest ties its package to Forza Horizon 3, in either
// form the expansions were published in: an Xbox content package
// (mx:ContentPackage with an xbox.contentpackage extension that allows FH3's
// product), or a Windows optional package whose MainPackageDependency is the
// Windows game (the Store builds, e.g. Hot Wheels 1.0.63.2, Blizzard Mountain
// 1.0.4.2). Azimuth itself checks neither; this only guards against a wrong
// folder.
func forFH3(m manifest) bool {
	for _, d := range m.Dependencies.MainPackageDependency {
		if d.Name == fh3MainPackage {
			return true
		}
	}
	if strings.TrimSpace(m.Properties.ContentPackage) != "true" {
		return false
	}
	for _, e := range m.Extensions.PackageExtension {
		if e.Category != "xbox.contentpackage" {
			continue
		}
		for _, a := range e.ContentPackage.AllowedProduct {
			if strings.EqualFold(a.ID, fh3Allowed) {
				return true
			}
		}
	}
	return false
}

func readPackage(k known, dir string) (pkg, error) {
	p := pkg{known: k, dir: dir}
	mb, err := os.ReadFile(filepath.Join(dir, "AppxManifest.xml"))
	if err != nil {
		return p, err
	}
	if len(mb) > 0x10000 {
		return p, fmt.Errorf("%s: AppxManifest.xml is larger than Azimuth reads (64 KiB)", dir)
	}
	var m manifest
	if err := xml.Unmarshal(mb, &m); err != nil {
		return p, fmt.Errorf("%s: AppxManifest.xml: %w", dir, err)
	}
	if m.Identity.Name != k.identity {
		return p, fmt.Errorf("%s: Identity Name is %q, want %q", dir, m.Identity.Name, k.identity)
	}
	if !forFH3(m) {
		return p, fmt.Errorf("%s: the manifest does not tie this package to Forza Horizon 3 (no MainPackageDependency %s, no xbox.contentpackage for product %s)", dir, fh3MainPackage, fh3Allowed)
	}
	// The same checks the validator makes after a list hit (0x1401a2396..0x1401a241b).
	v := m.Identity.Version
	if len(v) < 1 || len(v) > 23 || strings.Trim(v, "0123456789.") != "" {
		return p, fmt.Errorf("%s: Version %q is not what Azimuth accepts", dir, v)
	}
	dn := m.Properties.DisplayName
	if len(dn) < 1 || len(dn) > 64 || strings.ContainsAny(dn, "<") || strings.IndexFunc(dn, func(r rune) bool { return r < 0x20 }) >= 0 {
		return p, fmt.Errorf("%s: DisplayName %q is not what Azimuth accepts", dir, dn)
	}
	p.version, p.displayName = v, dn
	p.manifestHash = sha(mb)
	p.treeDigest, p.files, err = treeDigest(dir)
	return p, err
}

// treeDigest reproduces 0x1401a1260 over the listing of 0x140558fa0.
func treeDigest(root string) (string, int, error) {
	var lines []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&fs.ModeSymlink != 0 || info.Mode()&fs.ModeIrregular != 0 {
			return fmt.Errorf("%s is a link; Azimuth refuses packages with links", path)
		}
		if d.IsDir() {
			return nil
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		b := []byte(filepath.ToSlash(rel))
		for i, c := range b {
			if c >= 'A' && c <= 'Z' {
				b[i] = c + 32
			}
		}
		lines = append(lines, string(b)+"\t"+strconv.FormatInt(info.Size(), 10)+"\n")
		return nil
	})
	if err != nil {
		return "", 0, err
	}
	sort.Strings(lines)
	return sha([]byte(strings.Join(lines, ""))), len(lines), nil
}

// verifyGame checks that each product id RVA holds the expected UTF-16 GUID.
func verifyGame(path string) error {
	if path == "" {
		fmt.Println("no game exe given; product id addresses not checked")
		return nil
	}
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		fmt.Printf("game exe %s not found; product id addresses not checked\n", path)
		return nil
	} else if err != nil {
		return err
	}
	if sha(b) != gameSHA256 {
		return fmt.Errorf("%s is not FH3 1.0.125.0 (sha256 %s); the product id addresses are for that build", path, sha(b))
	}
	for _, k := range packages {
		o, ok := rvaToOff(b, k.productRVA)
		if !ok || o+0x4a > len(b) {
			return fmt.Errorf("game RVA 0x%x is outside the file", k.productRVA)
		}
		u := make([]uint16, 0x24)
		for i := range u {
			u[i] = binary.LittleEndian.Uint16(b[o+2*i:])
		}
		got := string(utf16.Decode(u))
		if got != k.productID || binary.LittleEndian.Uint16(b[o+0x48:]) != 0 {
			return fmt.Errorf("game RVA 0x%x holds %q, want %q", k.productRVA, got, k.productID)
		}
	}
	fmt.Println("game exe: FH3 1.0.125.0, product ids found at their addresses")
	return nil
}

func rvaToOff(b []byte, rva uint32) (int, bool) {
	pe := int(binary.LittleEndian.Uint32(b[0x3c:]))
	n := int(binary.LittleEndian.Uint16(b[pe+6:]))
	sh := pe + 24 + int(binary.LittleEndian.Uint16(b[pe+20:]))
	for i := 0; i < n; i++ {
		h := b[sh+40*i:]
		vs, va := binary.LittleEndian.Uint32(h[8:]), binary.LittleEndian.Uint32(h[12:])
		rs, ro := binary.LittleEndian.Uint32(h[16:]), binary.LittleEndian.Uint32(h[20:])
		if rva >= va && rva < va+vs && rva-va < rs {
			return int(ro + rva - va), true
		}
	}
	return 0, false
}

// ---- the patch ---------------------------------------------------------------

type edit struct {
	off  int
	orig []byte
	new  []byte
	what string
}

func le32(v uint32) []byte { b := make([]byte, 4); binary.LittleEndian.PutUint32(b, v); return b }
func le64(v uint64) []byte { b := make([]byte, 8); binary.LittleEndian.PutUint64(b, v); return b }
func le16(v uint16) []byte { b := make([]byte, 2); binary.LittleEndian.PutUint16(b, v); return b }

// patchImage verifies every original byte it touches, then (unless
// verifyOnly) returns the patched image. raw must be the original exe.
var quiet bool

func patchImage(raw []byte, pk []pkg, verifyOnly bool) ([]byte, error) {
	if len(raw) != origFileSize {
		return nil, fmt.Errorf("file size 0x%x, want 0x%x", len(raw), origFileSize)
	}
	// The FH3 record really is FH3's.
	ro := func(rva uint32) int { o, _ := rvaToOff(raw, rva); return o }
	if binary.LittleEndian.Uint64(raw[ro(fh3RecordRVA):]) != imageBase+fh3TitleIDRVA {
		return nil, errors.New("FH3 record does not start with the 4ce1dfab title id pointer")
	}
	tid := raw[ro(fh3TitleIDRVA) : ro(fh3TitleIDRVA)+16]
	if !bytes.Equal(tid, []byte("4\x00c\x00e\x001\x00d\x00f\x00a\x00b\x00")) {
		return nil, errors.New("title id string is not 4ce1dfab")
	}
	if verifyOnly {
		return raw, nil
	}

	// New section contents.
	sec := make([]byte, newSecRawSize)
	str := 0x40
	put := func(s string) uint64 {
		va := imageBase + uint64(newSecVA+str)
		copy(sec[str:], s)
		str += (len(s) + 1 + 15) &^ 15
		return va
	}
	var relocOffs []uint16
	for i, p := range pk {
		e := i * 0x20
		binary.LittleEndian.PutUint64(sec[e+0x00:], put(p.manifestHash))
		binary.LittleEndian.PutUint64(sec[e+0x08:], put(p.treeDigest))
		binary.LittleEndian.PutUint64(sec[e+0x10:], uint64(p.productRVA))
		binary.LittleEndian.PutUint64(sec[e+0x18:], put(p.version))
		relocOffs = append(relocOffs, uint16(e+0x00), uint16(e+0x08), uint16(e+0x18))
	}
	if str > newSecRawSize {
		return nil, errors.New("section data does not fit")
	}
	secVSize := uint32(str)

	// Relocations. Windows applies base relocations page by page and expects the
	// blocks in ascending page order; a block appended out of order made the
	// image load at its preferred base every time (ASLR off), which pushed the
	// game image away from 0x140000000. So the table is rebuilt sorted: the
	// record's list pointer joins the existing block of its page, and the new
	// section gets its own block at the end (its page is above all others).
	pages := map[uint32][]uint16{}
	var order []uint32
	for p := 0; p < origRelocSize; {
		page := le32get(raw[relocRaw+p:])
		size := int(le32get(raw[relocRaw+p+4:]))
		if size < 8 || p+size > origRelocSize {
			return nil, errors.New("malformed relocation table")
		}
		if _, ok := pages[page]; !ok {
			order = append(order, page)
		}
		for k := 8; k < size; k += 2 {
			e := uint16(raw[relocRaw+p+k]) | uint16(raw[relocRaw+p+k+1])<<8
			if e>>12 != 0 { // drop padding entries, re-added below
				pages[page] = append(pages[page], e)
			}
		}
		p += size
	}
	recPage := uint32(fh3RecordRVA+listPtrOff) &^ 0xfff
	if _, ok := pages[recPage]; !ok {
		order = append(order, recPage)
	}
	pages[recPage] = append(pages[recPage], relDir64<<12|uint16((fh3RecordRVA+listPtrOff)&0xfff))
	order = append(order, newSecVA)
	for _, o := range relocOffs {
		pages[newSecVA] = append(pages[newSecVA], relDir64<<12|o)
	}
	sort.Slice(order, func(i, j int) bool { return order[i] < order[j] })
	var rel []byte
	for _, page := range order {
		es := pages[page]
		sort.Slice(es, func(i, j int) bool { return es[i]&0xfff < es[j]&0xfff })
		n := len(es)
		if n%2 == 1 {
			n++
		}
		rel = append(rel, le32(page)...)
		rel = append(rel, le32(uint32(8+2*n))...)
		for _, e := range es {
			rel = append(rel, le16(e)...)
		}
		if len(es)%2 == 1 {
			rel = append(rel, le16(0)...)
		}
	}
	newRelocSize := uint32(len(rel))
	if newRelocSize > relocRawSize {
		return nil, errors.New("relocations do not fit in .reloc")
	}
	for _, b := range raw[relocRaw+origRelocSize : relocRaw+int(newRelocSize)] {
		if b != 0 {
			return nil, errors.New(".reloc slack is not empty")
		}
	}

	hdr := make([]byte, 40)
	copy(hdr, newSecName)
	copy(hdr[8:], le32(secVSize))
	copy(hdr[12:], le32(newSecVA))
	copy(hdr[16:], le32(newSecRawSize))
	copy(hdr[20:], le32(newSecRaw))
	copy(hdr[36:], le32(newSecChars))

	listOff := ro(fh3RecordRVA + listPtrOff)
	relocHdr := secHdrOff + 40*relocHdrIndex
	edits := []edit{
		{peOff + 6, le16(origSections), le16(origSections + 1), "FileHeader.NumberOfSections"},
		{optOff + 56, le32(origSizeImage), le32(newSecVA + 0x1000), "OptionalHeader.SizeOfImage"},
		{optOff + 112 + 8*5 + 4, le32(origRelocSize), le32(newRelocSize), "DataDirectory[BASERELOC].Size"},
		{relocHdr + 8, le32(origRelocSize), le32(newRelocSize), ".reloc VirtualSize"},
		{secHdrOff + 40*origSections, make([]byte, 40), hdr, "section header " + newSecName},
		{relocRaw, append([]byte(nil), raw[relocRaw:relocRaw+len(rel)]...), rel, ".reloc: table rebuilt in page order, 7 entries added"},
		{listOff, make([]byte, 16), append(le64(imageBase+newSecVA), le64(uint64(len(pk)))...), "FH3 record +0x7f8 list pointer, +0x800 count"},
	}
	// Sanity on the .reloc header and directory before trusting the offsets.
	if binary.LittleEndian.Uint32(raw[relocHdr+12:]) != relocVA || binary.LittleEndian.Uint32(raw[relocHdr+20:]) != relocRaw ||
		binary.LittleEndian.Uint32(raw[optOff+112+8*5:]) != relocVA {
		return nil, errors.New(".reloc section or directory is not where 1.0.4 has it")
	}
	for _, e := range edits {
		if !bytes.Equal(raw[e.off:e.off+len(e.orig)], e.orig) {
			return nil, fmt.Errorf("%s at file offset 0x%x: found %x, want %x", e.what, e.off, raw[e.off:e.off+len(e.orig)], e.orig)
		}
	}
	for _, e := range edits {
		copy(raw[e.off:], e.new)
		if !quiet {
			fmt.Printf("  file 0x%06x  %-44s %x -> %x\n", e.off, e.what, e.orig, e.new)
		}
	}
	raw = append(raw, sec...)
	if !quiet {
		fmt.Printf("  file 0x%06x  %-44s 0x%x bytes at VA 0x%x\n", newSecRaw, "appended section data", len(sec), imageBase+newSecVA)
	}
	return raw, nil
}

func le32get(b []byte) uint32 { return binary.LittleEndian.Uint32(b) }
