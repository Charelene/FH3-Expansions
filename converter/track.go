package main

import (
	"bytes"
	"errors"
	"fmt"
	"math/bits"
)

// Track textures. A .pvs record describes the whole texture (size, mips, total
// bytes, flags, streaming groups); each group is a separate bundle in a GeoChunk
// minizip that the loader (0x141e561b0) copies straight into the texture memory
// at the group's offset after checking that its size equals the group's span.
// With the console tile mode every mip tiles the same whether or not it is part
// of a larger chain, and XG packs mips without gaps, so groups convert one by one.

func (r *pvsRec) info() *texInfo {
	return &texInfo{w: r.w, h: r.h, d: r.d, array: 1, mips: r.mips,
		a: r.flags >> 5 & 0x3f, b: r.flags >> 11 & 0x3f, c: r.flags >> 20 & 7}
}

// xboxLayout computes the console layout of a full texture.
func xboxLayout(t *texInfo) (*xgResourceLayout, uint32, uint64, error) {
	f, err := t.dxgi()
	if err != nil {
		return nil, 0, 0, err
	}
	desc := xgTex2D{Width: t.w, Height: t.h, MipLevels: t.mips, ArraySize: 1, Format: f, SampleCount: 1, BindFlags: xgBindShaderResource}
	desc.TileMode = optimalTileMode(f, t.w, t.h, 1, 0)
	c, err := create2D(&desc)
	if err != nil {
		return nil, 0, 0, err
	}
	defer c.release()
	l, err := c.layout()
	if err != nil {
		return nil, 0, 0, err
	}
	return l, desc.TileMode, c.baseAlignment(), nil
}

// convertPVS rewrites every record of the texture table in place.
func convertPVS(b []byte, recs []pvsRec) error {
	for i, r := range recs {
		if r.flags&0x1f != 8 {
			return fmt.Errorf("record %d: tile mode %d, expected the UWP linear mode 8", i, r.flags&0x1f)
		}
		if r.d != 1 {
			return fmt.Errorf("record %d: depth %d not handled", i, r.d)
		}
		l, tile, _, err := xboxLayout(r.info())
		if err != nil {
			return fmt.Errorf("record %d (%dx%d mips %d): %w", i, r.w, r.h, r.mips, err)
		}
		o := r.off
		le.PutUint32(b[o+0x10:], uint32(l.SizeBytes))
		le.PutUint32(b[o+0x14:], r.flags&^0x1f|tile)
		for g, gr := range r.groupList() {
			off := l.Plane[0].MipLayout[gr.first].OffsetBytes
			le.PutUint32(b[o+0x18+g*4:], gr.count<<28|uint32(off))
		}
	}
	return nil
}

// convertGroup re-tiles a UWP group bundle's mips. Each mip is tiled as the top
// mip of a texture of its size, which XG lays out exactly as inside the full chain.
func convertGroup(t *texInfo, data []byte, lay gval) ([]byte, []byte, error) {
	if t.cube || t.volume || t.d > 1 || t.array > 1 {
		return nil, nil, errors.New("only plain 2D track textures handled")
	}
	f, err := t.dxgi()
	if err != nil {
		return nil, nil, err
	}
	bpe, block, err := formatElement(f)
	if err != nil {
		return nil, nil, err
	}
	var out bytes.Buffer
	var tile uint32
	for m := uint32(0); m < t.mips; m++ {
		w, h := mipDim(t.w, m), mipDim(t.h, m)
		desc := xgTex2D{Width: w, Height: h, MipLevels: t.mips - m, ArraySize: 1, Format: f, SampleCount: 1, BindFlags: xgBindShaderResource}
		desc.TileMode = optimalTileMode(f, w, h, 1, 0)
		if m == 0 {
			tile = desc.TileMode
		} else if desc.TileMode != tile {
			return nil, nil, fmt.Errorf("mip %d wants tile mode %d, mip 0 %d", m, desc.TileMode, tile)
		}
		c, err := create2D(&desc)
		if err != nil {
			return nil, nil, err
		}
		l, err := c.layout()
		if err != nil {
			c.release()
			return nil, nil, err
		}
		ml := l.Plane[0].MipLayout[0]
		buf := make([]byte, c.sizeBytes())
		row, slice := linearPitches(t, m, bpe, block)
		if t.mipSize[m] != slice || int(t.mipOff[m]+slice) > len(data) {
			c.release()
			return nil, nil, fmt.Errorf("mip %d: 0x%x bytes at 0x%x, expected 0x%x", m, t.mipSize[m], t.mipOff[m], slice)
		}
		err = c.copyInto(buf, 0, data[t.mipOff[m]:t.mipOff[m]+slice], row, slice)
		c.release()
		if err != nil {
			return nil, nil, err
		}
		out.Write(buf[ml.OffsetBytes : ml.OffsetBytes+ml.SizeBytes])
	}
	x := *t
	x.layW, x.layH, x.layD = lay.layW, lay.layH, 1
	x.d = 1
	x.layMips, x.first = lay.layMips, lay.first
	x.tile = tile
	_, _, al, err := xboxLayout(&texInfo{w: lay.layW, h: lay.layH, mips: lay.layMips, a: t.a, b: t.b, c: t.c, array: 1})
	if err != nil {
		return nil, nil, err
	}
	x.alignExp = uint32(bits.TrailingZeros64(al)) - 4 // see convertFull
	return x.xboxHeader(), out.Bytes(), nil
}

// pickLayout chooses the parent texture for a group bundle. Only the data size
// and bytes matter to the loader; the most common candidate fills the header.
func pickLayout(cand map[gkey]map[gval]int, t *texInfo) (gval, bool) {
	c := cand[gkey{t.w, t.h, t.mips, t.a, t.b, t.c}]
	best, n := gval{}, -1
	// ties are broken on every field so that two runs write the same bytes
	less := func(a, b gval) bool {
		if a.layW != b.layW {
			return a.layW < b.layW
		}
		if a.layH != b.layH {
			return a.layH < b.layH
		}
		if a.layMips != b.layMips {
			return a.layMips < b.layMips
		}
		return a.first < b.first
	}
	for v, k := range c {
		if k > n || (k == n && less(v, best)) {
			best, n = v, k
		}
	}
	return best, n > 0
}
