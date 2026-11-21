package main

// "uwp2xbox setup": converts the PC (UWP) copies of the two Forza Horizon 3
// expansions, which the user owns, into the Xbox One layout that Azimuth
// loads, and registers them with an Azimuth install. No game file and no
// Microsoft file travels with the tool: the textures are tiled by tile1d.go,
// the console shaders come from the Xbox base game that Azimuth runs.

import (
	"bytes"
	"encoding/xml"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"unicode/utf16"
	"unsafe"
)

// isOurMod says whether the file at path is the fh3-expansions dinput8.dll
// (tools/azimuth-dlc/mod; its log banner holds both strings).
func isOurMod(path string) bool {
	b, err := os.ReadFile(path)
	return err == nil && bytes.Contains(b, []byte("fh3-expansions ")) && bytes.Contains(b, []byte("(dinput8.dll)"))
}

func fileExists(p string) bool { _, err := os.Stat(p); return err == nil }

const baseGameName = "Microsoft.OpusPG" // Forza Horizon 3 for Windows

// findDumpPackages looks under root (up to four folders deep) for the Windows
// base game and the two expansions, by the identity in each AppxManifest.xml;
// what is not found there is then looked for on the rest of root's drive (from
// the drive's root, also four deep). Packages are searched inside too, since
// the expansions are often kept in the base game's folder; a copy of a package
// inside another copy of the same package counts once (the outer one). Copies
// converted by this tool (short display name, or a folder holding the
// conversion marker) and anything under skip are left out. Two separate copies
// of one package are an error: the user has to pick a folder that holds one.
func findDumpPackages(root, skip string) (base, hotwheels, blizzard string, err error) {
	found := map[string][]string{}
	want := map[string]bool{baseGameName: true, hotWheelsName: true, blizzardName: true}
	var walk func(dir string, depth int)
	walk = func(dir string, depth int) {
		if skip != "" && strings.EqualFold(filepath.Clean(dir), filepath.Clean(skip)) {
			return
		}
		if fileExists(filepath.Join(dir, markerName)) {
			return
		}
		if fileExists(filepath.Join(dir, "AppxManifest.xml")) {
			id, e := packageIdentity(dir)
			if e != nil {
				return
			}
			if short, ok := expansionNames[id]; ok && displayName(dir) == short {
				return // converted by this tool
			}
			if want[id] {
				found[id] = addOuter(found[id], dir)
			}
		}
		if depth == 4 {
			return
		}
		ents, e := os.ReadDir(dir)
		if e != nil {
			return
		}
		for _, en := range ents {
			if !en.IsDir() || en.Type()&fs.ModeSymlink != 0 {
				continue
			}
			n := en.Name()
			if strings.HasPrefix(n, "$") || strings.EqualFold(n, "System Volume Information") || strings.EqualFold(n, "Windows") {
				continue
			}
			walk(filepath.Join(dir, n), depth+1)
		}
	}
	walk(root, 0)
	drive := filepath.VolumeName(root) + `\`
	searchedDrive := false
	if !strings.EqualFold(filepath.Clean(root), filepath.Clean(drive)) {
		missing := map[string]bool{}
		for id := range want {
			if len(found[id]) == 0 {
				missing[id] = true
			}
		}
		if len(missing) > 0 {
			want = missing
			walk(drive, 0)
			searchedDrive = true
		}
	}
	pick := func(id, what string) (string, error) {
		switch c := found[id]; len(c) {
		case 0:
			if searchedDrive {
				return "", fmt.Errorf("%s is not in %s, nor anywhere else on %s (up to four folders deep). Look for a folder that has the expansion's AppxManifest.xml", what, root, drive)
			}
			return "", fmt.Errorf("%s is not in %s (up to four folders deep). Look for a folder that has its AppxManifest.xml", what, root)
		case 1:
			return c[0], nil
		default:
			return "", fmt.Errorf("%s is there more than once: %s. Choose a folder that holds one copy", what, strings.Join(c, ", "))
		}
	}
	if base, err = pick(baseGameName, "Forza Horizon 3 for Windows"); err != nil {
		return
	}
	if hotwheels, err = pick(hotWheelsName, "Hot Wheels"); err != nil {
		return
	}
	blizzard, err = pick(blizzardName, "Blizzard Mountain")
	return
}

// addOuter adds dir to a list of package folders unless it is one of them or
// lies inside one; folders inside dir are dropped, so nested copies of a
// package count once, by their outermost folder.
func addOuter(list []string, dir string) []string {
	inside := func(child, parent string) bool {
		rel, err := filepath.Rel(parent, child)
		return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
	}
	var out []string
	for _, d := range list {
		if inside(dir, d) {
			return list // dir is d or lies inside it
		}
		if !inside(d, dir) {
			out = append(out, d)
		}
	}
	return append(out, dir)
}

// displayName is the Properties DisplayName of a package's manifest.
func displayName(dir string) string {
	b, err := os.ReadFile(filepath.Join(dir, "AppxManifest.xml"))
	if err != nil {
		return ""
	}
	var m struct {
		Properties struct {
			DisplayName string `xml:"DisplayName"`
		} `xml:"Properties"`
	}
	xml.Unmarshal(b, &m)
	return m.Properties.DisplayName
}

// Names Azimuth shows for the converted packages (the user's choice, 2026-11-21).
var expansionNames = map[string]string{
	hotWheelsName: "HOT WHEELS",
	blizzardName:  "BLIZZARD MOUNTAIN",
}

// setExpansionName gives a converted package the short name Azimuth shows: the
// DisplayName of its Properties and of its ContentPackageVisualElements. The
// manifest is written as a new file, so a hard link to the user's original copy
// is replaced, never written through.
func setExpansionName(dir string) error {
	id, err := packageIdentity(dir)
	if err != nil {
		return err
	}
	name, ok := expansionNames[id]
	if !ok {
		return nil
	}
	p := filepath.Join(dir, "AppxManifest.xml")
	b, err := os.ReadFile(p)
	if err != nil {
		return err
	}
	var m struct {
		Properties struct {
			DisplayName string `xml:"DisplayName"`
		} `xml:"Properties"`
	}
	if err := xml.Unmarshal(b, &m); err != nil {
		return err
	}
	old := m.Properties.DisplayName
	if old == "" || old == name {
		return nil
	}
	s := string(b)
	s = strings.Replace(s, "<DisplayName>"+old+"</DisplayName>", "<DisplayName>"+name+"</DisplayName>", 1)
	s = strings.Replace(s, `DisplayName="`+old+`"`, `DisplayName="`+name+`"`, 1)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, []byte(s), 0o644); err != nil {
		return err
	}
	if err := os.Remove(p); err != nil {
		os.Remove(tmp)
		return err
	}
	return os.Rename(tmp, p)
}

func cmdSetNames(args []string) error {
	for _, d := range args {
		if err := setExpansionName(d); err != nil {
			return fmt.Errorf("%s: %w", d, err)
		}
		fmt.Println("named", d)
	}
	return nil
}

func init() { devCommands["set-names"] = cmdSetNames }

// placeMod copies the fh3-expansions dinput8.dll from srcDir into the Azimuth
// folder. Another mod's dinput8.dll is kept: it is renamed to dinput8.next.dll,
// which the expansions mod loads and passes DirectInput on to (azimuth-dlc
// -revert puts it back).
func placeMod(srcDir, azimuth string) error {
	src := filepath.Join(srcDir, "dinput8.dll")
	if !isOurMod(src) {
		return fmt.Errorf("the expansions mod (dinput8.dll) is missing next to %s", filepath.Join(srcDir, "azimuth-dlc.exe"))
	}
	dst := filepath.Join(azimuth, "dinput8.dll")
	if fileExists(dst) && !isOurMod(dst) {
		next := filepath.Join(azimuth, "dinput8.next.dll")
		if fileExists(next) {
			return fmt.Errorf("%s belongs to another mod and dinput8.next.dll is taken too; move one of them away and run this again", dst)
		}
		if err := os.Rename(dst, next); err != nil {
			return fmt.Errorf("another mod's dinput8.dll cannot be moved aside (is Azimuth running?): %w", err)
		}
		fmt.Println("another mod's dinput8.dll is now dinput8.next.dll; the expansions mod passes DirectInput on to it")
	}
	b, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	tmp := dst + ".tmp-fh3exp"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, dst)
}

// setupPause: the window this runs in closes with the program, so wait for Enter.
var setupPause bool

type setupInput struct {
	azimuth, xbox, media     string // Azimuth folder, FH3 Xbox folder, its media folder
	uwpBase                  string
	hotwheels, blizzard, out string
	islandZip                string // the UWP base's tracks\island\bin.zip
	islandDecode             bool
}

const (
	hotWheelsName = "Microsoft.ForzaHorizon3HotWheels"
	blizzardName  = "Microsoft.ForzaHorizon3BlizzardMountain"
	markerName    = "fh3-expansions-conversion.txt"
)

func packageIdentity(dir string) (string, error) {
	b, err := os.ReadFile(filepath.Join(dir, "AppxManifest.xml"))
	if err != nil {
		return "", errors.New("no AppxManifest.xml in " + dir)
	}
	var m struct {
		Identity struct {
			Name string `xml:"Name,attr"`
		} `xml:"Identity"`
	}
	if err := xml.Unmarshal(b, &m); err != nil {
		return "", fmt.Errorf("%s: %w", filepath.Join(dir, "AppxManifest.xml"), err)
	}
	return m.Identity.Name, nil
}

// fh3Folder returns the folder= of the Forza Horizon 3 title in azimuth.ini.
func fh3Folder(ini string) (string, error) {
	b, err := os.ReadFile(ini)
	if errors.Is(err, fs.ErrNotExist) {
		return "", fmt.Errorf("no %s. Start Azimuth once, add Forza Horizon 3, and try again", ini)
	} else if err != nil {
		return "", err
	}
	if len(b) < 2 || b[0] != 0xff || b[1] != 0xfe {
		return "", fmt.Errorf("%s is not UTF-16 text", ini)
	}
	u := make([]uint16, (len(b)-2)/2)
	for i := range u {
		u[i] = uint16(b[2+2*i]) | uint16(b[3+2*i])<<8
	}
	in := false
	for _, l := range strings.Split(strings.ReplaceAll(string(utf16.Decode(u)), "\r\n", "\n"), "\n") {
		t := strings.TrimSpace(l)
		if strings.HasPrefix(t, "[") {
			in = strings.EqualFold(t, "[title-4ce1dfab]")
			continue
		}
		if k, v, ok := strings.Cut(t, "="); ok && in && strings.EqualFold(strings.TrimSpace(k), "folder") {
			return strings.TrimSpace(v), nil
		}
	}
	return "", errors.New("Forza Horizon 3 is not added in this Azimuth. Add it in the launcher and try again")
}

// findIslandZip looks for the base game's tracks\island\bin.zip, which the Hot
// Wheels package does not carry. Only a developer build with a name table
// (names_dev.go) also recognises it under encoded names; decode then says the
// archive's entry names need the same decoding.
func findIslandZip(base string) (path string, decode bool) {
	filepath.WalkDir(base, func(p string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || path != "" {
			return nil
		}
		if nameTable == nil && !strings.EqualFold(d.Name(), "bin.zip") {
			return nil
		}
		rel, _ := filepath.Rel(base, p)
		rel = strings.ToLower(filepath.ToSlash(rel))
		if strings.HasSuffix(rel, "tracks/island/bin.zip") {
			path = p
		} else if nameTable != nil {
			if dec := realName(rel); !strings.Contains(dec, "?") && strings.HasSuffix(dec, "tracks/island/bin.zip") {
				path, decode = p, true
			}
		}
		return nil
	})
	return
}

func freeBytes(dir string) (uint64, error) {
	var free, total, avail uint64
	p, err := syscall.UTF16PtrFromString(dir)
	if err != nil {
		return 0, err
	}
	r, _, e := syscall.NewLazyDLL("kernel32.dll").NewProc("GetDiskFreeSpaceExW").Call(
		uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&avail)), uintptr(unsafe.Pointer(&total)), uintptr(unsafe.Pointer(&free)))
	if r == 0 {
		return 0, e
	}
	return avail, nil
}

func dirSize(root string) uint64 {
	var n uint64
	filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			if i, e := d.Info(); e == nil {
				n += uint64(i.Size())
			}
		}
		return nil
	})
	return n
}

func abs(p string) string {
	if p == "" {
		return ""
	}
	a, err := filepath.Abs(p)
	if err != nil {
		return p
	}
	return filepath.Clean(a)
}

func checkSetup(in *setupInput) error {
	if _, err := os.Stat(filepath.Join(in.azimuth, "azimuth.exe")); err != nil {
		return fmt.Errorf("no azimuth.exe in %s", in.azimuth)
	}
	if in.xbox == "" {
		f, err := fh3Folder(filepath.Join(in.azimuth, "data", "azimuth.ini"))
		if err != nil {
			return err
		}
		in.xbox = f
	}
	in.media = filepath.Join(in.xbox, "Mount", "media")
	for _, f := range []string{`_library\Shaders.zip`, `Tracks\homespace02\bin.zip`, `Tracks\australia\bin.zip`} {
		if _, err := os.Stat(filepath.Join(in.media, f)); err != nil {
			return fmt.Errorf("the Xbox game folder %s lacks media\\%s", in.xbox, f)
		}
	}
	for _, c := range []struct{ dir, want, what string }{{in.hotwheels, hotWheelsName, "Hot Wheels"}, {in.blizzard, blizzardName, "Blizzard Mountain"}} {
		if c.dir == "" {
			return fmt.Errorf("no %s folder given", c.what)
		}
		id, err := packageIdentity(c.dir)
		if err != nil {
			return fmt.Errorf("%s: %w", c.what, err)
		}
		if id != c.want {
			return fmt.Errorf("%s is the package %q, not the %s expansion", c.dir, id, c.what)
		}
		if _, err := os.Stat(filepath.Join(c.dir, "media")); err != nil {
			return fmt.Errorf("%s has no media folder", c.dir)
		}
	}
	if _, err := os.Stat(filepath.Join(in.hotwheels, "media", "Tracks", "island", "bin.zip")); err == nil {
		// the package carries its own shader archive
		in.islandZip, in.islandDecode = filepath.Join(in.hotwheels, "media", "Tracks", "island", "bin.zip"), false
	} else {
		if in.uwpBase == "" {
			return errors.New("no folder of the PC base game given (it holds the Hot Wheels island's shaders)")
		}
		in.islandZip, in.islandDecode = findIslandZip(in.uwpBase)
		if in.islandZip == "" && nameTable == nil {
			// the base game's file names may be encoded: learn them from the Xbox game's
			if learnBaseNames(in.uwpBase, in.media) {
				in.islandZip, in.islandDecode = findIslandZip(in.uwpBase)
			}
		}
		if in.islandZip == "" {
			return fmt.Errorf("the island shaders (tracks\\island\\bin.zip) are not in %s", in.uwpBase)
		}
	}
	return nil
}

// learnBaseNames looks for a folder of the Windows base game whose file names
// are encoded by a fixed character substitution, learns the substitution by
// comparing it with the Xbox game's media folder (solveNames) and, if the result
// is convincing, installs it as nameTable. A folder counts when at least half of
// its files decode to a path that exists in the Xbox media.
func learnBaseNames(base, xboxMedia string) bool {
	ents, err := os.ReadDir(base)
	if err != nil {
		return false
	}
	var best *nameSolution
	bestDir := ""
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		sol, err := solveNames(filepath.Join(base, e.Name()), xboxMedia, 4096, io.Discard)
		if err != nil || sol.files == 0 || sol.found*2 < sol.files {
			continue
		}
		if best == nil || sol.found > best.found {
			best, bestDir = sol, e.Name()
		}
	}
	if best == nil {
		return false
	}
	t := map[rune]rune{'/': '/'}
	for k, v := range best.table {
		t[k] = v
	}
	nameTable = t
	fmt.Printf("base game file names in %s are encoded: learned %d characters from the Xbox game's file list (%d of %d files found)\n",
		bestDir, len(best.table), best.found, best.files)
	return true
}

func cmdSetup(args []string) error {
	set := flag.NewFlagSet("setup", flag.ContinueOnError)
	var in setupInput
	check := set.Bool("check", false, "only check the inputs")
	set.BoolVar(&setupPause, "pause", false, "wait for Enter before closing")
	set.StringVar(&in.azimuth, "azimuth", "", "Azimuth folder (azimuth.exe, data\\azimuth.ini)")
	set.StringVar(&in.xbox, "xbox", "", "Xbox Forza Horizon 3 folder (default: the one in azimuth.ini)")
	set.StringVar(&in.uwpBase, "uwp", "", "folder of the PC (UWP) base game")
	set.StringVar(&in.hotwheels, "hotwheels", "", "PC Hot Wheels expansion folder")
	set.StringVar(&in.blizzard, "blizzard", "", "PC Blizzard Mountain expansion folder")
	set.StringVar(&in.out, "out", "", "folder for the converted expansions")
	dump := set.String("dump", "", "folder with the Windows base game and both expansions somewhere inside (fills -uwp, -hotwheels, -blizzard when not given)")
	tool := set.String("tool", "", "azimuth-dlc.exe (default: next to this program)")
	if err := set.Parse(args); err != nil {
		return err
	}
	for _, p := range []*string{&in.azimuth, &in.xbox, &in.uwpBase, &in.hotwheels, &in.blizzard, &in.out, dump} {
		*p = abs(*p)
	}
	if *dump != "" {
		b, h, z, err := findDumpPackages(*dump, in.out)
		if err != nil {
			return err
		}
		if in.uwpBase == "" {
			in.uwpBase = b
		}
		if in.hotwheels == "" {
			in.hotwheels = h
		}
		if in.blizzard == "" {
			in.blizzard = z
		}
		fmt.Println("found base game:", in.uwpBase)
		fmt.Println("found Hot Wheels:", in.hotwheels)
		fmt.Println("found Blizzard Mountain:", in.blizzard)
	}
	if in.azimuth == "" || in.out == "" {
		return errors.New("-azimuth and -out are required")
	}
	if *tool == "" {
		exe, _ := os.Executable()
		*tool = filepath.Join(filepath.Dir(exe), "azimuth-dlc.exe")
	}
	if _, err := os.Stat(*tool); err != nil {
		return fmt.Errorf("azimuth-dlc.exe is missing (%s)", *tool)
	}
	if err := checkSetup(&in); err != nil {
		return err
	}
	// Azimuth must accept the exe and be closed: azimuth-dlc checks both
	if out, err := exec.Command(*tool, "-azimuth", in.azimuth, "-check").CombinedOutput(); err != nil {
		return errors.New(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(string(out)), "error: ")))
	}
	if dst := filepath.Join(in.azimuth, "dinput8.dll"); fileExists(dst) && !isOurMod(dst) && fileExists(filepath.Join(in.azimuth, "dinput8.next.dll")) {
		return fmt.Errorf("%s belongs to another mod and dinput8.next.dll is taken too; move one of them away and run this again", dst)
	}
	if *check {
		fmt.Println("ok")
		return nil
	}

	need := dirSize(in.hotwheels) + dirSize(in.blizzard)
	os.MkdirAll(in.out, 0o755)
	if free, err := freeBytes(in.out); err == nil && free < need+need/10 {
		return fmt.Errorf("%s has %d GB free, the conversion can need %d GB", in.out, free>>30, (need+need/10)>>30)
	}
	hwOut, bmOut := filepath.Join(in.out, "HotWheels"), filepath.Join(in.out, "BlizzardMountain")
	marker := filepath.Join(in.out, markerName)
	if _, err := os.Stat(marker); err == nil {
		// an earlier run of this tool: its folders are ours to replace
		fmt.Println("removing the earlier conversion")
		for _, d := range []string{hwOut, bmOut} {
			if err := os.RemoveAll(d); err != nil {
				return fmt.Errorf("%s cannot be removed (is Azimuth or the game running?): %w", d, err)
			}
		}
	}
	for _, d := range []string{hwOut, bmOut} {
		if _, err := os.Stat(d); err == nil {
			return fmt.Errorf("%s exists and was not made by this tool; choose another output folder", d)
		}
	}

	if err := setupTiler(); err != nil {
		return err
	}
	if err := loadBaseShaders(in.media); err != nil {
		return err
	}
	zipOpts.decodeNames = false

	fmt.Println("== 1/5 Hot Wheels")
	if err := cmdConvertPackage(in.hotwheels, hwOut); err != nil {
		return fmt.Errorf("Hot Wheels: %w", err)
	}
	if _, err := os.Stat(filepath.Join(hwOut, "media", "Tracks", "island", "bin.zip")); err != nil {
		fmt.Println("== 2/5 island shaders from", in.islandZip)
		zipOpts.decodeNames = in.islandDecode
		st := &stats{}
		if err := convertZip(in.islandZip, filepath.Join(hwOut, "media", "Tracks", "island", "bin.zip"), st); err != nil {
			return fmt.Errorf("island shaders: %w", err)
		}
		st.print()
		zipOpts.decodeNames = false
	}
	fmt.Println("== 3/5 Blizzard Mountain")
	if err := cmdConvertPackage(in.blizzard, bmOut); err != nil {
		return fmt.Errorf("Blizzard Mountain: %w", err)
	}

	for _, d := range []string{hwOut, bmOut} {
		if err := setExpansionName(d); err != nil {
			return fmt.Errorf("naming %s: %w", d, err)
		}
	}

	fmt.Println("== 4/5 checking the result")
	bad := 0
	for _, root := range []string{hwOut, bmOut} {
		filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && !d.IsDir() && strings.EqualFold(filepath.Ext(p), ".zip") {
				if n, b, e := zipExtraCount(p); e != nil || b != 0 || n == 0 {
					fmt.Println("  problem in", p)
					bad++
				}
			}
			return nil
		})
	}
	if bad != 0 {
		return fmt.Errorf("%d archives did not pass the check; nothing was changed in Azimuth", bad)
	}
	os.WriteFile(marker, []byte(fmt.Sprintf("converted by uwp2xbox setup\r\nHot Wheels from %s\r\nBlizzard Mountain from %s\r\n", in.hotwheels, in.blizzard)), 0o644)

	fmt.Println("== 5/5 registering with Azimuth")
	if err := placeMod(filepath.Dir(*tool), in.azimuth); err != nil {
		return err
	}
	cmd := exec.Command(*tool, "-mod", "-azimuth", in.azimuth, "-blizzard", bmOut, "-hotwheels", hwOut)
	var buf bytes.Buffer
	cmd.Stdout, cmd.Stderr = &buf, &buf
	if err := cmd.Run(); err != nil {
		return errors.New(strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(buf.String()), "error: ")))
	}
	fmt.Println()
	fmt.Println("Done. Azimuth now lists Hot Wheels and Blizzard Mountain for Forza Horizon 3.")
	fmt.Println("Converted files are in", in.out)
	return nil
}
