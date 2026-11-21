package main

import (
	"bytes"
	"fmt"
	"math/rand"
)

// cmdIndep checks whether mip m of a full chain tiles identically to mip 0 of a
// standalone texture of that size, and whether mips are packed without gaps.
func cmdIndep() {
	for _, f := range []uint32{0x62, 0x47, 0x4d, 0x1c} {
		for _, wh := range [][2]uint32{{1024, 1024}, {512, 1024}, {2048, 512}, {256, 256}} {
			full := xgTex2D{Width: wh[0], Height: wh[1], ArraySize: 1, Format: f, SampleCount: 1, BindFlags: xgBindShaderResource}
			n := uint32(1)
			for (wh[0]>>n) > 0 || (wh[1]>>n) > 0 {
				n++
			}
			full.MipLevels = n
			full.TileMode = optimalTileMode(f, wh[0], wh[1], 1, 0)
			cf, err := create2D(&full)
			if err != nil {
				fmt.Println(err)
				continue
			}
			lf, _ := cf.layout()
			bpe, block, _ := formatElement(f)
			t := &texInfo{w: wh[0], h: wh[1]}
			res := ""
			for m := uint32(0); m < n; m++ {
				row, slice := linearPitches(t, m, bpe, block)
				lin := make([]byte, slice)
				rand.Read(lin)
				buf := make([]byte, cf.sizeBytes())
				cf.copyInto(buf, m, lin, row, slice)
				ml := lf.Plane[0].MipLayout[m]
				// standalone texture of this mip's size with the rest of its chain
				sw, sh := mipDim(wh[0], m), mipDim(wh[1], m)
				sd := xgTex2D{Width: sw, Height: sh, MipLevels: n - m, ArraySize: 1, Format: f, SampleCount: 1, BindFlags: xgBindShaderResource, TileMode: full.TileMode}
				cs, err := create2D(&sd)
				if err != nil {
					res += fmt.Sprintf(" m%d:err", m)
					continue
				}
				ls, _ := cs.layout()
				sbuf := make([]byte, cs.sizeBytes())
				t2 := &texInfo{w: sw, h: sh}
				r2, s2 := linearPitches(t2, 0, bpe, block)
				cs.copyInto(sbuf, 0, lin, r2, s2)
				m0 := ls.Plane[0].MipLayout[0]
				same := ml.SizeBytes == m0.SizeBytes && bytes.Equal(buf[ml.OffsetBytes:ml.OffsetBytes+ml.SizeBytes], sbuf[m0.OffsetBytes:m0.OffsetBytes+m0.SizeBytes])
				gap := uint64(0)
				if m+1 < n {
					gap = lf.Plane[0].MipLayout[m+1].OffsetBytes - (ml.OffsetBytes + ml.SizeBytes)
				}
				res += fmt.Sprintf(" m%d:%v/tm%d/gap%d", m, same, ml.TileMode, gap)
				cs.release()
			}
			fmt.Printf("fmt 0x%x %dx%d tile %d size 0x%x:%s\n", f, wh[0], wh[1], full.TileMode, cf.sizeBytes(), res)
			cf.release()
		}
	}
}
