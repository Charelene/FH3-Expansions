package main

// thumbs: the game's stock car thumbnails as PNG files, for the server's own
// Auction House listings (a listing made by a player carries pictures the game
// rendered; one made by the server has none).
//
//	uwp2xbox thumbs <Mount/media/stripped> <out dir> <car id>...
//
// A car's pictures are Thumbnail_<id>_Small.swatchbin and
// Thumbnail_<id>_Big.swatchbin in one of the RC*.zip archives: a burG bundle
// whose first blob is an Xbox-tiled texture. Output: <id>_small.png and
// <id>_large.png. The output is game art: it stays on the owner's machines.

import (
	"archive/zip"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"os"
	"path/filepath"
	"strings"
)

// rgb565 expands a 5:6:5 colour.
func rgb565(v uint16) (r, g, b uint8) {
	r5, g6, b5 := uint8(v>>11&31), uint8(v>>5&63), uint8(v&31)
	return r5<<3 | r5>>2, g6<<2 | g6>>4, b5<<3 | b5>>2
}

// bcColors is the four-colour palette of a BC1-style colour block. opaque
// forces the four-colour mode (BC2 and BC3 colour blocks).
func bcColors(blk []byte, opaque bool) (pal [4]color.NRGBA) {
	c0, c1 := le.Uint16(blk), le.Uint16(blk[2:])
	r0, g0, b0 := rgb565(c0)
	r1, g1, b1 := rgb565(c1)
	pal[0], pal[1] = color.NRGBA{r0, g0, b0, 255}, color.NRGBA{r1, g1, b1, 255}
	mix := func(a, b uint8, wa, wb, div int) uint8 { return uint8((int(a)*wa + int(b)*wb) / div) }
	if c0 > c1 || opaque {
		pal[2] = color.NRGBA{mix(r0, r1, 2, 1, 3), mix(g0, g1, 2, 1, 3), mix(b0, b1, 2, 1, 3), 255}
		pal[3] = color.NRGBA{mix(r0, r1, 1, 2, 3), mix(g0, g1, 1, 2, 3), mix(b0, b1, 1, 2, 3), 255}
	} else {
		pal[2] = color.NRGBA{mix(r0, r1, 1, 1, 2), mix(g0, g1, 1, 1, 2), mix(b0, b1, 1, 1, 2), 255}
		pal[3] = color.NRGBA{}
	}
	return pal
}

// bcAlpha is the eight-value palette of a BC3 alpha block (also BC4).
func bcAlpha(blk []byte) (pal [8]uint8) {
	a0, a1 := int(blk[0]), int(blk[1])
	pal[0], pal[1] = uint8(a0), uint8(a1)
	if a0 > a1 {
		for i := 1; i <= 6; i++ {
			pal[1+i] = uint8(((7-i)*a0 + i*a1) / 7)
		}
	} else {
		for i := 1; i <= 4; i++ {
			pal[1+i] = uint8(((5-i)*a0 + i*a1) / 5)
		}
		pal[6], pal[7] = 0, 255
	}
	return pal
}

// decodeBC turns linear block-compressed data (rows of 4x4 blocks) into an
// image. Formats: BC1, BC2, BC3 and plain R8G8B8A8.
func decodeBC(dxgi uint32, w, h int, data []byte, rowPitch uint32) (*image.NRGBA, error) {
	img := image.NewNRGBA(image.Rect(0, 0, w, h))
	switch dxgi {
	case 0x1c, 0x1d:
		for y := 0; y < h; y++ {
			copy(img.Pix[y*img.Stride:y*img.Stride+w*4], data[y*int(rowPitch):])
		}
		return img, nil
	case 0x62, 0x63: // BC7
		for by := 0; by < (h+3)/4; by++ {
			for bx := 0; bx < (w+3)/4; bx++ {
				off := by*int(rowPitch) + bx*16
				if off+16 > len(data) {
					return nil, errors.New("texture data too short")
				}
				px := decodeBC7Block(data[off : off+16])
				for i := 0; i < 16; i++ {
					if x, y := bx*4+i%4, by*4+i/4; x < w && y < h {
						img.SetNRGBA(x, y, px[i])
					}
				}
			}
		}
		return img, nil
	case 0x47, 0x48, 0x4a, 0x4b, 0x4d, 0x4e:
	default:
		return nil, fmt.Errorf("no decoder for DXGI format 0x%x", dxgi)
	}
	size := 16
	if dxgi == 0x47 || dxgi == 0x48 {
		size = 8
	}
	for by := 0; by < (h+3)/4; by++ {
		for bx := 0; bx < (w+3)/4; bx++ {
			off := by*int(rowPitch) + bx*size
			if off+size > len(data) {
				return nil, errors.New("texture data too short")
			}
			blk := data[off : off+size]
			col := blk
			var alpha [16]uint8
			for i := range alpha {
				alpha[i] = 255
			}
			switch dxgi {
			case 0x4a, 0x4b: // BC2: 4 bits of alpha per pixel
				col = blk[8:]
				for i := 0; i < 16; i++ {
					a := blk[i/2] >> (uint(i%2) * 4) & 15
					alpha[i] = a<<4 | a
				}
			case 0x4d, 0x4e: // BC3: interpolated alpha
				col = blk[8:]
				pal := bcAlpha(blk)
				bits := uint64(blk[2]) | uint64(blk[3])<<8 | uint64(blk[4])<<16 | uint64(blk[5])<<24 | uint64(blk[6])<<32 | uint64(blk[7])<<40
				for i := 0; i < 16; i++ {
					alpha[i] = pal[bits>>(uint(i)*3)&7]
				}
			}
			pal := bcColors(col, size == 16)
			idx := le.Uint32(col[4:])
			for i := 0; i < 16; i++ {
				x, y := bx*4+i%4, by*4+i/4
				if x >= w || y >= h {
					continue
				}
				c := pal[idx>>(uint(i)*2)&3]
				if size == 16 {
					c.A = alpha[i]
				}
				img.SetNRGBA(x, y, c)
			}
		}
	}
	return img, nil
}

