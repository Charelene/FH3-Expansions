package main

import (
	"bytes"
	"compress/flate"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"hash/adler32"
	"hash/crc32"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
)

type stats struct {
	mu     sync.Mutex
	counts map[string]int
}

func (s *stats) add(k string) {
	s.mu.Lock()
	if s.counts == nil {
		s.counts = map[string]int{}
	}
	s.counts[k]++
	s.mu.Unlock()
}

func (s *stats) print() {
	ks := []string{}
	for k := range s.counts {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("  %-60s %d\n", k, s.counts[k])
	}
}

// convertBundleBytes converts a UWP texture bundle; ok=false means "leave as is".
// lay != nil marks a track group bundle.
func convertBundleBytes(b []byte, cand map[gkey]map[gval]int, st *stats) ([]byte, bool, error) {
	if len(b) < 0x14 || string(b[:4]) != "burG" {
		return nil, false, nil
	}
	bu, err := parseBundle(b)
	if err != nil {
		return nil, false, nil
	}
	changed := false
	for i := range bu.blobs {
		bl := &bu.blobs[i]
		if bl.tag != "BCXT" {
			continue
		}
		if bl.major == 2 {
			st.add("texture already Xbox")
			continue
		}
		if bl.major != 0 || len(bl.metas) != 1 || bl.metas[0].tag != "HCXT" {
			return nil, false, fmt.Errorf("BCXT v%d.%d with %d metadata records", bl.major, bl.minor, len(bl.metas))
		}
		t, err := parsePCHeader(bl.metas[0].payload)
		if err != nil {
			return nil, false, err
		}
		var hdr, data []byte
		if cand != nil {
			lay, found := pickLayout(cand, t)
			if !found {
				lay = gval{t.w, t.h, t.mips, 0}
				st.add("track bundle without pvs record (converted as whole texture)")
			}
			hdr, data, err = convertGroup(t, bl.data, lay)
			st.add("track texture group")
		} else {
			hdr, data, err = convertFull(t, bl.data)
			st.add("texture")
		}
		if err != nil {
			return nil, false, fmt.Errorf("%v: %w", t, err)
		}
		bl.major, bl.minor = 2, 0
		bl.metas[0].payload = hdr
		bl.data = data
		changed = true
	}
	if !changed {
		return nil, false, nil
	}
	return bu.serialize(), true, nil
}

// ---- zip archives -------------------------------------------------------------

