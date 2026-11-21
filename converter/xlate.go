package main

// PC (shader model 5.1) -> console (5.0) translation of track shaders.
//
// Beyond the 5.1 syntax, the PC builds differ from the console builds in how
// a draw finds its material:
//
//   InstancedDrawBundleCBuffer  cb0,space2  int IDBModelInstanceDataValue
//       low 16 bits  = base index into TextureOffsets (flat, 4 per element)
//       high 16 bits = base element in g_ParameterConstantBufferData
//   TextureOffsetsCBuffer       cb0,space1  int4 TextureOffsets[4096]
//   InstancedDrawBundleTextures2D  t0,space1  unbounded Texture2D array
//   ParameterConstantBuffer     cb0,space3  float4 data[4096]
//
// Material texture k is Textures2D[TextureOffsets[base+k]], material constant
// element J is data[paramBase+J]. The console binds texture k to register
// t(16+k) and the material constants to cb0 (the shaderbin's PMXT and PMBC
// tables, which are the same file on both platforms).
//
// The index arithmetic is plain integer code, so the translator runs it: an
// interpreter evaluates the integer instructions for several made-up values
// of IDBModelInstanceDataValue at once, with TextureOffsets filled with
// markers. An index that comes out as marker(base+k) for every value proves
// k; a constant-buffer index that comes out as paramBase+J proves J.
//
// PC-only inputs are replaced by constants:
//   globalCBuffer elements 69-71 (g_ShadowQualitySettings),
//   SSAOMap (t8) and SAOWidthHeight (lightScenarioCBuffer element 1141).

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"math"
	"sort"
)

// ---- operands ----

type xIndex struct {
	rep uint32
	imm []uint32 // 1 token for rep 0 and 3, 2 for rep 1 and 4
	rel *xOperand
}

type xOperand struct {
	tok uint32
	ext []uint32
	imm []uint32
	idx []xIndex
}

func (o *xOperand) typ() uint32 { return o.tok >> 12 & 0xff }

func parseOperand(in []uint32, p int) (*xOperand, int, error) {
	if p >= len(in) {
		return nil, 0, errors.New("operand past the end")
	}
	o := &xOperand{tok: in[p]}
	p++
	for e := o.tok; e&0x80000000 != 0; {
		if p >= len(in) {
			return nil, 0, errors.New("extended operand past the end")
		}
		e = in[p]
		o.ext = append(o.ext, e)
		p++
	}
	typ := o.typ()
	if typ == 4 || typ == 5 {
		n := 0
		switch o.tok & 3 {
		case 1:
			n = 1
		case 2:
			n = 4
		default:
			return nil, 0, fmt.Errorf("immediate with component mode %d", o.tok&3)
		}
		if typ == 5 {
			n *= 2
		}
		if p+n > len(in) {
			return nil, 0, errors.New("immediate past the end")
		}
		o.imm = append([]uint32(nil), in[p:p+n]...)
		p += n
	}
	dim := int(o.tok >> 20 & 3)
	for d := 0; d < dim; d++ {
		x := xIndex{rep: o.tok >> (22 + 3*uint(d)) & 7}
		take := func(n int) error {
			if p+n > len(in) {
				return errors.New("index past the end")
			}
			x.imm = append([]uint32(nil), in[p:p+n]...)
			p += n
			return nil
		}
		var err error
		switch x.rep {
		case 0:
			err = take(1)
		case 1:
			err = take(2)
		case 2:
			x.rel, p, err = parseOperand(in, p)
		case 3:
			if err = take(1); err == nil {
				x.rel, p, err = parseOperand(in, p)
			}
		case 4:
			if err = take(2); err == nil {
				x.rel, p, err = parseOperand(in, p)
			}
		default:
			err = fmt.Errorf("index representation %d", x.rep)
		}
		if err != nil {
			return nil, 0, err
		}
		o.idx = append(o.idx, x)
	}
	return o, p, nil
}

func (o *xOperand) emit(out *[]uint32) {
	tok := o.tok &^ (0x7ff << 20)
	tok |= uint32(len(o.idx)) << 20
	for d, x := range o.idx {
		tok |= x.rep << (22 + 3*uint(d))
	}
	*out = append(*out, tok)
	*out = append(*out, o.ext...)
	*out = append(*out, o.imm...)
	for _, x := range o.idx {
		*out = append(*out, x.imm...)
		if x.rel != nil {
			x.rel.emit(out)
		}
	}
}

// srcComp returns which component of the operand feeds destination
// component c.
func (o *xOperand) srcComp(c int) int {
	if o.tok&3 != 2 {
		return 0
	}
	switch o.tok >> 2 & 3 {
	case 1:
		return int(o.tok >> (4 + 2*uint(c)) & 3)
	case 2:
		return int(o.tok >> 4 & 3)
	}
	return c
}

