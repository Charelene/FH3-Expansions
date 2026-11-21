package main

// Our own implementation of the console texture layout the converter needs, so
// that no file of the Xbox GDK has to travel with it.
//
// Every texture of the expansions uses XG tile mode 13 (1D_THIN, GCN thin
// micro tiles of 8 x 8 elements). An element is a pixel, or a 4 x 4 block of a
// block-compressed format. Layout, as measured against the GDK's xg.dll (see
// cmdTilerCheck, which compares both bit for bit):
//
//   - mip m has element dimensions ceil(max(1, w >> m) / block) by the same
//     for height;
//   - a texture with more than one mip pads the width of its base level to the
//     next power of two (in elements) and halves that for every mip, and pads
//     the height of each mip to the next power of two, always at least 8; a
//     single-mip texture pads width and height to a multiple of 8;
//   - the size of a surface is a multiple of 256 bytes; when it is not, the
//     pitch grows by 8 elements until it is;
//   - the padded surface is a grid of micro tiles, row after row, each tile
//     64 elements in the order x0 y0 x1 y1 x2 y2 (Morton) of the position in
//     the tile;
//   - mips follow each other, the next one starting where the last one ends;
//   - an array or cube holds all slices of a mip together, the slice count
//     rounded up to a power of two; slice s of mip m is at the mip's offset
//     plus s * the size of one padded slice;
//   - base alignment 256.

import (
	"errors"
	"fmt"
)

type ownMip struct {
	offset, size, slice uint64
	we, he, pw, ph      uint32 // element and padded element dimensions
}

type ownLayout struct {
	bpe, block        uint32
	mips, arr, slices uint32
	m                 []ownMip
	size              uint64
}

func nextPow2(v uint32) uint32 {
	p := uint32(1)
	for p < v {
		p <<= 1
	}
	return p
}

func align8(v uint32) uint32 { return (v + 7) &^ 7 }

// ownSupported reports whether the layout code covers a DXGI format.
func ownSupported(f uint32) bool {
	_, _, err := formatElement(f)
	return err == nil
}

func newOwnLayout(f, w, h, mips, arr uint32) (*ownLayout, error) {
	bpe, block, err := formatElement(f)
	if err != nil {
		return nil, err
	}
	if mips == 0 || mips > 15 || arr == 0 {
		return nil, fmt.Errorf("%d mips, %d slices", mips, arr)
	}
	l := &ownLayout{bpe: bpe, block: block, mips: mips, arr: arr, slices: arr}
	if mips > 1 {
		l.slices = nextPow2(arr)
	}
	var off uint64
	for i := uint32(0); i < mips; i++ {
		we := (mipDim(w, i) + block - 1) / block
		he := (mipDim(h, i) + block - 1) / block
		pw, ph := align8(we), align8(he)
		if mips > 1 {
			// the pitch is the base level's, to a power of two, halved with
			// every mip; the height is each mip's own, to a power of two
			w0 := (w + block - 1) / block
			pw, ph = max32(8, nextPow2(w0)>>i), max32(8, nextPow2(he))
		}
		// the size of a surface is a multiple of 256 bytes: the pitch grows by
		// 8 elements until it is
		for uint64(pw)*uint64(ph)*uint64(bpe)%256 != 0 {
			pw += 8
		}
		slice := uint64(pw) * uint64(ph) * uint64(bpe)
		mm := ownMip{offset: off, slice: slice, size: slice * uint64(l.slices), we: we, he: he, pw: pw, ph: ph}
		l.m = append(l.m, mm)
		off += mm.size
	}
	l.size = off
	return l, nil
}

// mortonIdx is the position of element (x, y) inside a thin micro tile.
func mortonIdx(x, y uint32) uint32 {
	return x&1 | (y&1)<<1 | (x>>1&1)<<2 | (y>>1&1)<<3 | (x>>2&1)<<4 | (y>>2&1)<<5
}

var mortonTable [64]uint32 // mortonTable[y*8+x]

func init() {
	for y := uint32(0); y < 8; y++ {
		for x := uint32(0); x < 8; x++ {
			mortonTable[y*8+x] = mortonIdx(x, y)
		}
	}
}

// tileInto tiles one subresource (mip m, slice s) from linear rows of
// rowPitch bytes into dst, which is the whole texture.
func (l *ownLayout) tileInto(dst []byte, m, s uint32, src []byte, rowPitch uint32) error {
	if m >= l.mips || s >= l.arr {
		return errors.New("subresource out of range")
	}
	mm := l.m[m]
	base := mm.offset + uint64(s)*mm.slice
	if base+mm.slice > uint64(len(dst)) {
		return errors.New("destination too small")
	}
	bpe := l.bpe
	if uint64(rowPitch) < uint64(mm.we)*uint64(bpe) {
		return errors.New("row pitch smaller than a row")
	}
	if uint64(mm.he-1)*uint64(rowPitch)+uint64(mm.we)*uint64(bpe) > uint64(len(src)) {
		return errors.New("source too small")
	}
	tilesPerRow := mm.pw / 8
	for y := uint32(0); y < mm.he; y++ {
		row := src[uint64(y)*uint64(rowPitch):]
		ty, iy := y>>3, y&7
		for x := uint32(0); x < mm.we; x++ {
			tile := uint64(ty*tilesPerRow + x>>3)
			o := base + tile*64*uint64(bpe) + uint64(mortonTable[iy*8+x&7])*uint64(bpe)
			copy(dst[o:o+uint64(bpe)], row[uint64(x)*uint64(bpe):uint64(x+1)*uint64(bpe)])
		}
	}
	return nil
}

// resourceLayout fills the part of XG's structure the converter reads.
func (l *ownLayout) resourceLayout() *xgResourceLayout {
	r := new(xgResourceLayout)
	r.SizeBytes = l.size
	r.BaseAlignmentBytes = 256
	r.MipLevels, r.Planes = l.mips, 1
	p := &r.Plane[0]
	p.SizeBytes = l.size
	p.BytesPerElement = l.bpe
	p.BaseAlignmentBytes = 256
	for i, mm := range l.m {
		p.MipLayout[i] = xgMipLayout{
			SizeBytes: mm.size, OffsetBytes: mm.offset, Slice2DSizeBytes: mm.slice,
			PitchPixels: mm.pw * l.block, PitchBytes: mm.pw * l.bpe, AlignmentBytes: 256,
			PaddedWidthElements: mm.pw, PaddedHeightElements: mm.ph, PaddedDepth: 1,
			WidthElements: mm.we, HeightElements: mm.he, DepthOrArraySize: l.arr,
			SampleCount: 1, TileMode: 13,
		}
	}
	return r
}