// convertZip rewrites a zip, keeping every original record byte for byte and
// changing only the CRC, sizes and offsets of converted entries. The game's
// zips carry an extra field (id 0x1123) in the central directory only; the
// game's reader expects local headers exactly as its packer wrote them.
func convertZip(src, dst string, st *stats) error {
	raw, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	eocd := bytes.LastIndex(raw, []byte("PK\x05\x06"))
	if eocd < 0 || eocd+22 > len(raw) {
		return errors.New("no end of central directory")
	}
	n := int(le.Uint16(raw[eocd+10:]))
	cdSize := int(le.Uint32(raw[eocd+12:]))
	cdOff := int(le.Uint32(raw[eocd+16:]))
	if le.Uint16(raw[eocd+8:]) != uint16(n) || cdOff+cdSize > eocd || n == 0xffff || cdOff == -1 {
		return errors.New("unsupported zip (multi-disk or zip64)")
	}
	type ent struct {
		cd       []byte // central record
		lh       []byte // local header incl. name and extra
		data     []byte // compressed data
		name     string
		method   uint16
		newData  []byte
		newCRC   uint32
		newUsize uint32
		newName  string // set when the entry is renamed
		drop     bool   // left out of the output
	}
	ents := make([]ent, n)
	p := cdOff
	for i := 0; i < n; i++ {
		if p+46 > len(raw) || string(raw[p:p+4]) != "PK\x01\x02" {
			return fmt.Errorf("bad central record %d", i)
		}
		nl, el, cl := int(le.Uint16(raw[p+28:])), int(le.Uint16(raw[p+30:])), int(le.Uint16(raw[p+32:]))
		rec := raw[p : p+46+nl+el+cl]
		lo := int(le.Uint32(rec[42:]))
		if lo+30 > len(raw) || string(raw[lo:lo+4]) != "PK\x03\x04" {
			return fmt.Errorf("bad local header %d", i)
		}
		if le.Uint16(raw[lo+6:])&0x8 != 0 {
			return fmt.Errorf("entry %d uses a data descriptor", i)
		}
		lnl, lel := int(le.Uint16(raw[lo+26:])), int(le.Uint16(raw[lo+28:]))
		csz := int(le.Uint32(rec[20:]))
		ds := lo + 30 + lnl + lel
		if ds+csz > len(raw) {
			return fmt.Errorf("entry %d data past end", i)
		}
		ents[i] = ent{cd: rec, lh: raw[lo:ds], data: raw[ds : ds+csz], name: string(rec[46 : 46+nl]), method: le.Uint16(rec[10:])}
		p += len(rec)
	}
	// Names. With zipOpts.decodeNames (developer builds only) encoded entry names are decoded;
	// shaders that exist only as a PC build get the name the console build asks
	// for, with the base game's own console build when it has one of that name.
	cur := func(e *ent) string {
		if e.newName != "" {
			return e.newName
		}
		return e.name
	}
	have := map[string]bool{}
	dirShader := map[string]string{}
	dirMaps := map[string]string{}
	for i := range ents {
		if zipOpts.decodeNames {
			d := realName(ents[i].name)
			if strings.Contains(d, "?") {
				return fmt.Errorf("%s: name cannot be decoded (%s)", ents[i].name, d)
			}
			ents[i].newName = d
		}
		lc := strings.ToLower(cur(&ents[i]))
		have[lc] = true
		if strings.HasSuffix(lc, ".shaderbin") {
			k := strings.LastIndexAny(lc, "/\\")
			dirShader[lc[:k+1]] = strings.TrimSuffix(lc[k+1:], ".shaderbin")
			if d, err := entPlain(ents[i].method, ents[i].data); err == nil {
				dirMaps[lc[:k+1]] = paramMaps(d)
			}
		}
	}
	for i := range ents {
		e := &ents[i]
		c := cur(e)
		l := strings.ToLower(c)
		if !strings.HasSuffix(l, ".pc.vso") && !strings.HasSuffix(l, ".pc.pso") {
			continue
		}
		k := strings.LastIndex(l, ".pc.")
		nn := c[:k] + ".durango." + c[k+4:]
		if have[strings.ToLower(nn)] {
			continue
		}
		e.newName = nn
		base := strings.ToLower(nn[strings.LastIndexAny(nn, "/\\")+1:])
		// The base game's console build is taken only when its shader agrees with
		// this archive's on the parameter maps; a same-named shader of another
		// revision would read its textures and constants from the wrong places.
		sameMaps := zipOpts.baseMaps[base] == dirMaps[l[:strings.LastIndexAny(l, "/\\")+1]]
		if _, ok := zipOpts.base[base]; ok && !sameMaps {
			st.add("shader: base game has another revision of it")
		}
		if f, ok := zipOpts.base[base]; ok && sameMaps {
			rc, err := f.Open()
			if err != nil {
				return err
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				return err
			}
			var cbuf bytes.Buffer
			fw, _ := flate.NewWriter(&cbuf, flate.DefaultCompression)
			fw.Write(b)
			fw.Close()
			e.newData, e.newCRC, e.newUsize = cbuf.Bytes(), crc32.ChecksumIEEE(b), uint32(len(b))
			st.add("shader: console build from the base game")
		} else {
			plain, err := entPlain(e.method, e.data)
			if err != nil {
				return fmt.Errorf("%s: %w", c, err)
			}
			dir := l[:strings.LastIndexAny(l, "/\\")+1]
			sh := dirShader[dir]
			what := "shader: PC build translated to the console form"
			b, _, terr := shaderPCToConsole(plain)
			if terr != nil {
				b = nil
				if sh != "" && strings.HasPrefix(base, sh) {
					sig, isgn := shaderSig(plain), shaderSigOf(plain, false)
					for level, w := range []string{"shader: stand-in with the same signatures", "shader: stand-in with the same inputs", "shader: stand-in of the same scenario only"} {
						var from string
						if b, from = standInLevel(sh, base[len(sh):], sig, isgn, level); b != nil {
							zipOpts.standIns[sh+" "+base[len(sh):]] = from
							what = w
							break
						}
					}
				}
			}
			if b == nil {
				e.drop = true
				st.add("shader: nothing to put in its place, left out")
				continue
			}
			var cbuf bytes.Buffer
			fw, _ := flate.NewWriter(&cbuf, flate.DefaultCompression)
			fw.Write(b)
			fw.Close()
			e.newData, e.newCRC, e.newUsize = cbuf.Bytes(), crc32.ChecksumIEEE(b), uint32(len(b))
			st.add(what)
		}
	}
	var wg sync.WaitGroup
	sem := make(chan struct{}, runtime.NumCPU())
	errs := make([]error, n)
	for i := range ents {
		if !strings.EqualFold(filepath.Ext(cur(&ents[i])), ".swatchbin") {
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(e *ent, i int) {
			defer wg.Done()
			defer func() { <-sem }()
			var plain []byte
			switch e.method {
			case 0:
				plain = e.data
			case 8:
				b, err := io.ReadAll(flate.NewReader(bytes.NewReader(e.data)))
				if err != nil {
					errs[i] = fmt.Errorf("%s: %w", e.name, err)
					return
				}
				plain = b
			default:
				errs[i] = fmt.Errorf("%s: compression method %d", e.name, e.method)
				return
			}
			nb, ok, err := convertBundleBytes(plain, nil, st)
			if err != nil {
				errs[i] = fmt.Errorf("%s: %w", e.name, err)
				return
			}
			if !ok {
				return
			}
			var cbuf bytes.Buffer
			fw, _ := flate.NewWriter(&cbuf, flate.DefaultCompression)
			fw.Write(nb)
			fw.Close()
			e.newData, e.newCRC, e.newUsize = cbuf.Bytes(), crc32.ChecksumIEEE(nb), uint32(len(nb))
		}(&ents[i], i)
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			return err
		}
	}
	var out bytes.Buffer
	out.Grow(len(raw) + len(raw)/16)
	offs := make([]uint32, n)
	lhLen := make([]int, n)
	kept := 0
	for i := range ents {
		e := &ents[i]
		if e.drop {
			continue
		}
		kept++
		offs[i] = uint32(out.Len())
		lh := e.lh
		if e.newName != "" || e.newData != nil {
			lh = append([]byte(nil), e.lh...)
		}
		if e.newName != "" {
			onl := int(le.Uint16(lh[26:]))
			nlh := append([]byte(nil), lh[:30]...)
			nlh = append(nlh, e.newName...)
			nlh = append(nlh, lh[30+onl:]...)
			le.PutUint16(nlh[26:], uint16(len(e.newName)))
			lh = nlh
		}
		lhLen[i] = len(lh)
		if e.newData == nil {
			out.Write(lh)
			out.Write(e.data)
			continue
		}
		le.PutUint16(lh[8:], 8)
		le.PutUint32(lh[14:], e.newCRC)
		le.PutUint32(lh[18:], uint32(len(e.newData)))
		le.PutUint32(lh[22:], e.newUsize)
		out.Write(lh)
		out.Write(e.newData)
	}
	newCD := out.Len()
	for i := range ents {
		e := &ents[i]
		if e.drop {
			continue
		}
		rec := append([]byte(nil), e.cd...)
		if e.newName != "" {
			onl := int(le.Uint16(rec[28:]))
			nrec := append([]byte(nil), rec[:46]...)
			nrec = append(nrec, e.newName...)
			nrec = append(nrec, rec[46+onl:]...)
			le.PutUint16(nrec[28:], uint16(len(e.newName)))
			rec = nrec
		}
		le.PutUint32(rec[42:], offs[i])
		// Extra field 0x1123 holds the entry's absolute data offset; the game
		// reads the data from there without parsing the local header.
		nl, el := int(le.Uint16(rec[28:])), int(le.Uint16(rec[30:]))
		ex := rec[46+nl : 46+nl+el]
		found := false
		for k := 0; k+4 <= len(ex); {
			id, sz := le.Uint16(ex[k:]), int(le.Uint16(ex[k+2:]))
			if k+4+sz > len(ex) {
				break
			}
			if id == 0x1123 && sz >= 4 {
				le.PutUint32(ex[k+4:], offs[i]+uint32(lhLen[i]))
				found = true
			}
			k += 4 + sz
		}
		if !found && el > 0 {
			return fmt.Errorf("%s: central extra field without the 0x1123 data offset", e.name)
		}
		if e.newData != nil {
			le.PutUint16(rec[10:], 8)
			le.PutUint32(rec[16:], e.newCRC)
			le.PutUint32(rec[20:], uint32(len(e.newData)))
			le.PutUint32(rec[24:], e.newUsize)
		}
		out.Write(rec)
	}
	end := append([]byte(nil), raw[eocd:]...)
	le.PutUint16(end[8:], uint16(kept))
	le.PutUint16(end[10:], uint16(kept))
	le.PutUint32(end[12:], uint32(out.Len()-newCD))
	le.PutUint32(end[16:], uint32(newCD))
	out.Write(end)
	if uint64(out.Len()) > 0xffffffff {
		return errors.New("converted zip exceeds 4 GiB")
	}
	return os.WriteFile(dst, out.Bytes(), 0o644)
}