// immLike builds an immediate that stands in for o: the values v are those of
// the replaced operand's x, y, z, w.
func immLike(o *xOperand, v [4]uint32) *xOperand {
	if o.tok&3 == 2 && o.tok>>2&3 == 1 {
		n := &xOperand{tok: 0x00004002}
		for c := 0; c < 4; c++ {
			n.imm = append(n.imm, v[o.srcComp(c)])
		}
		return n
	}
	return &xOperand{tok: 0x00004001, imm: []uint32{v[o.srcComp(0)]}}
}

// ---- interpreter ----

const xN = 6 // test vectors

var (
	xTexBase   = [xN]uint32{0, 1, 2, 3, 37, 1022}
	xParamBase = [xN]uint32{0, 7, 100, 1, 2000, 513}
)

const xMarker = 0x01000000

type xVal struct {
	known bool
	v     [xN]uint32
}

func xConst(c uint32) xVal {
	var r xVal
	r.known = true
	for i := range r.v {
		r.v[i] = c
	}
	return r
}

type xState map[uint32]*[4]xVal

func (s xState) clone() xState {
	n := make(xState, len(s))
	for k, v := range s {
		c := *v
		n[k] = &c
	}
	return n
}

func (s xState) get(reg uint32, c int) xVal {
	if r, ok := s[reg]; ok {
		return r[c]
	}
	return xVal{}
}

func (s xState) set(reg uint32, c int, v xVal) {
	r, ok := s[reg]
	if !ok {
		r = &[4]xVal{}
		s[reg] = r
	}
	r[c] = v
}

func xMerge(a, b xState) xState {
	out := xState{}
	for k, av := range a {
		bv, ok := b[k]
		if !ok {
			continue
		}
		var r [4]xVal
		for c := 0; c < 4; c++ {
			if av[c].known && bv[c].known && av[c].v == bv[c].v {
				r[c] = av[c]
			}
		}
		out[k] = &r
	}
	return out
}

type xInstr struct {
	op   uint32
	head []uint32    // opcode token and its extensions
	ops  []*xOperand // operands (not for declarations)
	raw  []uint32    // whole instruction
}

type xlator struct {
	ins []xInstr

	// 5.1 declarations: id -> space / lower bound, per operand type
	space map[uint32]map[uint32]uint32
	lower map[uint32]map[uint32]uint32
	upper map[uint32]map[uint32]uint32

	idbID, offID, parID, texID int64 // -1 when the shader has none
	hasSSAO                    bool

	st      xState
	texUsed map[uint32]bool
	parMax  int
	parDyn  bool
	notes   map[string]int
}

func (x *xlator) classOf(o *xOperand) (space, lb uint32, ok bool) {
	t := o.typ()
	if (t != opndCB && t != opndResource && t != opndSampler && t != opndUAV) || len(o.idx) == 0 || o.idx[0].rep != 0 {
		return 0, 0, false
	}
	id := o.idx[0].imm[0]
	sp, ok := x.space[t][id]
	return sp, x.lower[t][id], ok
}

// value of a source operand as seen by destination component c
func (x *xlator) read(o *xOperand, c int) xVal {
	sc := o.srcComp(c)
	switch o.typ() {
	case 0: // temp
		if len(o.idx) == 1 && o.idx[0].rep == 0 {
			return x.st.get(o.idx[0].imm[0], sc)
		}
	case 4:
		if len(o.imm) == 1 {
			return xConst(o.imm[0])
		}
		if len(o.imm) == 4 {
			return xConst(o.imm[sc])
		}
	case opndCB:
		if len(o.idx) != 3 || o.idx[0].rep != 0 {
			break
		}
		id := int64(o.idx[0].imm[0])
		if id == x.idbID && x.idbID >= 0 {
			if o.idx[2].rep == 0 && o.idx[2].imm[0] == 0 && sc == 0 {
				var r xVal
				r.known = true
				for i := range r.v {
					r.v[i] = xParamBase[i]<<16 | xTexBase[i]
				}
				return r
			}
			return xConst(0)
		}
		if id == x.offID && x.offID >= 0 {
			e, ok := x.index(&o.idx[2])
			if !ok {
				break
			}
			var r xVal
			r.known = true
			for i := range r.v {
				r.v[i] = xMarker + e.v[i]*4 + uint32(sc)
			}
			return r
		}
	}
	return xVal{}
}

