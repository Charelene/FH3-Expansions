package main

import (
	"fmt"
	"strconv"
)

// cmdTilerProbe prints XG's layout of one texture: probe <dxgi hex> <w> <h> <mips> [array] [cube].
func cmdTilerProbe(args []string) {
	f, _ := strconv.ParseUint(args[0], 0, 32)
	w, _ := strconv.Atoi(args[1])
	h, _ := strconv.Atoi(args[2])
	mips, _ := strconv.Atoi(args[3])
	arr := 1
	if len(args) > 4 {
		arr, _ = strconv.Atoi(args[4])
	}
	misc := uint32(0)
	if len(args) > 5 && args[5] == "cube" {
		misc = xgMiscTextureCube
	}
	d := xgTex2D{Width: uint32(w), Height: uint32(h), MipLevels: uint32(mips), ArraySize: uint32(arr), Format: uint32(f), SampleCount: 1, BindFlags: xgBindShaderResource, MiscFlags: misc}
	d.TileMode = optimalTileMode(uint32(f), uint32(w), uint32(h), uint32(arr), misc)
	c, err := create2D(&d)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer c.release()
	l, err := c.layout()
	if err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("fmt 0x%x %dx%d mips %d array %d tile %d size %d base align %d bpe %d\n", f, w, h, mips, arr, d.TileMode, l.SizeBytes, l.BaseAlignmentBytes, l.Plane[0].BytesPerElement)
	for i := 0; i < mips; i++ {
		m := l.Plane[0].MipLayout[i]
		fmt.Printf("  mip %2d off %9d size %9d pitchPix %5d pitchB %7d align %6d padded %5dx%-5d elems %4dx%-4d tile %d slice2D %d\n",
			i, m.OffsetBytes, m.SizeBytes, m.PitchPixels, m.PitchBytes, m.AlignmentBytes, m.PaddedWidthElements, m.PaddedHeightElements, m.WidthElements, m.HeightElements, m.TileMode, m.Slice2DSizeBytes)
	}
}

// cmdTileMap tiles numbered elements of one mip with XG and prints where each landed:
// tilemap <dxgi hex> <w> <h> <mips> <mip>
func cmdTileMap(args []string) {
	f, _ := strconv.ParseUint(args[0], 0, 32)
	w, _ := strconv.Atoi(args[1])
	h, _ := strconv.Atoi(args[2])
	mips, _ := strconv.Atoi(args[3])
	m, _ := strconv.Atoi(args[4])
	d := xgTex2D{Width: uint32(w), Height: uint32(h), MipLevels: uint32(mips), ArraySize: 1, Format: uint32(f), SampleCount: 1, BindFlags: xgBindShaderResource}
	d.TileMode = optimalTileMode(uint32(f), uint32(w), uint32(h), 1, 0)
	c, err := create2DXG(&d)
	if err != nil {
		fmt.Println(err)
		return
	}
	defer c.release()
	l, _ := c.layout()
	ml := l.Plane[0].MipLayout[m]
	bpe, _, _ := formatElement(uint32(f))
	we, he := int(ml.WidthElements), int(ml.HeightElements)
	src := make([]byte, we*he*int(bpe))
	for i := 0; i < we*he; i++ {
		for b := 0; b < int(bpe); b++ {
			src[i*int(bpe)+b] = byte(i + 1)
		}
	}
	out := make([]byte, c.sizeBytes())
	if err := c.copyInto(out, uint32(m), src, uint32(we)*bpe, uint32(len(src))); err != nil {
		fmt.Println(err)
		return
	}
	fmt.Printf("mip %d: %dx%d elements, padded %dx%d, offset %d size %d\n", m, we, he, ml.PaddedWidthElements, ml.PaddedHeightElements, ml.OffsetBytes, ml.SizeBytes)
	for i := uint64(0); i < ml.SizeBytes; i += uint64(bpe) {
		v := out[ml.OffsetBytes+i]
		if v != 0 {
			e := int(v) - 1
			fmt.Printf("  byte %4d <- element (%d,%d)\n", i, e%we, e/we)
		}
	}
}
