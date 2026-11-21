package main

import (
	"archive/zip"
	"crypto/sha256"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type baseShader struct {
	shader string // shader name (its .shaderbin)
	f      *zip.File
	sig    string // input and output signature, filled on demand
	isgn   string // input signature alone
}

// zipOpts steers convertZip.
var zipOpts struct {
	decodeNames bool
	base        map[string]*zip.File     // console shader builds of the base game, by lower-case file name
	bySuffix    map[string][]*baseShader // the same, by scenario suffix ("tracknightlightscenario.durango.pso")
	standIns    map[string]string        // report: shader -> stand-in used
	baseMaps    map[string]string        // parameter maps of the shaderbin beside each base console build, by file name
}

// defaultXboxMedia is the Xbox game's Mount\media for the developer commands
// (UWP2XBOX_XBOX_MEDIA names it); setup takes it from azimuth.ini.
var defaultXboxMedia = os.Getenv("UWP2XBOX_XBOX_MEDIA")

// loadBaseShaders indexes the console (.durango) shader builds the Xbox base game ships.
func loadBaseShaders(media string) error {
	zipOpts.base = map[string]*zip.File{}
	zipOpts.bySuffix = map[string][]*baseShader{}
	zipOpts.standIns = map[string]string{}
	zipOpts.baseMaps = map[string]string{}
	for _, rel := range []string{`_library\Shaders.zip`, `Tracks\homespace02\bin.zip`, `Tracks\australia\bin.zip`} {
		r, err := zip.OpenReader(filepath.Join(media, rel))
		if err != nil {
			return err
		}
		dirBin := map[string]string{}
		dirMaps := map[string]string{}
		for _, f := range r.File {
			l := strings.ToLower(f.Name)
			if strings.HasSuffix(l, ".shaderbin") {
				k := strings.LastIndexAny(l, "/\\")
				dirBin[l[:k+1]] = strings.TrimSuffix(l[k+1:], ".shaderbin")
				if d, err := readZipFile(f); err == nil {
					dirMaps[l[:k+1]] = paramMaps(d)
				}
			}
		}
		for _, f := range r.File {
			l := strings.ToLower(f.Name)
			if !strings.HasSuffix(l, ".durango.vso") && !strings.HasSuffix(l, ".durango.pso") {
				continue
			}
			k := strings.LastIndexAny(l, "/\\")
			base := l[k+1:]
			zipOpts.base[base] = f
			zipOpts.baseMaps[base] = dirMaps[l[:k+1]]
			if sh, ok := dirBin[l[:k+1]]; ok && strings.HasPrefix(base, sh) {
				suf := base[len(sh):]
				zipOpts.bySuffix[suf] = append(zipOpts.bySuffix[suf], &baseShader{shader: sh, f: f})
			}
		}
	}
	fmt.Printf("base game console shaders indexed: %d\n", len(zipOpts.base))
	return nil
}

// shaderSig identifies a shader's input and output signatures (or the input one alone).
func shaderSig(b []byte) string { return shaderSigOf(b, true) }

func shaderSigOf(b []byte, withOutput bool) string {
	h := sha256.New()
	for _, c := range dxbcChunks(b) {
		if c.tag == "ISGN" || (withOutput && c.tag == "OSGN") {
			h.Write([]byte(c.tag))
			h.Write(c.data)
		}
	}
	return string(h.Sum(nil))
}

func readZipFile(f *zip.File) ([]byte, error) {
	rc, err := f.Open()
	if err != nil {
		return nil, err
	}
	defer rc.Close()
	return io.ReadAll(rc)
}

// standIn finds a console build of another shader with the same scenario,
// preferring the most similar shader name. level 0 wants equal input and output
// signatures, 1 an equal input signature, 2 takes any.
func standIn(shader, suffix, sig string) ([]byte, string) {
	return standInLevel(shader, suffix, sig, "", 0)
}

func standInLevel(shader, suffix, sig, isgn string, level int) ([]byte, string) {
	cands := zipOpts.bySuffix[suffix]
	common := func(a, b string) int {
		n := 0
		for n < len(a) && n < len(b) && a[n] == b[n] {
			n++
		}
		return n
	}
	sort.SliceStable(cands, func(i, j int) bool {
		ci, cj := common(cands[i].shader, shader), common(cands[j].shader, shader)
		if ci != cj {
			return ci > cj
		}
		return len(cands[i].shader) < len(cands[j].shader)
	})
	for _, c := range cands {
		b, err := readZipFile(c.f)
		if err != nil {
			continue
		}
		if c.sig == "" {
			c.sig = shaderSig(b)
			c.isgn = shaderSigOf(b, false)
		}
		if level == 2 || (level == 1 && c.isgn == isgn) || (level == 0 && c.sig == sig) {
			return b, c.shader
		}
	}
	return nil, ""
}

func printStandIns() {
	ks := []string{}
	for k := range zipOpts.standIns {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("  stand-in: %-52s <- %s\n", k, zipOpts.standIns[k])
	}
}

// paramMaps returns the parameter maps of a shaderbin (constants, textures,
// samplers) as one comparable string. Two shaders of the same name agree on
// where their material is bound exactly when these are equal.
func paramMaps(shaderbin []byte) string {
	bu, err := parseBundle(shaderbin)
	if err != nil {
		return "unreadable"
	}
	out := ""
	for _, bl := range bu.blobs {
		if bl.tag == "PMBC" || bl.tag == "PMXT" || bl.tag == "PMPS" {
			out += bl.tag + string(bl.data)
		}
	}
	return out
}