// index evaluates an operand index (immediate, register, or register+imm).
func (x *xlator) index(ix *xIndex) (xVal, bool) {
	switch ix.rep {
	case 0:
		return xConst(ix.imm[0]), true
	case 2, 3:
		v := x.read(ix.rel, 0)
		if !v.known {
			return v, false
		}
		if ix.rep == 3 {
			for i := range v.v {
				v.v[i] += ix.imm[0]
			}
		}
		return v, true
	}
	return xVal{}, false
}

func xMap1(a xVal, f func(uint32) uint32) xVal {
	if !a.known {
		return xVal{}
	}
	var r xVal
	r.known = true
	for i := range r.v {
		r.v[i] = f(a.v[i])
	}
	return r
}

func xMap2(a, b xVal, f func(a, b uint32) uint32) xVal {
	if !a.known || !b.known {
		return xVal{}
	}
	var r xVal
	r.known = true
	for i := range r.v {
		r.v[i] = f(a.v[i], b.v[i])
	}
	return r
}

func xIsConst(a xVal, c uint32) bool { return a.known && a == xConst(c) }

func b2u(b bool) uint32 {
	if b {
		return 0xffffffff
	}
	return 0
}

// opcodes without a destination operand, and with two
var xNoDest = map[uint32]bool{2: true, 3: true, 4: true, 5: true, 6: true, 7: true, 8: true, 9: true, 10: true, 13: true, 18: true, 19: true, 20: true, 21: true, 22: true, 23: true, 31: true, 44: true, 48: true, 58: true, 62: true, 63: true, 76: true, 0x75: true, 0x76: true, 0x77: true, 0xa4: true, 0xa6: true, 0xa8: true}
var xTwoDest = map[uint32]bool{38: true, 77: true, 78: true, 81: true, 0x8e: true, 0x84: true, 0x85: true}

// step runs one instruction on the interpreter state.
func (x *xlator) step(in *xInstr) {
	if xNoDest[in.op] || len(in.ops) == 0 {
		return
	}
	nd := 1
	if xTwoDest[in.op] {
		nd = 2
	}
	var res [4]xVal
	exact := true
	src := in.ops[1:]
	for c := 0; c < 4; c++ {
		r := func(k int) xVal { return x.read(src[k], c) }
		var v xVal
		switch {
		case in.op == 54 && len(src) == 1: // mov
			v = r(0)
		case in.op == 1 && len(src) == 2: // and
			a, b := r(0), r(1)
			if xIsConst(a, 0) || xIsConst(b, 0) {
				v = xConst(0)
			} else {
				v = xMap2(a, b, func(a, b uint32) uint32 { return a & b })
			}
		case in.op == 60 && len(src) == 2: // or
			v = xMap2(r(0), r(1), func(a, b uint32) uint32 { return a | b })
		case in.op == 87 && len(src) == 2: // xor
			v = xMap2(r(0), r(1), func(a, b uint32) uint32 { return a ^ b })
		case in.op == 59 && len(src) == 1: // not
			v = xMap1(r(0), func(a uint32) uint32 { return ^a })
		case in.op == 30 && len(src) == 2: // iadd
			v = xMap2(r(0), r(1), func(a, b uint32) uint32 { return a + b })
		case in.op == 40 && len(src) == 1: // ineg
			v = xMap1(r(0), func(a uint32) uint32 { return -a })
		case in.op == 41 && len(src) == 2: // ishl
			v = xMap2(r(0), r(1), func(a, b uint32) uint32 { return a << (b & 31) })
		case in.op == 42 && len(src) == 2: // ishr
			v = xMap2(r(0), r(1), func(a, b uint32) uint32 { return uint32(int32(a) >> (b & 31)) })
		case in.op == 85 && len(src) == 2: // ushr
			v = xMap2(r(0), r(1), func(a, b uint32) uint32 { return a >> (b & 31) })
		case in.op == 79 && len(src) == 2: // ult
			v = xMap2(r(0), r(1), func(a, b uint32) uint32 { return b2u(a < b) })
		case in.op == 80 && len(src) == 2: // uge
			v = xMap2(r(0), r(1), func(a, b uint32) uint32 { return b2u(a >= b) })
		case in.op == 34 && len(src) == 2: // ilt
			v = xMap2(r(0), r(1), func(a, b uint32) uint32 { return b2u(int32(a) < int32(b)) })
		case in.op == 33 && len(src) == 2: // ige
			v = xMap2(r(0), r(1), func(a, b uint32) uint32 { return b2u(int32(a) >= int32(b)) })
		case in.op == 32 && len(src) == 2: // ieq
			v = xMap2(r(0), r(1), func(a, b uint32) uint32 { return b2u(a == b) })
		case in.op == 39 && len(src) == 2: // ine
			v = xMap2(r(0), r(1), func(a, b uint32) uint32 { return b2u(a != b) })
		case in.op == 36 && len(src) == 2: // imax
			v = xMap2(r(0), r(1), func(a, b uint32) uint32 {
				if int32(a) > int32(b) {
					return a
				}
				return b
			})
		case in.op == 37 && len(src) == 2: // imin
			v = xMap2(r(0), r(1), func(a, b uint32) uint32 {
				if int32(a) < int32(b) {
					return a
				}
				return b
			})
		case in.op == 83 && len(src) == 2: // umax
			v = xMap2(r(0), r(1), func(a, b uint32) uint32 {
				if a > b {
					return a
				}
				return b
			})
		case in.op == 84 && len(src) == 2: // umin
			v = xMap2(r(0), r(1), func(a, b uint32) uint32 {
				if a < b {
					return a
				}
				return b
			})
		case in.op == 35 && len(src) == 3: // imad
			v = xMap2(xMap2(r(0), r(1), func(a, b uint32) uint32 { return a * b }), r(2), func(a, b uint32) uint32 { return a + b })
		case in.op == 55 && len(src) == 3: // movc
			cnd, a, b := r(0), r(1), r(2)
			if cnd.known {
				v.known = true
				for i := range v.v {
					pick := b
					if cnd.v[i] != 0 {
						pick = a
					}
					if !pick.known {
						v.known = false
						break
					}
					v.v[i] = pick.v[i]
				}
			} else if a.known && b.known && a.v == b.v {
				v = a
			}
		case in.op == 0x8a && len(src) == 3: // ubfe width, offset, value
			w, off, val := r(0), r(1), r(2)
			if w.known && off.known && val.known {
				v.known = true
				for i := range v.v {
					wd, of := w.v[i]&31, off.v[i]&31
					switch {
					case wd == 0:
						v.v[i] = 0
					case wd+of < 32:
						v.v[i] = val.v[i] << (32 - wd - of) >> (32 - wd)
					default:
						v.v[i] = val.v[i] >> of
					}
				}
			}
		default:
			exact = false
		}
		res[c] = v
	}
	_ = exact
	for d := 0; d < nd && d < len(in.ops); d++ {
		dst := in.ops[d]
		if dst.typ() != 0 || len(dst.idx) != 1 || dst.idx[0].rep != 0 {
			continue
		}
		reg := dst.idx[0].imm[0]
		for c := 0; c < 4; c++ {
			if dst.tok&3 == 2 && dst.tok>>2&3 == 0 && dst.tok>>(4+uint(c))&1 == 0 {
				continue
			}
			if nd == 2 {
				x.st.set(reg, c, xVal{})
			} else {
				x.st.set(reg, c, res[c])
			}
		}
	}
}

