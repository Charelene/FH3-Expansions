package main

// Desktop XG (Xbox One texture layout library from the GDK, bin/XboxOne/xg.dll),
// called through syscall. Only the calls the converter needs.

import (
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"syscall"
	"unsafe"
)

// XG_TEXTURE2D_DESC (xg.h).
type xgTex2D struct {
	Width, Height, MipLevels, ArraySize, Format uint32
	SampleCount, SampleQuality                  uint32
	Usage, BindFlags, CPUAccessFlags, MiscFlags uint32
	ESRAMOffsetBytes, ESRAMUsageBytes           uint32
	TileMode, Pitch                             uint32
}

const (
	xgBindShaderResource = 0x8
	xgMiscTextureCube    = 0x4
	xgDimTexture2D       = 3
)

type xgMipLayout struct {
	SizeBytes, OffsetBytes, Slice2DSizeBytes               uint64
	PitchPixels, PitchBytes, AlignmentBytes                uint32
	PaddedWidthElements, PaddedHeightElements, PaddedDepth uint32
	WidthElements, HeightElements, DepthOrArraySize        uint32
	SampleCount, TileMode                                  uint32
	BankRotationAddressBitMask, BankRotationBytesPerSlice  uint64
	SliceDepthElements                                     uint32
	_                                                      uint32
}

type xgPlaneLayout struct {
	Usage              uint32
	_                  uint32
	SizeBytes          uint64
	BaseOffsetBytes    uint64
	BaseAlignmentBytes uint64
	BytesPerElement    uint32
	_                  uint32
	MipLayout          [15]xgMipLayout
}

type xgResourceLayout struct {
	SizeBytes          uint64
	BaseAlignmentBytes uint64
	MipLevels, Planes  uint32
	Plane              [4]xgPlaneLayout
	Dimension          uint32
	_                  uint32
}

var (
	xgDLL         *syscall.DLL
	procCreate2D  *syscall.Proc
	procOptimalTM *syscall.Proc
)

func loadXG(path string) error {
	var err error
	if xgDLL, err = syscall.LoadDLL(path); err != nil {
		return err
	}
	if procCreate2D, err = xgDLL.FindProc("XGCreateTexture2DComputer"); err != nil {
		return err
	}
	procOptimalTM, err = xgDLL.FindProc("XGComputeOptimalTileMode")
	if err == nil {
		xgActive = true
	}
	return err
}

// xgComputer is either a computer of the GDK's xg.dll (p) or our own layout
// (own, tile1d.go). Which one create2D builds depends on xgActive.
type xgComputer struct {
	p   unsafe.Pointer
	own *ownLayout
}

// xgActive is set by loadXG: the GDK DLL is loaded and used. Without it every
// texture goes through the converter's own tiler.
var xgActive bool

// setupTiler is what the converting commands call: the own tiler, unless the
// environment variable UWP2XBOX_XG asks for xg.dll (a path, or 1 for the default).
func setupTiler() error {
	p := os.Getenv("UWP2XBOX_XG")
	if p == "" {
		return nil
	}
	if p == "1" {
		p = defaultXG
	}
	return loadXG(p)
}

func (c xgComputer) vcall(slot int, args ...uintptr) uintptr {
	vt := *(*unsafe.Pointer)(c.p)
	fn := *(*uintptr)(unsafe.Add(vt, slot*8))
	r, _, _ := syscall.SyscallN(fn, append([]uintptr{uintptr(c.p)}, args...)...)
	return r
}

func create2D(d *xgTex2D) (xgComputer, error) {
	if xgActive {
		return create2DXG(d)
	}
	if d.TileMode != 13 || d.SampleCount != 1 {
		return xgComputer{}, fmt.Errorf("own tiler: tile mode %d, %d samples", d.TileMode, d.SampleCount)
	}
	l, err := newOwnLayout(d.Format, d.Width, d.Height, d.MipLevels, d.ArraySize)
	if err != nil {
		return xgComputer{}, err
	}
	return xgComputer{own: l}, nil
}