// ---- minizips -------------------------------------------------------------------

// zlibHuffman writes a zlib stream with the 4 KiB window header the game's files
// use (0x48 0xc7) and Huffman-only deflate, which has no back references.
func zlibHuffman(b []byte) []byte {
	var buf bytes.Buffer
	buf.Write([]byte{0x48, 0xc7})
	fw, _ := flate.NewWriter(&buf, flate.HuffmanOnly)
	fw.Write(b)
	fw.Close()
	var a [4]byte
	binary.BigEndian.PutUint32(a[:], adler32.Checksum(b))
	buf.Write(a[:])
	return buf.Bytes()
}

func convertMinizip(src, dst string, cand map[gkey]map[gval]int, st *stats) error {
	m, err := openMinizip(src)
	if err != nil {
		return err
	}
	defer m.f.Close()
	// The game takes an entry's compressed size as the next entry's offset (in
	// index order) minus its own, minus the padding count in flag bits 14-15
	// (0x141c4c980). Streams must therefore lie in index order.
	for i := 1; i < len(m.entries); i++ {
		if m.entries[i].abs <= m.entries[i-1].abs {
			return fmt.Errorf("entry %d does not follow entry %d in the file; layout not handled", i, i-1)
		}
	}
	// unique source blobs in file order
	type blobRef struct {
		abs   int64
		csz   int64
		first int // first entry using it
	}
	byAbs := map[int64]int{}
	var blobs []blobRef
	for i, e := range m.entries {
		if _, ok := byAbs[e.abs]; ok {
			continue
		}
		byAbs[e.abs] = len(blobs)
		blobs = append(blobs, blobRef{e.abs, e.csz, i})
	}
	sort.Slice(blobs, func(i, j int) bool { return blobs[i].abs < blobs[j].abs })
	for k, b := range blobs {
		byAbs[b.abs] = k
	}
	newUsize := make([]uint32, len(blobs))
	// Flag bits 14-15 of an entry count the zero bytes that pad its stream to a
	// 4-byte boundary; the reader decompresses slot length minus that count.
	newPad := make([]int, len(blobs)) // -1: stream copied as is, keep the flags
	newAbs := make([]int64, len(blobs))

	cnt, ng, per := len(m.entries), len(m.groups), m.perPage
	pages := (cnt + per - 1) / per
	idxSize := 0x20 + ng*4 + 4
	if idxSize%8 != 0 {
		idxSize += 4
	}
	idxSize += pages*8 + cnt*12 + 12
	dataStart := int64(idxSize)

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	// ordered parallel pipeline: convert in workers, write in file order
	type res struct {
		k     int
		data  []byte
		usize uint32
		pad   int
		err   error
	}
	jobs := make(chan int, 64)
	results := make(chan res, 64)
	var wg sync.WaitGroup
	var failed atomic.Bool
	for w := 0; w < runtime.NumCPU(); w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			f, err := os.Open(src)
			if err != nil {
				failed.Store(true)
				return
			}
			defer f.Close()
			for k := range jobs {
				b := blobs[k]
				raw := make([]byte, b.csz)
				if _, err := f.ReadAt(raw, b.abs); err != nil {
					results <- res{k: k, err: err}
					continue
				}
				r := res{k: k, data: raw, usize: m.entries[b.first].usize, pad: -1}
				if failed.Load() {
					results <- r
					continue
				}
				zr, err := newZlibReader(raw)
				if err == nil {
					plain, err2 := io.ReadAll(zr)
					if err2 == nil && uint32(len(plain)) == r.usize {
						nb, ok, err3 := convertBundleBytes(plain, cand, st)
						if err3 != nil {
							r.err = fmt.Errorf("entry %d: %w", b.first, err3)
						} else if ok {
							z := zlibHuffman(nb)
							pad := (4 - len(z)%4) % 4
							r.data, r.usize, r.pad = append(z, make([]byte, pad)...), uint32(len(nb)), pad
						}
					}
				}
				results <- r
			}
		}()
	}
	go func() {
		for k := range blobs {
			jobs <- k
		}
		close(jobs)
	}()
	go func() { wg.Wait(); close(results) }()

	pending := map[int]res{}
	next := 0
	pos := dataStart
	var firstErr error
	for r := range results {
		if r.err != nil && firstErr == nil {
			firstErr = r.err
			failed.Store(true)
		}
		pending[r.k] = r
		for {
			x, ok := pending[next]
			if !ok {
				break
			}
			delete(pending, next)
			if firstErr == nil {
				if _, err := out.WriteAt(x.data, pos); err != nil {
					firstErr = err
					failed.Store(true)
				}
			}
			newAbs[next], newUsize[next], newPad[next] = pos, x.usize, x.pad
			pos += int64(len(x.data))
			next++
		}
	}
	if firstErr != nil {
		return firstErr
	}
	end := pos
	// The originals are zero-padded to a 4 KiB multiple; the game opens them
	// unbuffered, so reads are whole sectors.
	if err := out.Truncate((end + 0xfff) &^ 0xfff); err != nil {
		return err
	}

	// index
	idx := make([]byte, idxSize)
	copy(idx, m.hdr)
	p := 0x20
	for _, g := range m.groups {
		le.PutUint32(idx[p:], g)
		p += 4
	}
	le.PutUint32(idx[p:], uint32(cnt))
	p += 4
	if p%8 != 0 {
		p += 4
	}
	var base int64
	for i, e := range m.entries {
		if i%per == 0 {
			base = 1 << 62
			for j := i; j < cnt && j < i+per; j++ {
				if a := newAbs[byAbs[m.entries[j].abs]]; a < base {
					base = a
				}
			}
			le.PutUint64(idx[p:], uint64(base))
			p += 8
		}
		k := byAbs[e.abs]
		rel := newAbs[k] - base
		if rel < 0 || rel > 0xffffffff {
			return fmt.Errorf("entry %d offset does not fit its page", i)
		}
		le.PutUint32(idx[p:], uint32(rel))
		le.PutUint32(idx[p+4:], newUsize[k])
		fl := e.flags
		if newPad[k] >= 0 {
			fl = fl&^0xc000 | uint32(newPad[k])<<14
		}
		le.PutUint32(idx[p+8:], fl)
		p += 12
	}
	if end-base > 0xffffffff {
		return errors.New("end offset does not fit the last page")
	}
	le.PutUint32(idx[p:], uint32(end-base))
	copy(idx[p+4:], m.trailer)
	if p+12 != idxSize {
		return fmt.Errorf("index size mismatch %d vs %d", p+12, idxSize)
	}
	_, err = out.WriteAt(idx, 0)
	return err
}

