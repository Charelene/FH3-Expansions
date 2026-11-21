package main

// Texture metadata ("HCXT") of a BCXT blob.
//
// Xbox One (BCXT 2.0), 0x23 bytes, read by 0x141a78160 / 0x141a84b80:
//   +0x00 GUID, +0x10 u16 width, +0x12 u16 height, +0x14 u16 depth or array size,
//   +0x16/+0x18/+0x1a u16 layout width/height/depth, +0x1c u8 mips in this blob,
//   +0x1d u8 mips of the layout, +0x1e u8 first mip of this blob in the layout,
//   +0x1f u32: bits 0-4 XG tile mode, 5-10 format index a, 11-16 format index b,
//              20-22 c (1 = sRGB), 25 cube, 26 volume, 27 flag (same as UWP P+0x27 bit 1),
//              28-31 alignment exponent (alignment = 1 << (e+4))
//
// UWP (BCXT 0.0), read by 0x141a84d60 with param_3 = P+8. A serialized struct
// with two fixup chains (pointer fields relative to P, data offsets relative
// to the blob data); each pointer field is followed by the next link (-1 ends).
//   P+0x00 first payload-relative pointer field (0x38), P+0x04 first data-relative field
//   P+0x08 GUID, +0x18 u32 width, +0x1c u32 height, +0x20 u32 depth,
//   +0x24 u16 array size (low 14 bits), +0x26 u8 mips, +0x27 flags (bit 0 cube, bit 1 flag),
//   +0x28 u32 b, +0x2c u32 ?, +0x30 u32 c, +0x34 u32 ?,
//   +0x38 ptr -> {u32 a}, link; +0x44 ptr -> mip list, link
//   mip records {u32 size, u32 data offset, u32 next link}

import (
	"errors"
	"fmt"
)

type texInfo struct {
	guid                 [16]byte
	w, h, d              uint32
	array                uint32
	mips                 uint32
	layW, layH, layD     uint32
	layMips, first       uint32
	a, b, c              uint32
	cube, volume, flag27 bool
	tile                 uint32
	alignExp             uint32
	pcUnk2c, pcUnk34     uint32
	mipSize, mipOff      []uint32 // UWP only
	raw                  []byte
}

func parsePCHeader(p []byte) (*texInfo, error) {
	if len(p) < 0x4c {
		return nil, errors.New("UWP texture header too short")
	}
	t := &texInfo{raw: p}
	copy(t.guid[:], p[8:24])
	t.w, t.h, t.d = le.Uint32(p[0x18:]), le.Uint32(p[0x1c:]), le.Uint32(p[0x20:])
	t.array = uint32(le.Uint16(p[0x24:]) & 0x3fff)
	t.mips = uint32(p[0x26])
	fl := p[0x27]
	t.cube, t.flag27 = fl&1 != 0, fl&2 != 0
	if fl&^3 != 0 {
		return nil, fmt.Errorf("unknown UWP flag bits 0x%02x", fl)
	}
	t.b, t.pcUnk2c, t.c, t.pcUnk34 = le.Uint32(p[0x28:]), le.Uint32(p[0x2c:]), le.Uint32(p[0x30:]), le.Uint32(p[0x34:])
	fp := le.Uint32(p[0x38:])
	if int(fp)+4 > len(p) {
		return nil, errors.New("format pointer outside header")
	}
	t.a = le.Uint32(p[fp:])
	mp := int(le.Uint32(p[0x44:]))
	slices := t.array
	if slices == 0 {
		slices = 1
	}
	for k := uint32(0); k < t.mips*slices; k++ {
		if mp+12 > len(p) {
			return nil, errors.New("mip record outside header")
		}
		t.mipSize = append(t.mipSize, le.Uint32(p[mp:]))
		t.mipOff = append(t.mipOff, le.Uint32(p[mp+4:]))
		mp += 12
	}
	return t, nil
}

func parseXboxHeader(p []byte) (*texInfo, error) {
	if len(p) != 0x23 {
		return nil, fmt.Errorf("Xbox texture header is 0x%x bytes, want 0x23", len(p))
	}
	t := &texInfo{raw: p}
	copy(t.guid[:], p[:16])
	t.w, t.h, t.d = uint32(le.Uint16(p[0x10:])), uint32(le.Uint16(p[0x12:])), uint32(le.Uint16(p[0x14:]))
	t.layW, t.layH, t.layD = uint32(le.Uint16(p[0x16:])), uint32(le.Uint16(p[0x18:])), uint32(le.Uint16(p[0x1a:]))
	t.mips, t.layMips, t.first = uint32(p[0x1c]), uint32(p[0x1d]), uint32(p[0x1e])
	d := le.Uint32(p[0x1f:])
	t.tile, t.a, t.b, t.c = d&0x1f, d>>5&0x3f, d>>11&0x3f, d>>20&7
	t.cube, t.volume, t.flag27 = d>>25&1 != 0, d>>26&1 != 0, d>>27&1 != 0
	t.pcUnk2c, t.pcUnk34 = d>>17&7, d>>23&3
	t.alignExp = d >> 28
	return t, nil
}

func (t *texInfo) xboxHeader() []byte {
	p := make([]byte, 0x23)
	copy(p, t.guid[:])
	le.PutUint16(p[0x10:], uint16(t.w))
	le.PutUint16(p[0x12:], uint16(t.h))
	le.PutUint16(p[0x14:], uint16(t.d))
	le.PutUint16(p[0x16:], uint16(t.layW))
	le.PutUint16(p[0x18:], uint16(t.layH))
	le.PutUint16(p[0x1a:], uint16(t.layD))
	p[0x1c], p[0x1d], p[0x1e] = byte(t.mips), byte(t.layMips), byte(t.first)
	d := t.tile&0x1f | (t.a&0x3f)<<5 | (t.b&0x3f)<<11 | (t.pcUnk2c&7)<<17 | (t.c&7)<<20 | (t.pcUnk34&3)<<23 | t.alignExp<<28
	if t.cube {
		d |= 1 << 25
	}
	if t.volume {
		d |= 1 << 26
	}
	if t.flag27 {
		d |= 1 << 27
	}
	le.PutUint32(p[0x1f:], d)
	return p
}

func (t *texInfo) String() string {
	return fmt.Sprintf("%dx%dx%d array %d mips %d (layout %dx%dx%d mips %d first %d) fmt(a%d b%d c%d) tile %d alignExp %d cube %v vol %v f27 %v unk2c %d unk34 %d mipSizes %v",
		t.w, t.h, t.d, t.array, t.mips, t.layW, t.layH, t.layD, t.layMips, t.first, t.a, t.b, t.c, t.tile, t.alignExp, t.cube, t.volume, t.flag27, t.pcUnk2c, t.pcUnk34, t.mipSize)
}