// written lists the temp components a run of instructions writes.
func xWritten(ins []xInstr) map[uint32][4]bool {
	m := map[uint32][4]bool{}
	for i := range ins {
		in := &ins[i]
		if xNoDest[in.op] || len(in.ops) == 0 {
			continue
		}
		nd := 1
		if xTwoDest[in.op] {
			nd = 2
		}
		for d := 0; d < nd && d < len(in.ops); d++ {
			dst := in.ops[d]
			if dst.typ() != 0 || len(dst.idx) != 1 {
				continue
			}
			w := m[dst.idx[0].imm[0]]
			for c := 0; c < 4; c++ {
				if dst.tok&3 != 2 || dst.tok>>2&3 != 0 || dst.tok>>(4+uint(c))&1 != 0 {
					w[c] = true
				}
			}
			m[dst.idx[0].imm[0]] = w
		}
	}
	return m
}

func (x *xlator) forget(w map[uint32][4]bool) {
	for reg, cs := range w {
		for c := 0; c < 4; c++ {
			if cs[c] {
				x.st.set(reg, c, xVal{})
			}
		}
	}
}

// ---- translation ----

func isDecl(op uint32) bool { return (op >= 0x58 && op <= 0x6a) || (op >= 0x8f && op <= 0xa2) }

// shadow quality settings the PC build reads from globalCBuffer 69-71
// (g_ShadowQualitySettings: PSSM taps x3, pad; CSM taps x4; CloudShadow,
// MaxCSMLevel, MaxPSSMLevel, LevelBlending). Our choice of values.
var xShadowQuality = map[uint32][4]uint32{
	69: {16, 16, 16, 0},
	70: {16, 16, 16, 16},
	71: {1, 3, 2, 0},
}

var xOne = math.Float32bits(1)

