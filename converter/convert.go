package main

import (
	"bytes"
	"errors"
	"fmt"
	"math/bits"
)

// DXGI formats by the game's format index a (table at 0x144645b74, 12 bytes per
// row: linear, sRGB, next index). Index b >= 2 uses the table at 0x144645ae4,
// which is the same list shifted by two; all UWP content seen uses b = 0.
var formatByIndex = [][2]uint32{
	{0x47, 0x48}, {0x4a, 0x4b}, {0x4d, 0x4e}, {0x50, 0x50}, {0x51, 0x51},
	{0x53, 0x53}, {0x54, 0x54}, {0x5f, 0}, {0x60, 0}, {0x62, 0x63},
	{0x02, 0}, {0x0b, 0}, {0x0a, 0}, {0x1c, 0x1d}, {0x55, 0}, {0x56, 0},
	{0, 0}, {0, 0}, {0x3d, 0}, {0x41, 0}, {0x31, 0}, {0x62, 0x63},
}

// element size in bytes and block edge (4 for block-compressed formats).
func formatElement(dxgi uint32) (bytesPer, block uint32, err error) {
	switch dxgi {
	case 0x47, 0x48, 0x50, 0x51: // BC1, BC4
		return 8, 4, nil
	case 0x4a, 0x4b, 0x4d, 0x4e, 0x53, 0x54, 0x5f, 0x60, 0x62, 0x63: // BC2, BC3, BC5, BC6H, BC7
		return 16, 4, nil
	case 0x02: // R32G32B32A32_FLOAT
		return 16, 1, nil
	case 0x0a, 0x0b: // R16G16B16A16
		return 8, 1, nil
	case 0x1c, 0x1d: // R8G8B8A8
		return 4, 1, nil
	case 0x55, 0x56, 0x31: // B5G6R5, B5G5R5A1, R8G8
		return 2, 1, nil
	case 0x3d, 0x41: // R8, A8
		return 1, 1, nil
	}
	return 0, 0, fmt.Errorf("no element size for DXGI format 0x%x", dxgi)
}

func (t *texInfo) dxgi() (uint32, error) {
	if t.b >= 2 {
		return 0, fmt.Errorf("format index b=%d not handled", t.b)
	}
	if int(t.a) >= len(formatByIndex) {
		return 0, fmt.Errorf("format index a=%d out of range", t.a)
	}
	f := formatByIndex[t.a][0]
	if t.c == 1 && formatByIndex[t.a][1] != 0 {
		f = formatByIndex[t.a][1]
	}
	if f == 0 {
		return 0, fmt.Errorf("format index a=%d has no DXGI format", t.a)
	}
	return f, nil
}

func mipDim(v, m uint32) uint32 {
	v >>= m
	if v == 0 {
		v = 1
	}
	return v
}

// linearPitches of mip m in the UWP data (tightly packed rows).
func linearPitches(t *texInfo, m, bpe, block uint32) (row, slice uint32) {
	w := (mipDim(t.w, m) + block - 1) / block
	h := (mipDim(t.h, m) + block - 1) / block
	return w * bpe, w * bpe * h
}

// convertFull converts a UWP texture that holds its whole mip chain.
// It returns the Xbox header payload and the tiled data.
func convertFull(t *texInfo, data []byte) ([]byte, []byte, error) {
	if t.volume || t.d > 1 {
		return nil, nil, errors.New("volume textures not handled")
	}
	f, err := t.dxgi()
	if err != nil {
		return nil, nil, err
	}
	bpe, block, err := formatElement(f)
	if err != nil {
		return nil, nil, err
	}
	faces := t.array
	if faces == 0 {
		faces = 1
	}
	misc := uint32(0)
	if t.cube {
		misc = xgMiscTextureCube
		if faces%6 != 0 {
			return nil, nil, fmt.Errorf("cube with %d faces", faces)
		}
	}
	desc := xgTex2D{Width: t.w, Height: t.h, MipLevels: t.mips, ArraySize: faces, Format: f,
		SampleCount: 1, BindFlags: xgBindShaderResource, MiscFlags: misc}
	desc.TileMode = optimalTileMode(f, t.w, t.h, faces, misc)
	c, err := create2D(&desc)
	if err != nil {
		return nil, nil, err
	}
	defer c.release()
	out := make([]byte, c.sizeBytes())
	// UWP subresource records run slice by slice, mips inside (D3D order).
	if len(t.mipSize) != int(t.mips*faces) {
		return nil, nil, errors.New("mip records do not match mips x slices")
	}
	for face := uint32(0); face < faces; face++ {
		for m := uint32(0); m < t.mips; m++ {
			row, slice := linearPitches(t, m, bpe, block)
			ri := face*t.mips + m
			if t.mipSize[ri] != slice {
				return nil, nil, fmt.Errorf("subresource %d is 0x%x bytes, expected 0x%x", ri, t.mipSize[ri], slice)
			}
			off := t.mipOff[ri]
			if int(off+slice) > len(data) {
				return nil, nil, fmt.Errorf("mip %d face %d outside the data", m, face)
			}
			sub := face*t.mips + m
			if err := c.copyInto(out, sub, data[off:off+slice], row, slice); err != nil {
				return nil, nil, fmt.Errorf("mip %d: %w", m, err)
			}
		}
	}
	x := *t
	x.layW, x.layH, x.layD = t.w, t.h, faces
	x.d = faces
	x.layMips, x.first = t.mips, 0
	x.tile = desc.TileMode
	// XG's base alignment as 1 << (e+4). This matches the console files for
	// flat textures; for some (a cube, partial track groups) the cooker wrote
	// one less. The loader uses max(256, 1 << (e+4)), so both give 256.
	al := c.baseAlignment()
	if al < 16 || al&(al-1) != 0 {
		return nil, nil, fmt.Errorf("odd base alignment %d", al)
	}
	x.alignExp = uint32(bits.TrailingZeros64(al)) - 4
	return x.xboxHeader(), out, nil
}

// cmdCompareZip converts the UWP texture and compares it with an Xbox copy.
func cmdCompareZip(pcZip, xbZip, suffix string) error {
	pcB, _, err := zipEntry(pcZip, suffix)
	if err != nil {
		return err
	}
	xbB, _, err := zipEntry(xbZip, suffix)
	if err != nil {
		return err
	}
	pb, err := parseBundle(pcB)
	if err != nil {
		return err
	}
	xb, err := parseBundle(xbB)
	if err != nil {
		return err
	}
	pt, err := parsePCHeader(pb.blobs[0].metas[0].payload)
	if err != nil {
		return err
	}
	hdr, data, err := convertFull(pt, pb.blobs[0].data)
	if err != nil {
		return err
	}
	xt, _ := parseXboxHeader(hdr)
	fmt.Println("converted:", xt)
	ot, _ := parseXboxHeader(xb.blobs[0].metas[0].payload)
	fmt.Println("xbox     :", ot)
	fmt.Printf("header equal: %v\n  conv % x\n  xbox % x\n", bytes.Equal(hdr, xb.blobs[0].metas[0].payload), hdr, xb.blobs[0].metas[0].payload)
	od := xb.blobs[0].data
	fmt.Printf("data sizes: converted 0x%x xbox 0x%x\n", len(data), len(od))
	n := len(data)
	if len(od) < n {
		n = len(od)
	}
	diff := 0
	first := -1
	for i := 0; i < n; i++ {
		if data[i] != od[i] {
			diff++
			if first < 0 {
				first = i
			}
		}
	}
	fmt.Printf("data equal: %v (differing bytes %d, first at 0x%x)\n", bytes.Equal(data, od), diff, first)
	return nil
}