// swatchImage decodes the top mip of the texture in a .swatchbin bundle.
func swatchImage(b []byte) (*image.NRGBA, *texInfo, error) {
	bu, err := parseBundle(b)
	if err != nil {
		return nil, nil, err
	}
	if len(bu.blobs) == 0 || len(bu.blobs[0].metas) == 0 {
		return nil, nil, errors.New("bundle without a texture blob")
	}
	t, err := parseXboxHeader(bu.blobs[0].metas[0].payload)
	if err != nil {
		return nil, nil, err
	}
	f, err := t.dxgi()
	if err != nil {
		return nil, t, err
	}
	bpe, block, err := formatElement(f)
	if err != nil {
		return nil, t, err
	}
	desc := xgTex2D{Width: t.w, Height: t.h, MipLevels: t.mips, ArraySize: 1, Format: f, SampleCount: 1, BindFlags: xgBindShaderResource, TileMode: t.tile}
	c, err := create2D(&desc)
	if err != nil {
		return nil, t, err
	}
	defer c.release()
	data := bu.blobs[0].data
	if uint64(len(data)) < c.sizeBytes() {
		return nil, t, fmt.Errorf("texture data is %d bytes, the layout needs %d", len(data), c.sizeBytes())
	}
	row, slice := linearPitches(t, 0, bpe, block)
	lin := make([]byte, slice)
	if err := c.copyFrom(lin, 0, data, row, slice); err != nil {
		return nil, t, err
	}
	img, err := decodeBC(f, int(t.w), int(t.h), lin, row)
	return img, t, err
}

func runThumbs(stripped, outDir string, ids []string) error {
	zips, err := filepath.Glob(filepath.Join(stripped, "RC*.zip"))
	if err != nil || len(zips) == 0 {
		return fmt.Errorf("no RC*.zip in %s", stripped)
	}
	want := map[string]string{} // lower-case entry name -> output file
	for _, id := range ids {
		want[strings.ToLower("Thumbnail_"+id+"_Small.swatchbin")] = id + "_small.png"
		want[strings.ToLower("Thumbnail_"+id+"_Big.swatchbin")] = id + "_large.png"
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	done := 0
	for _, zp := range zips {
		zr, err := zip.OpenReader(zp)
		if err != nil {
			return fmt.Errorf("%s: %w", zp, err)
		}
		for _, f := range zr.File {
			out, ok := want[strings.ToLower(filepath.Base(f.Name))]
			if !ok {
				continue
			}
			rc, err := f.Open()
			if err != nil {
				zr.Close()
				return fmt.Errorf("%s in %s: %w", f.Name, zp, err)
			}
			b, err := io.ReadAll(rc)
			rc.Close()
			if err != nil {
				zr.Close()
				return fmt.Errorf("%s in %s: %w", f.Name, zp, err)
			}
			img, t, err := swatchImage(b)
			if err != nil {
				zr.Close()
				return fmt.Errorf("%s in %s: %w (texture %v)", f.Name, zp, err, t)
			}
			w, err := os.Create(filepath.Join(outDir, out))
			if err != nil {
				zr.Close()
				return err
			}
			err = png.Encode(w, img)
			w.Close()
			if err != nil {
				zr.Close()
				return err
			}
			fd, _ := t.dxgi()
			fmt.Printf("%s: %dx%d, DXGI format 0x%x, tile mode %d, from %s\n", out, t.w, t.h, fd, t.tile, filepath.Base(zp))
			delete(want, strings.ToLower(filepath.Base(f.Name)))
			done++
		}
		zr.Close()
	}
	for name := range want {
		fmt.Printf("not found: %s\n", name)
	}
	fmt.Printf("%d pictures written to %s\n", done, outDir)
	return nil
}

// runSwatches writes every .swatchbin entry of a zip whose name contains one of the patterns (case-insensitive) as a PNG.
func runSwatches(zipPath, outDir string, patterns []string) error {
	zr, err := zip.OpenReader(zipPath)
	if err != nil {
		return err
	}
	defer zr.Close()
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		return err
	}
	done := 0
	for _, f := range zr.File {
		name := strings.ToLower(f.Name)
		if !strings.HasSuffix(name, ".swatchbin") {
			continue
		}
		hit := false
		for _, p := range patterns {
			if strings.Contains(name, strings.ToLower(p)) {
				hit = true
				break
			}
		}
		if !hit {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			fmt.Printf("%s: %v\n", f.Name, err)
			continue
		}
		b, err := io.ReadAll(rc)
		rc.Close()
		if err != nil {
			fmt.Printf("%s: %v\n", f.Name, err)
			continue
		}
		img, t, err := swatchImage(b)
		if err != nil {
			fmt.Printf("%s: %v (texture %v)\n", f.Name, err, t)
			continue
		}
		out := strings.ReplaceAll(strings.TrimSuffix(f.Name, filepath.Ext(f.Name)), "/", "_") + ".png"
		w, err := os.Create(filepath.Join(outDir, out))
		if err != nil {
			return err
		}
		err = png.Encode(w, img)
		w.Close()
		if err != nil {
			return err
		}
		fd, _ := t.dxgi()
		fmt.Printf("%s: %dx%d, DXGI format 0x%x, tile mode %d\n", out, t.w, t.h, fd, t.tile)
		done++
	}
	fmt.Printf("%d pictures written to %s\n", done, outDir)
	return nil
}