// ---- package ---------------------------------------------------------------------

func linkOrCopy(src, dst string) error {
	if err := os.Link(src, dst); err == nil {
		return nil
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		return err
	}
	return out.Close()
}

// cmdConvertPackage converts every texture of a UWP content package into dst.
func cmdConvertPackage(src, dst string) error {
	if _, err := os.Stat(dst); err == nil {
		return fmt.Errorf("%s exists; remove it or choose another folder", dst)
	}
	st := &stats{}
	// pvs candidates per track directory (the folder holding GeoChunk*.minizip)
	cands := map[string]map[gkey]map[gval]int{}
	err := filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(src, p)
		out := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(out, 0o755)
		}
		switch strings.ToLower(filepath.Ext(p)) {
		case ".zip":
			fmt.Println("zip    ", rel)
			return convertZip(p, out, st)
		case ".pvs":
			fmt.Println("pvs    ", rel)
			b, recs, err := readPVS(p)
			if err != nil {
				return err
			}
			if err := convertPVS(b, recs); err != nil {
				return err
			}
			st.add(fmt.Sprintf("pvs texture records (%s)", filepath.Base(p)))
			return os.WriteFile(out, b, 0o644)
		case ".minizip":
			return nil // after all pvs files are read
		}
		return linkOrCopy(p, out)
	})
	if err != nil {
		return err
	}
	return filepath.Walk(src, func(p string, info os.FileInfo, err error) error {
		if err != nil || info.IsDir() || !strings.EqualFold(filepath.Ext(p), ".minizip") {
			return err
		}
		dir := filepath.Dir(p)
		c, ok := cands[dir]
		if !ok {
			pv, _ := filepath.Glob(filepath.Join(dir, "Ribbon_*", "*.pvs"))
			c = map[gkey]map[gval]int{}
			for _, f := range pv {
				_, recs, err := readPVS(f)
				if err != nil {
					return err
				}
				for k, v := range groupCandidates(recs) {
					if c[k] == nil {
						c[k] = map[gval]int{}
					}
					for vv, n := range v {
						c[k][vv] += n
					}
				}
			}
			cands[dir] = c
		}
		rel, _ := filepath.Rel(src, p)
		fmt.Println("minizip", rel)
		if err := convertMinizip(p, filepath.Join(dst, rel), c, st); err != nil {
			return fmt.Errorf("%s: %w", rel, err)
		}
		st.print()
		return nil
	})
}

func newZlibReader(b []byte) (io.Reader, error) { return zlib.NewReader(bytes.NewReader(b)) }

// entPlain returns an entry's uncompressed data.
func entPlain(method uint16, data []byte) ([]byte, error) {
	switch method {
	case 0:
		return data, nil
	case 8:
		return io.ReadAll(flate.NewReader(bytes.NewReader(data)))
	}
	return nil, fmt.Errorf("compression method %d", method)
}
