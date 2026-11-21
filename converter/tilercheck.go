package main

import (
	"fmt"
	"math/rand"
)

// cmdTilerCheck tiles random data with the GDK's xg.dll and with the own tiler
// and compares layout and bytes. XG must be loaded (loadXG).
func cmdTilerCheck(count int, seed int64) {
	rng := rand.New(rand.NewSource(seed))
	formats := []uint32{0x1c, 0x1d, 0x47, 0x48, 0x50, 0x51, 0x53, 0x54, 0x62, 0x63, 0x4a, 0x4d, 0x4e, 0x5f, 0x60, 0x02, 0x0a, 0x0b, 0x55, 0x56, 0x31, 0x3d, 0x41}
	sizes := []uint32{1, 2, 3, 4, 5, 7, 8, 9, 15, 16, 17, 31, 32, 33, 60, 63, 64, 100, 127, 128, 129, 150, 255, 256, 257, 300, 511, 512, 600, 1000, 1024, 1500, 2048, 3000}
	arrays := []uint32{1, 1, 1, 1, 2, 3, 4, 6, 6, 8}
	bad, layoutBad, tileBad := 0, 0, 0
	bytesDone := uint64(0)
	for n := 0; n < count; n++ {
		f := formats[rng.Intn(len(formats))]
		w, h := sizes[rng.Intn(len(sizes))], sizes[rng.Intn(len(sizes))]
		maxMips := uint32(1)
		for v := max32(w, h); v > 1; v >>= 1 {
			maxMips++
		}
		mips := uint32(1 + rng.Intn(int(maxMips)))
		if rng.Intn(3) == 0 {
			mips = maxMips
		}
		arr := arrays[rng.Intn(len(arrays))]
		misc := uint32(0)
		if arr == 6 && rng.Intn(2) == 0 {
			misc = xgMiscTextureCube
		}
		if w*h > 2048*1024 {
			continue
		}
		desc := xgTex2D{Width: w, Height: h, MipLevels: mips, ArraySize: arr, Format: f, SampleCount: 1, BindFlags: xgBindShaderResource, MiscFlags: misc}
		desc.TileMode = optimalTileMode(f, w, h, arr, misc)
		if desc.TileMode != 13 {
			fmt.Printf("fmt 0x%x %dx%d mips %d array %d: XG wants tile mode %d\n", f, w, h, mips, arr, desc.TileMode)
			bad++
			continue
		}
		xc, err := create2DXG(&desc)
		if err != nil {
			fmt.Printf("fmt 0x%x %dx%d mips %d array %d: XG: %v\n", f, w, h, mips, arr, err)
			bad++
			continue
		}
		ol, err := newOwnLayout(f, w, h, mips, arr)
		if err != nil {
			fmt.Printf("fmt 0x%x %dx%d: own: %v\n", f, w, h, err)
			bad++
			xc.release()
			continue
		}
		xl, _ := xc.layout()
		desc2 := fmt.Sprintf("fmt 0x%02x %4dx%-4d mips %2d array %d cube %v", f, w, h, mips, arr, misc != 0)
		ok := xc.sizeBytes() == ol.size && xc.baseAlignment() == 256
		for i := uint32(0); i < mips && ok; i++ {
			a, b := xl.Plane[0].MipLayout[i], ol.resourceLayout().Plane[0].MipLayout[i]
			if a.OffsetBytes != b.OffsetBytes || a.SizeBytes != b.SizeBytes || a.Slice2DSizeBytes != b.Slice2DSizeBytes {
				fmt.Printf("%s: mip %d XG off %d size %d slice %d, own off %d size %d slice %d\n", desc2, i, a.OffsetBytes, a.SizeBytes, a.Slice2DSizeBytes, b.OffsetBytes, b.SizeBytes, b.Slice2DSizeBytes)
				ok = false
			}
		}
		if !ok {
			if xc.sizeBytes() != ol.size || xc.baseAlignment() != 256 {
				fmt.Printf("%s: XG size %d align %d, own size %d\n", desc2, xc.sizeBytes(), xc.baseAlignment(), ol.size)
			}
			layoutBad++
			xc.release()
			continue
		}
		xb := make([]byte, xc.sizeBytes())
		ob := make([]byte, ol.size)
		for s := uint32(0); s < arr; s++ {
			for m := uint32(0); m < mips; m++ {
				mm := ol.m[m]
				row := mm.we * ol.bpe
				src := make([]byte, row*mm.he)
				rng.Read(src)
				if err := xc.copyInto(xb, s*mips+m, src, row, row*mm.he); err != nil {
					fmt.Printf("%s: XG copy: %v\n", desc2, err)
					ok = false
					break
				}
				if err := ol.tileInto(ob, m, s, src, row); err != nil {
					fmt.Printf("%s: own copy: %v\n", desc2, err)
					ok = false
					break
				}
			}
		}
		if ok {
			bytesDone += uint64(len(xb))
			for i := range xb {
				if xb[i] != ob[i] {
					fmt.Printf("%s: bytes differ at %d (XG %02x, own %02x)\n", desc2, i, xb[i], ob[i])
					ok = false
					break
				}
			}
		}
		if !ok {
			tileBad++
		}
		xc.release()
	}
	fmt.Printf("checked %d textures (%d MB tiled): %d XG refusals or other, %d layout differences, %d byte differences\n", count, bytesDone>>20, bad, layoutBad, tileBad)
}

func max32(a, b uint32) uint32 {
	if a > b {
		return a
	}
	return b
}