type xFrame struct {
	kind    uint32
	saved   xState
	then    xState
	hasElse bool
	written map[uint32][4]bool
	// switch: the merged state of every break, whether the code after the
	// last break is unreachable, and whether a default label was seen
	exits      xState
	dead       bool
	hasDefault bool
}

func translateCode(code []byte) ([]byte, map[string]int, error) {
	if len(code) < 8 || len(code)%4 != 0 {
		return nil, nil, errors.New("bad code chunk")
	}
	in := make([]uint32, len(code)/4)
	for i := range in {
		in[i] = le.Uint32(code[i*4:])
	}
	if in[0]&0xff != 0x51 {
		return nil, nil, fmt.Errorf("shader model %x, not 5.1", in[0]&0xff)
	}
	if int(in[1]) != len(in) {
		return nil, nil, errors.New("code length field does not match the chunk")
	}
	x := &xlator{
		space: map[uint32]map[uint32]uint32{}, lower: map[uint32]map[uint32]uint32{}, upper: map[uint32]map[uint32]uint32{},
		idbID: -1, offID: -1, parID: -1, texID: -1,
		st: xState{}, texUsed: map[uint32]bool{}, parMax: -1, notes: map[string]int{},
	}
	for _, t := range []uint32{opndCB, opndResource, opndSampler, opndUAV} {
		x.space[t], x.lower[t], x.upper[t] = map[uint32]uint32{}, map[uint32]uint32{}, map[uint32]uint32{}
	}
	// pass 1: split into instructions, read the declarations
	for i := 2; i < len(in); {
		t := in[i]
		op := t & 0x7ff
		ln := int(t >> 24 & 0x7f)
		if op == 0x35 {
			ln = int(in[i+1])
			if ln < 2 || i+ln > len(in) {
				return nil, nil, errors.New("custom data past the end")
			}
			x.ins = append(x.ins, xInstr{op: op, raw: in[i : i+ln]})
			i += ln
			continue
		}
		if ln == 0 || i+ln > len(in) {
			return nil, nil, fmt.Errorf("instruction %#x at %d has length %d", op, i, ln)
		}
		raw := in[i : i+ln]
		p := 1
		for e := t; e&0x80000000 != 0; p++ {
			if p >= ln {
				return nil, nil, errors.New("extended opcode past the instruction")
			}
			e = raw[p]
		}
		ins := xInstr{op: op, head: raw[:p], raw: raw}
		switch {
		case op == 0x59 || op == 0x5a || op == 0x58 || op == 0xa1 || op == 0xa2 || op == 0x9c || op == 0x9d || op == 0x9e:
			if p+4 > ln {
				return nil, nil, errors.New("short declaration")
			}
			ot := raw[p]
			if ot>>20&3 != 3 || ot>>22&0x1ff != 0 || ot&0x80000000 != 0 {
				return nil, nil, fmt.Errorf("declaration operand %#x is not id[lb:ub]", ot)
			}
			typ := ot >> 12 & 0xff
			id, lb, ub, sp := raw[p+1], raw[p+2], raw[p+3], raw[ln-1]
			x.space[typ][id], x.lower[typ][id], x.upper[typ][id] = sp, lb, ub
			switch {
			case typ == opndCB && sp == 2 && lb == 0:
				x.idbID = int64(id)
			case typ == opndCB && sp == 1 && lb == 0:
				x.offID = int64(id)
			case typ == opndCB && sp == 3 && lb == 0:
				x.parID = int64(id)
			case typ == opndResource && sp == 1 && lb == 0 && ub == 0xffffffff:
				x.texID = int64(id)
			case typ == opndResource && sp == 0 && lb == 8 && op == 0x58:
				x.hasSSAO = true
			case sp != 0:
				return nil, nil, fmt.Errorf("unknown binding: operand type %d register %d space %d", typ, lb, sp)
			case lb != ub:
				return nil, nil, fmt.Errorf("register range %d..%d", lb, ub)
			}
		case isDecl(op):
		case op == 0x78:
			return nil, nil, errors.New("interface call not handled")
		default:
			for p < ln {
				var o *xOperand
				var err error
				if o, p, err = parseOperand(raw, p); err != nil {
					return nil, nil, fmt.Errorf("opcode %#x at token %d: %w", op, i, err)
				}
				ins.ops = append(ins.ops, o)
			}
		}
		x.ins = append(x.ins, ins)
		i += ln
	}
	// The SSAO map is only dropped when the shader is a pixel shader that
	// reads SAOWidthHeight; t8 could be something else elsewhere.
	if x.hasSSAO {
		sz := uint32(0)
		for i := range x.ins {
			in := &x.ins[i]
			if in.op == 0x59 {
				p := len(in.head)
				if x.space[opndCB][in.raw[p+1]] == 0 && in.raw[p+2] == 3 {
					sz = in.raw[p+4]
				}
			}
		}
		if sz != 1142 {
			x.hasSSAO = false
		}
	}

	// pass 2: interpret and rewrite
	type outIns struct {
		toks []uint32
		kind int // 0 plain, 1 resource declaration, 2 parameter buffer placeholder, 3 texture placeholder
		reg  uint32
	}
	var out []outIns
	var stack []xFrame
	matchEnd := func(i int, open, close uint32) int {
		depth := 0
		for k := i; k < len(x.ins); k++ {
			switch x.ins[k].op {
			case open:
				depth++
			case close:
				depth--
				if depth == 0 {
					return k
				}
			}
		}
		return len(x.ins) - 1
	}
	var texRet uint32 = 0x5555
	for i := range x.ins {
		ins := &x.ins[i]
		op := ins.op
		if op == 0x35 {
			out = append(out, outIns{toks: ins.raw})
			continue
		}
		p := len(ins.head)
		if ins.ops == nil && (isDecl(op)) {
			o := outIns{}
			switch op {
			case 0x59:
				id := ins.raw[p+1]
				switch int64(id) {
				case x.idbID, x.offID:
					continue
				case x.parID:
					out = append(out, outIns{kind: 2})
					continue
				}
				o.toks = append(append([]uint32{}, ins.head...), ins.raw[p]&^(0x7ff<<20)|2<<20, ins.raw[p+2], ins.raw[p+4])
			case 0x5a, 0xa1, 0x9d:
				o.toks = append(append([]uint32{}, ins.head...), ins.raw[p]&^(0x7ff<<20|0xfff)|1<<20, ins.raw[p+2])
				if op == 0xa1 {
					o.kind, o.reg = 1, ins.raw[p+2]
				}
			case 0x58, 0x9c, 0xa2, 0x9e:
				id := ins.raw[p+1]
				if op == 0x58 && int64(id) == x.texID {
					texRet = ins.raw[p+4]
					out = append(out, outIns{kind: 3})
					continue
				}
				if op == 0x58 && x.hasSSAO && ins.raw[p+2] == 8 {
					continue
				}
				o.toks = append(append([]uint32{}, ins.head...), ins.raw[p]&^(0x7ff<<20|0xfff)|1<<20, ins.raw[p+2], ins.raw[p+4])
				if op == 0x58 || op == 0xa2 {
					o.kind, o.reg = 1, ins.raw[p+2]
				}
			default:
				o.toks = append([]uint32{}, ins.raw...)
			}
			o.toks[0] = o.toks[0]&^(0x7f<<24) | uint32(len(o.toks))<<24
			out = append(out, o)
			continue
		}

		// control flow bookkeeping for the interpreter
		switch op {
		case 31: // if
			stack = append(stack, xFrame{kind: op, saved: x.st.clone()})
		case 18: // else
			if n := len(stack); n > 0 && stack[n-1].kind == 31 {
				stack[n-1].then, stack[n-1].hasElse = x.st, true
				x.st = stack[n-1].saved.clone()
			}
		case 21: // endif
			if n := len(stack); n > 0 && stack[n-1].kind == 31 {
				f := stack[n-1]
				stack = stack[:n-1]
				if f.hasElse {
					x.st = xMerge(x.st, f.then)
				} else {
					x.st = xMerge(x.st, f.saved)
				}
			}
		case 48: // loop: whatever the body writes is unknown inside and after
			w := xWritten(x.ins[i : matchEnd(i, 48, 22)+1])
			x.forget(w)
			stack = append(stack, xFrame{kind: op, written: w})
		case 22: // endloop
			if n := len(stack); n > 0 && stack[n-1].kind == 48 {
				x.forget(stack[n-1].written)
				stack = stack[:n-1]
			}
		case 76: // switch: every label starts from the state at the switch
			stack = append(stack, xFrame{kind: op, saved: x.st.clone(), dead: true})
		case 6, 10: // case, default
			if n := len(stack); n > 0 && stack[n-1].kind == 76 {
				f := &stack[n-1]
				if f.dead {
					x.st = f.saved.clone()
				} else {
					x.st = xMerge(x.st, f.saved)
				}
				f.dead = false
				if op == 10 {
					f.hasDefault = true
				}
			}
		case 2, 3, 62: // break, breakc, ret
			for n := len(stack) - 1; n >= 0; n-- {
				f := &stack[n]
				if f.kind == 48 {
					break
				}
				if f.kind != 76 {
					continue
				}
				if op != 62 {
					if f.exits == nil {
						f.exits = x.st.clone()
					} else {
						f.exits = xMerge(f.exits, x.st)
					}
				}
				if n == len(stack)-1 && op != 3 {
					f.dead = true
				}
				break
			}
		case 23: // endswitch
			if n := len(stack); n > 0 && stack[n-1].kind == 76 {
				f := stack[n-1]
				stack = stack[:n-1]
				end := f.exits
				if !f.dead {
					if end == nil {
						end = x.st
					} else {
						end = xMerge(end, x.st)
					}
				}
				if !f.hasDefault {
					if end == nil {
						end = f.saved
					} else {
						end = xMerge(end, f.saved)
					}
				}
				if end == nil {
					end = xState{}
				}
				x.st = end
			}
		case 4, 5, 44:
			if x.idbID >= 0 {
				return nil, nil, errors.New("subroutines with instanced draw bundles")
			}
		}

		// rewrite the operands (using the state before this instruction)
		ssaoSample := false
		newOps := make([]*xOperand, len(ins.ops))
		for k, o := range ins.ops {
			n, err := x.rewrite(o, &ssaoSample)
			if err != nil {
				return nil, nil, fmt.Errorf("instruction %d (opcode %#x): %w", i, op, err)
			}
			newOps[k] = n
		}
		x.step(ins)

		var toks []uint32
		if ssaoSample {
			// sample dest, uv, t8, sampler[, ...] -> mov dest, 1.0
			if len(newOps) < 3 || newOps[0].typ() != 0 {
				return nil, nil, fmt.Errorf("instruction %d: unexpected use of the SSAO map (opcode %#x)", i, op)
			}
			toks = []uint32{54}
			newOps[0].emit(&toks)
			(&xOperand{tok: 0x00004002, imm: []uint32{xOne, xOne, xOne, xOne}}).emit(&toks)
			x.notes["ssao sample -> 1.0"]++
		} else {
			toks = append(toks, ins.head...)
			for _, o := range newOps {
				o.emit(&toks)
			}
		}
		if len(toks) > 0x7f {
			return nil, nil, errors.New("instruction too long")
		}
		toks[0] = toks[0]&^(0x7f<<24) | uint32(len(toks))<<24
		out = append(out, outIns{toks: toks})
	}

	// material textures and the parameter buffer
	var texDecls []outIns
	ks := make([]int, 0, len(x.texUsed))
	for k := range x.texUsed {
		ks = append(ks, int(k))
	}
	sort.Ints(ks)
	for _, k := range ks {
		reg := uint32(16 + k)
		texDecls = append(texDecls, outIns{kind: 1, reg: reg, toks: []uint32{0x58 | 3<<11 | 4<<24, 0x00107000, reg, texRet}})
	}
	var final [][]uint32
	var res []outIns
	flushRes := func() {
		sort.SliceStable(res, func(a, b int) bool { return res[a].reg < res[b].reg })
		for _, r := range res {
			final = append(final, r.toks)
		}
		res = nil
	}
	inRes := false
	for _, o := range out {
		switch o.kind {
		case 1:
			if !inRes {
				inRes = true
				res = append(res, texDecls...)
				texDecls = nil
			}
			res = append(res, o)
			continue
		case 3:
			if !inRes {
				inRes = true
			}
			res = append(res, texDecls...)
			texDecls = nil
			continue
		case 2:
			if x.parMax >= 0 {
				t := uint32(0x59 | 4<<24)
				if x.parDyn {
					t |= 1 << 11
				}
				final = append(final, []uint32{t, 0x00208e46, 0, uint32(x.parMax + 1)})
			}
			continue
		}
		if inRes {
			flushRes()
			inRes = false
		}
		final = append(final, o.toks)
	}
	if inRes {
		flushRes()
	}
	if len(texDecls) > 0 {
		return nil, nil, errors.New("material textures used but no place for their declarations")
	}
	res2 := []uint32{in[0]&^0xff | 0x50, 0}
	for _, t := range final {
		res2 = append(res2, t...)
	}
	res2[1] = uint32(len(res2))
	b := make([]byte, len(res2)*4)
	for i, v := range res2 {
		le.PutUint32(b[i*4:], v)
	}
	if len(ks) > 0 {
		x.notes[fmt.Sprintf("material textures %v", ks)]++
	}
	if x.parMax >= 0 {
		x.notes[fmt.Sprintf("material constants cb0[%d]", x.parMax+1)]++
	}
	return b, x.notes, nil
}

