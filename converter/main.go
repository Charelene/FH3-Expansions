// uwp2xbox converts the textures of Forza Horizon 3 UWP (PC) content into the
// Xbox One layout the console executable expects, so UWP expansion packages
// can run under an Xbox One compatibility layer.
package main

import (
	"archive/zip"
	"bufio"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
)

// defaultXG is the GDK's desktop xg.dll for the developer commands that compare
// against it (not included; UWP2XBOX_XG_DLL names it).
var defaultXG = envOr("UWP2XBOX_XG_DLL", "xg.dll")

func envOr(name, def string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}
	return def
}

func zipEntry(path, suffix string) ([]byte, string, error) {
	r, err := zip.OpenReader(path)
	if err != nil {
		return nil, "", err
	}
	defer r.Close()
	for _, f := range r.File {
		if strings.HasSuffix(strings.ToLower(f.Name), strings.ToLower(suffix)) {
			rc, err := f.Open()
			if err != nil {
				return nil, "", err
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			return b, f.Name, err
		}
	}
	return nil, "", fmt.Errorf("%s: no entry ending in %s", path, suffix)
}

func describe(b []byte) {
	bu, err := parseBundle(b)
	if err != nil {
		fmt.Println("  ", err)
		return
	}
	for _, bl := range bu.blobs {
		fmt.Printf("  blob %s v%d.%d data 0x%x\n", bl.tag, bl.major, bl.minor, len(bl.data))
		for _, m := range bl.metas {
			fmt.Printf("    meta %s lo4 %d word2 %d payload 0x%x\n", m.tag, m.lo4, m.word2, len(m.payload))
			if bl.tag == "BCXT" && m.tag == "HCXT" {
				var t *texInfo
				if bl.major == 2 {
					t, err = parseXboxHeader(m.payload)
				} else {
					t, err = parsePCHeader(m.payload)
				}
				if err != nil {
					fmt.Println("    ", err)
				} else {
					fmt.Println("    ", t)
				}
			}
		}
	}
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("usage: uwp2xbox dump-zip <zip> <entry suffix> | dump-mz <minizip> <index>...")
		os.Exit(2)
	}
	switch os.Args[1] {
	case "dump-zip":
		b, name, err := zipEntry(os.Args[2], os.Args[3])
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		fmt.Println(name)
		describe(b)
	case "guid-mz":
		m, _ := openMinizip(os.Args[2])
		i, _ := strconv.Atoi(os.Args[3])
		b, _ := m.read(i)
		bu, _ := parseBundle(b)
		fmt.Printf("%x\n", bu.blobs[0].metas[0].payload[8:24])
	case "dump-mz":
		m, err := openMinizip(os.Args[2])
		if err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		for _, a := range os.Args[3:] {
			i, _ := strconv.Atoi(a)
			b, err := m.read(i)
			fmt.Printf("entry %d flags 0x%x usize 0x%x\n", i, m.entries[i].flags, m.entries[i].usize)
			if err != nil {
				fmt.Println("  ", err)
				continue
			}
			describe(b)
		}
	case "compare-zip":
		if err := setupTiler(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if err := cmdCompareZip(os.Args[2], os.Args[3], os.Args[4]); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
	case "validate":
		if err := setupTiler(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		cmdValidate(os.Args[2], os.Args[3:])
	case "indep":
		if err := loadXG(defaultXG); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		cmdIndep()
	case "mzstats":
		n, _ := strconv.Atoi(os.Args[3])
		cmdMzStats(os.Args[2], n)
	case "pvscheck":
		cmdPVSCheck(os.Args[2], os.Args[3:])
	case "convert":
		if err := setupTiler(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if err := loadBaseShaders(defaultXboxMedia); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if err := cmdConvertPackage(os.Args[2], os.Args[3]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
		printStandIns()
		printTileTally()
		fmt.Println("done")
	case "test-mz":
		if err := setupTiler(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if err := cmdTestMinizip(os.Args[2], os.Args[3]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "verify-mz":
		if err := cmdVerifyMinizip(os.Args[2], os.Args[3], os.Args[4]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "aligntest":
		if err := loadXG(defaultXG); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		cmdAlignTest()
	case "zipcheck":
		if err := cmdZipCheck(os.Args[2], os.Args[3]); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
	case "granny":
		cmdGrannyScan(os.Args[2:])
	case "convert-zip":
		if err := setupTiler(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		if err := loadBaseShaders(defaultXboxMedia); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		zipOpts.decodeNames = len(os.Args) > 4 && os.Args[4] == "decode"
		if zipOpts.decodeNames && nameTable == nil {
			fmt.Println("decode needs a name table (developer builds only)")
			os.Exit(2)
		}
		st := &stats{}
		if err := convertZip(os.Args[2], os.Args[3], st); err != nil {
			fmt.Println("error:", err)
			os.Exit(1)
		}
		st.print()
		printStandIns()
	case "zipextra":
		n, _ := strconv.Atoi(os.Args[3])
		if err := cmdZipExtra(os.Args[2], n); err != nil {
			fmt.Println("error:", err)
		}
	case "padcheck":
		n, _ := strconv.Atoi(os.Args[3])
		cmdPadCheck(os.Args[2], n)
	case "mzorder":
		for _, f := range os.Args[2:] {
			cmdMzOrder(f)
		}
	case "pvsdiff":
		cmdPVSDiff(os.Args[2], os.Args[3])
	case "peek":
		i, _ := strconv.Atoi(os.Args[3])
		cmdPeek(os.Args[2], i)
	case "recnames":
		n, _ := strconv.Atoi(os.Args[3])
		cmdRecNames(os.Args[2], n)
	case "nested":
		n, _ := strconv.Atoi(os.Args[3])
		cmdNested(os.Args[2], n)
	case "xcheck":
		if err := setupTiler(); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		mx, _ := strconv.ParseInt(os.Args[2], 10, 64)
		cmdXCheck(os.Args[4:], os.Args[3], mx)
	case "zipnames":
		show := ""
		if len(os.Args) > 4 {
			show = os.Args[4]
		}
		cmdZipNames(os.Args[2], os.Args[3] == "decode", show)
	case "shaderpairs":
		cmdShaderPairs(os.Args[2])
	case "shadercov":
		cmdShaderCov(os.Args[2], os.Args[3] == "decode", os.Args[4:])
	case "shaderdecls":
		n, _ := strconv.Atoi(os.Args[3])
		cmdShaderDecls(os.Args[2], n)
	case "shaderbincmp":
		cmdShaderBinCmp(os.Args[2], os.Args[3] == "decode", os.Args[4:])
	case "shaderbinblobs":
		cmdShaderBinBlobs(os.Args[2], os.Args[3] == "decode", os.Args[4:])
	case "whichshader":
		cmdWhichShader(os.Args[2], os.Args[3:])
	case "shadertest":
		n, _ := strconv.Atoi(os.Args[4])
		cmdShaderTest(os.Args[2], os.Args[3] == "decode", n)
	case "decltok":
		cmdDeclTok(os.Args[2], os.Args[3])
	case "samplelines":
		cmdSampleLines(os.Args[2], os.Args[3])
	case "spaces":
		loadBaseShaders(defaultXboxMedia)
		n, _ := strconv.Atoi(os.Args[4])
		cmdSpaces(os.Args[2], os.Args[3] == "decode", n)
	case "pairdis":
		loadBaseShaders(defaultXboxMedia)
		cmdPairDis(os.Args[2], os.Args[3] == "decode", os.Args[4], os.Args[5])
	case "missingshaders":
		loadBaseShaders(defaultXboxMedia)
		cmdMissingShaders(os.Args[2], os.Args[3] == "decode")
	case "leftout":
		loadBaseShaders(defaultXboxMedia)
		cmdLeftOut(os.Args[2], os.Args[3] == "decode", len(os.Args) > 4)
	case "sbdump":
		n, _ := strconv.Atoi(os.Args[5])
		cmdSBDump(os.Args[2], os.Args[3] == "decode", os.Args[4], n)
	case "sbmaps":
		cmdSBMaps(os.Args[2], os.Args[3] == "decode")
	case "mydis":
		cmdMyDis(os.Args[2], os.Args[3] == "decode", os.Args[4])
	case "cbscan":
		cmdCBScan(os.Args[2], os.Args[3] == "decode", os.Args[4])
	case "shgrep":
		mx, _ := strconv.Atoi(os.Args[6])
		cx, _ := strconv.Atoi(os.Args[7])
		cmdShGrep(os.Args[2], os.Args[3] == "decode", os.Args[4], os.Args[5], mx, cx)
	case "xlstats":
		show := ""
		if len(os.Args) > 4 {
			show = os.Args[4]
		}
		cmdXlateStats(os.Args[2], os.Args[3] == "decode", show)
	case "xlone":
		cmdXlateOne(os.Args[2], os.Args[3] == "decode", os.Args[4], os.Args[5])
	case "xlcheck":
		loadBaseShaders(defaultXboxMedia)
		n, _ := strconv.Atoi(os.Args[4])
		cmdXlateCheck(os.Args[2], os.Args[3] == "decode", n)
	case "tilerprobe":
		if err := loadXG(defaultXG); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		cmdTilerProbe(os.Args[2:])
	case "tilercheck":
		if err := loadXG(defaultXG); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		n, _ := strconv.Atoi(os.Args[2])
		seed := int64(1)
		if len(os.Args) > 3 {
			seed, _ = strconv.ParseInt(os.Args[3], 10, 64)
		}
		cmdTilerCheck(n, seed)
	case "tilemap":
		if err := loadXG(defaultXG); err != nil {
			fmt.Println(err)
			os.Exit(1)
		}
		cmdTileMap(os.Args[2:])
	case "treecmp":
		cmdTreeCmp(os.Args[2], os.Args[3])
	case "setup":
		err := cmdSetup(os.Args[2:])
		if err != nil {
			fmt.Fprintln(os.Stderr, "error:", err)
		}
		if setupPause {
			fmt.Println("\nPress Enter to close this window.")
			bufio.NewReader(os.Stdin).ReadString('\n')
		}
		if err != nil {
			os.Exit(1)
		}
	case "mzdiff":
		n, _ := strconv.Atoi(os.Args[4])
		cmdMzDiff(os.Args[2], os.Args[3], n)
	case "find":
		cmdFind(os.Args[2:])
	case "swatches":
		if len(os.Args) < 5 {
			fmt.Println("usage: uwp2xbox swatches <zip> <out dir> <name part>...")
			os.Exit(2)
		}
		if err := loadXG(defaultXG); err != nil {
			fmt.Println("xg.dll:", err)
			os.Exit(1)
		}
		if err := runSwatches(os.Args[2], os.Args[3], os.Args[4:]); err != nil {
			fmt.Println("swatches:", err)
			os.Exit(1)
		}
	case "thumbs":
		if len(os.Args) < 5 {
			fmt.Println("usage: uwp2xbox thumbs <Mount/media/stripped> <out dir> <car id>...")
			os.Exit(2)
		}
		if err := loadXG(defaultXG); err != nil {
			fmt.Println("xg.dll:", err)
			os.Exit(1)
		}
		if err := runThumbs(os.Args[2], os.Args[3], os.Args[4:]); err != nil {
			fmt.Println("thumbs:", err)
			os.Exit(1)
		}
	default:
		if f, ok := devCommands[os.Args[1]]; ok {
			if err := f(os.Args[2:]); err != nil {
				fmt.Println(os.Args[1]+":", err)
				os.Exit(1)
			}
			return
		}
		fmt.Println("unknown command")
		os.Exit(2)
	}
}

// devCommands holds commands that only developer builds have (files with the
// build tag "dev" register them in init).
var devCommands = map[string]func(args []string) error{}