func create2DXG(d *xgTex2D) (xgComputer, error) {
	var c unsafe.Pointer
	r, _, _ := procCreate2D.Call(uintptr(unsafe.Pointer(d)), uintptr(unsafe.Pointer(&c)))
	if int32(r) < 0 || c == nil {
		return xgComputer{}, fmt.Errorf("XGCreateTexture2DComputer hr=0x%08x for %+v", uint32(r), *d)
	}
	return xgComputer{p: c}, nil
}

var (
	tallyMu   sync.Mutex
	tileTally = map[string]int{}
)

func optimalTileMode(format, w, h, arraySize, misc uint32) uint32 {
	if !xgActive {
		if ownSupported(format) {
			return 13
		}
		return 0
	}
	r, _, _ := procOptimalTM.Call(xgDimTexture2D, uintptr(format), uintptr(w), uintptr(h), uintptr(arraySize), 1, xgBindShaderResource, uintptr(misc))
	tallyMu.Lock()
	tileTally[fmt.Sprintf("format 0x%02x tile %d array>1 %v misc %d", format, uint32(r), arraySize > 1, misc)]++
	tallyMu.Unlock()
	return uint32(r)
}

// printTileTally lists which (format, tile mode) pairs a run asked XG for.
func printTileTally() {
	tallyMu.Lock()
	defer tallyMu.Unlock()
	ks := make([]string, 0, len(tileTally))
	for k := range tileTally {
		ks = append(ks, k)
	}
	sort.Strings(ks)
	for _, k := range ks {
		fmt.Printf("%8d  %s\n", tileTally[k], k)
	}
}

func (c xgComputer) release() {
	if c.own == nil {
		c.vcall(1)
	}
}
func (c xgComputer) layout() (*xgResourceLayout, error) {
	if c.own != nil {
		return c.own.resourceLayout(), nil
	}
	l := new(xgResourceLayout)
	if r := c.vcall(2, uintptr(unsafe.Pointer(l))); int32(r) < 0 {
		return nil, fmt.Errorf("GetResourceLayout hr=0x%08x", uint32(r))
	}
	return l, nil
}
func (c xgComputer) sizeBytes() uint64 {
	if c.own != nil {
		return c.own.size
	}
	return uint64(c.vcall(3))
}
func (c xgComputer) baseAlignment() uint64 {
	if c.own != nil {
		return 256
	}
	return uint64(c.vcall(4))
}

// copyInto tiles one subresource from linear data.
func (c xgComputer) copyInto(dst []byte, sub uint32, src []byte, rowPitch, slicePitch uint32) error {
	if c.own != nil {
		return c.own.tileInto(dst, sub%c.own.mips, sub/c.own.mips, src, rowPitch)
	}
	if len(dst) == 0 || len(src) == 0 {
		return errors.New("empty buffer")
	}
	r := c.vcall(8, uintptr(unsafe.Pointer(&dst[0])), 0, uintptr(sub), uintptr(unsafe.Pointer(&src[0])), uintptr(rowPitch), uintptr(slicePitch))
	if int32(r) < 0 {
		return fmt.Errorf("CopyIntoSubresource hr=0x%08x", uint32(r))
	}
	return nil
}

// copyFrom detiles one subresource into linear data.
func (c xgComputer) copyFrom(dst []byte, sub uint32, src []byte, rowPitch, slicePitch uint32) error {
	if c.own != nil {
		return errors.New("detiling needs the GDK's xg.dll")
	}
	r := c.vcall(9, uintptr(unsafe.Pointer(&dst[0])), 0, uintptr(sub), uintptr(unsafe.Pointer(&src[0])), uintptr(rowPitch), uintptr(slicePitch))
	if int32(r) < 0 {
		return fmt.Errorf("CopyFromSubresource hr=0x%08x", uint32(r))
	}
	return nil
}