// rewrite returns the console form of one operand.
func (x *xlator) rewrite(o *xOperand, ssao *bool) (*xOperand, error) {
	n := &xOperand{tok: o.tok, ext: o.ext, imm: o.imm}
	for _, ix := range o.idx {
		c := xIndex{rep: ix.rep, imm: ix.imm}
		if ix.rel != nil {
			r, err := x.rewrite(ix.rel, ssao)
			if err != nil {
				return nil, err
			}
			c.rel = r
		}
		n.idx = append(n.idx, c)
	}
	t := o.typ()
	switch t {
	case opndSampler, opndResource, opndUAV:
		if len(o.idx) != 2 || o.idx[0].rep != 0 {
			return nil, fmt.Errorf("operand type %d with %d indices", t, len(o.idx))
		}
		id := int64(o.idx[0].imm[0])
		if t == opndResource && id == x.texID && x.texID >= 0 {
			v, ok := x.index(&o.idx[1])
			if !ok {
				return nil, errors.New("material texture index is not derived from the bundle value")
			}
			k := v.v[0] - xMarker - xTexBase[0]
			for i := range v.v {
				if v.v[i]-xMarker-xTexBase[i] != k {
					return nil, errors.New("material texture index does not follow the texture offsets")
				}
			}
			if k >= 96 {
				return nil, fmt.Errorf("material texture slot %d", k)
			}
			x.texUsed[k] = true
			n.idx = []xIndex{{rep: 0, imm: []uint32{16 + k}}}
			return n, nil
		}
		if _, lb, ok := x.classOf(o); ok && t == opndResource && x.hasSSAO && lb == 8 && x.space[t][uint32(id)] == 0 {
			*ssao = true
		}
		if o.idx[1].rep != 0 {
			return nil, fmt.Errorf("dynamic index on operand type %d", t)
		}
		n.idx = n.idx[1:]
		return n, nil
	case opndCB:
		if len(o.idx) != 3 || o.idx[0].rep != 0 {
			return nil, fmt.Errorf("constant buffer operand with %d indices", len(o.idx))
		}
		id := int64(o.idx[0].imm[0])
		switch {
		case id == x.idbID && x.idbID >= 0, id == x.offID && x.offID >= 0:
			return immLike(o, [4]uint32{}), nil
		case id == x.parID && x.parID >= 0:
			v, ok := x.index(&o.idx[2])
			j := int64(-1)
			if ok {
				j = int64(v.v[0]) - int64(xParamBase[0])
				for i := range v.v {
					if int64(v.v[i])-int64(xParamBase[i]) != j {
						j = -1
						break
					}
				}
			}
			if j < 0 || j >= 4096 {
				return nil, errors.New("material constant index does not follow the bundle value")
			}
			if int(j) > x.parMax {
				x.parMax = int(j)
			}
			n.idx = []xIndex{{rep: 0, imm: []uint32{0}}, {rep: 0, imm: []uint32{uint32(j)}}}
			return n, nil
		}
		reg := x.lower[opndCB][uint32(id)]
		if o.idx[1].rep != 0 {
			return nil, errors.New("dynamic constant buffer register")
		}
		if o.idx[2].rep == 0 {
			e := o.idx[2].imm[0]
			if q, ok := xShadowQuality[e]; ok && reg == 2 {
				x.notes["shadow quality constant"]++
				return immLike(o, q), nil
			}
			if reg == 3 && e == 1141 && x.hasSSAO {
				x.notes["SAOWidthHeight constant"]++
				return immLike(o, [4]uint32{xOne, xOne, 0, 0}), nil
			}
		}
		n.idx = n.idx[1:]
		return n, nil
	}
	return n, nil
}

// shaderPCToConsole converts a PC shader build into the console form.
func shaderPCToConsole(b []byte) ([]byte, map[string]int, error) {
	chunks := dxbcChunks(b)
	if len(chunks) == 0 {
		return nil, nil, errors.New("not a DXBC container")
	}
	var sig, rest []dxChunk
	var notes map[string]int
	for _, c := range chunks {
		switch c.tag {
		case "ISGN", "OSGN", "PCSG", "ISG1", "OSG1", "PSG1":
			sig = append(sig, c)
		case "SHEX", "SHDR":
			code, n, err := translateCode(c.data)
			if err != nil {
				return nil, nil, err
			}
			notes = n
			h := sha256.Sum256(code)
			rest = append(rest, dxChunk{"SHEX", code}, dxChunk{"XHSH", h[:8]})
		case "RDEF", "STAT":
		default:
			return nil, nil, fmt.Errorf("unexpected chunk %s", c.tag)
		}
	}
	if rest == nil {
		return nil, nil, errors.New("no code chunk")
	}
	return buildDXBC(append(sig, rest...)), notes, nil
}
