package main

// GeoChunk*.minizip ("PGZP"), read by IOSys::CCompressedVolume
// (index size computed in 0x141c522c0).
//
//   0x00 "PGZP", u32 100, u32 0x20 (header size), u32 entry count,
//        u32 group count, u32 entries per page (0x200), u32 page count, u32 1
//   0x20 u32 group table [group count] (first entry index of each group)
//        u32 entry count again
//        pad to 8
//   pages: u64 absolute file offset of the page's data, then up to 512 entries
//          {u32 offset from page base, u32 uncompressed size, u32 flags}
//          flags: low 14 bits 8 (zlib), bits 14-15 kept as is, high 16 bits group
//   u32 end offset relative to the last page base, u64 0
//   data: zlib streams. Several entries may share one stream (duplicates).

import (
	"bytes"
	"compress/zlib"
	"errors"
	"fmt"
	"io"
	"os"
	"sort"
)

type mzEntry struct {
	abs   int64
	usize uint32
	flags uint32
	csz   int64
}

type minizip struct {
	f       *os.File
	hdr     []byte
	groups  []uint32
	perPage int
	entries []mzEntry
	end     int64
	trailer []byte // the 8 bytes after the end offset
}

func openMinizip(path string) (*minizip, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	m := &minizip{f: f, hdr: make([]byte, 0x20)}
	if _, err := f.ReadAt(m.hdr, 0); err != nil {
		return nil, err
	}
	if string(m.hdr[:4]) != "PGZP" || le.Uint32(m.hdr[4:]) != 100 || le.Uint32(m.hdr[8:]) != 0x20 {
		return nil, errors.New("not a PGZP v100 minizip")
	}
	cnt := int(le.Uint32(m.hdr[0xc:]))
	ng := int(le.Uint32(m.hdr[0x10:]))
	m.perPage = int(le.Uint32(m.hdr[0x14:]))
	pages := int(le.Uint32(m.hdr[0x18:]))
	if m.perPage <= 0 || (cnt+m.perPage-1)/m.perPage != pages {
		return nil, fmt.Errorf("page count %d does not fit %d entries of %d", pages, cnt, m.perPage)
	}
	idx := make([]byte, 0x20+ng*4+8+pages*8+cnt*12+16)
	if _, err := f.ReadAt(idx, 0); err != nil {
		return nil, err
	}
	p := 0x20
	for i := 0; i < ng; i++ {
		m.groups = append(m.groups, le.Uint32(idx[p:]))
		p += 4
	}
	if le.Uint32(idx[p:]) != uint32(cnt) {
		return nil, fmt.Errorf("entry count not found at 0x%x", p)
	}
	p += 4
	if p%8 != 0 {
		p += 4
	}
	var base int64
	for i := 0; i < cnt; i++ {
		if i%m.perPage == 0 {
			base = int64(le.Uint64(idx[p:]))
			p += 8
		}
		m.entries = append(m.entries, mzEntry{abs: base + int64(le.Uint32(idx[p:])), usize: le.Uint32(idx[p+4:]), flags: le.Uint32(idx[p+8:])})
		p += 12
	}
	m.end = base + int64(le.Uint32(idx[p:]))
	m.trailer = append([]byte(nil), idx[p+4:p+12]...)
	offs := make([]int64, 0, cnt+1)
	for _, e := range m.entries {
		offs = append(offs, e.abs)
	}
	offs = append(offs, m.end)
	sort.Slice(offs, func(i, j int) bool { return offs[i] < offs[j] })
	for i := range m.entries {
		k := sort.Search(len(offs), func(j int) bool { return offs[j] > m.entries[i].abs })
		m.entries[i].csz = offs[k] - m.entries[i].abs
	}
	return m, nil
}

func (m *minizip) raw(i int) ([]byte, error) {
	e := m.entries[i]
	buf := make([]byte, e.csz)
	if _, err := m.f.ReadAt(buf, e.abs); err != nil {
		return nil, err
	}
	return buf, nil
}

func (m *minizip) read(i int) ([]byte, error) {
	buf, err := m.raw(i)
	if err != nil {
		return nil, err
	}
	r, err := zlib.NewReader(bytes.NewReader(buf))
	if err != nil {
		return nil, err
	}
	b, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	if uint32(len(b)) != m.entries[i].usize {
		return nil, fmt.Errorf("entry %d: %d bytes, index says %d", i, len(b), m.entries[i].usize)
	}
	return b, nil
}
