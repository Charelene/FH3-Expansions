package main

import "bytes"

// detileCompare untiles the Xbox data with the converted layout and reports,
// per mip, whether it equals the UWP linear data. Equal everywhere means the
// difference is padding; unequal means the pixels themselves differ.
func detileCompare(t *texInfo, pcData, xbData []byte) (equalMips, mips int) {
	f, err := t.dxgi()
	if err != nil {
		return 0, 0
	}
	bpe, block, _ := formatElement(f)
	desc := xgTex2D{Width: t.w, Height: t.h, MipLevels: t.mips, ArraySize: 1, Format: f, SampleCount: 1, BindFlags: xgBindShaderResource}
	desc.TileMode = optimalTileMode(f, t.w, t.h, 1, 0)
	c, err := create2D(&desc)
	if err != nil {
		return 0, 0
	}
	defer c.release()
	if uint64(len(xbData)) < c.sizeBytes() {
		return 0, int(t.mips)
	}
	for m := uint32(0); m < t.mips; m++ {
		row, slice := linearPitches(t, m, bpe, block)
		lin := make([]byte, slice)
		if c.copyFrom(lin, m, xbData, row, slice) != nil {
			continue
		}
		if bytes.Equal(lin, pcData[t.mipOff[m]:t.mipOff[m]+slice]) {
			equalMips++
		}
	}
	return equalMips, int(t.mips)
}
