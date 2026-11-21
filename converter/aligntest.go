package main

import "fmt"

func cmdAlignTest() {
	type tc struct {
		name               string
		f, w, h, mips, arr uint32
		misc               uint32
	}
	for _, c := range []tc{
		{"BC1 1024 1 mip", 0x47, 1024, 1024, 1, 1, 0},
		{"BC1 1024 11 mips", 0x47, 1024, 1024, 11, 1, 0},
		{"BC4 1024 11 mips", 0x50, 1024, 1024, 11, 1, 0},
		{"BC7 1024 11 mips", 0x62, 1024, 1024, 11, 1, 0},
		{"RGBA16F 64 cube 1 mip", 0x0a, 64, 64, 1, 6, xgMiscTextureCube},
		{"RGBA16F 64 1 mip", 0x0a, 64, 64, 1, 1, 0},
		{"BC7 64x512 2 mips", 0x62, 64, 512, 2, 1, 0},
	} {
		d := xgTex2D{Width: c.w, Height: c.h, MipLevels: c.mips, ArraySize: c.arr, Format: c.f, SampleCount: 1, BindFlags: xgBindShaderResource, MiscFlags: c.misc}
		d.TileMode = optimalTileMode(c.f, c.w, c.h, c.arr, c.misc)
		x, err := create2D(&d)
		if err != nil {
			fmt.Println(c.name, err)
			continue
		}
		l, _ := x.layout()
		fmt.Printf("%-24s tile %d base %d planeBase %d mip0 align %d size 0x%x\n", c.name, d.TileMode, x.baseAlignment(), l.Plane[0].BaseAlignmentBytes, l.Plane[0].MipLayout[0].AlignmentBytes, x.sizeBytes())
		x.release()
	}
}
