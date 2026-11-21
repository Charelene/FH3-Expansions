package main

// Shader model 5.1 -> 5.0 rewrite of DXBC bytecode.
//
// The PC builds are 5.1: constant buffers, samplers, resources and UAVs are
// declared as "id[lower:upper], space" and used as "id[register]" (plus the
// element for a constant buffer). 5.0 has no ids, ranges or spaces: the
// declaration and every use name the register directly. With one register
// per declaration and space 0, as these shaders have, the rewrite drops the
// id index everywhere and the range and space from the declarations.
// The container is rebuilt the way the console builds look: ISGN, OSGN (and
// PCSG), SHEX, XHSH, without RDEF and STAT, and re-signed.

import (
	"bytes"
	"crypto/md5"
	"crypto/sha256"
	"encoding"
	"encoding/binary"
	"errors"
	"fmt"
)

// dxbcChecksum is the container hash at offset 4: MD5's block function over
// the data from offset 20 with its own final block.
func dxbcChecksum(b []byte) [16]byte {
	data := b[20:]
	left := len(data) % 64
	full, last := data[:len(data)-left], data[len(data)-left:]
	numBits := uint32(len(data) * 8)
	numBits2 := numBits>>2 | 1
	var buf bytes.Buffer
	buf.Write(full)
	u32 := func(v uint32) []byte { x := make([]byte, 4); le.PutUint32(x, v); return x }
	if left >= 56 {
		buf.Write(last)
		pad := make([]byte, 64-left)
		pad[0] = 0x80
		buf.Write(pad)
		blk := make([]byte, 64)
		le.PutUint32(blk, numBits)
		le.PutUint32(blk[60:], numBits2)
		buf.Write(blk)
	} else {
		buf.Write(u32(numBits))
		buf.Write(last)
		pad := make([]byte, 56-left)
		pad[0] = 0x80
		buf.Write(pad)
		buf.Write(u32(numBits2))
	}
	h := md5.New()
	h.Write(buf.Bytes())
	st, _ := h.(encoding.BinaryMarshaler).MarshalBinary()
	var out [16]byte
	for i := 0; i < 4; i++ {
		le.PutUint32(out[i*4:], binary.BigEndian.Uint32(st[4+i*4:]))
	}
	return out
}

const (
	opndSampler  = 6
	opndResource = 7
	opndCB       = 8
	opndUAV      = 30
)

// convOperand copies one operand from in[p:] to out, dropping the id index of
// 5.1 resource-class operands. It returns the position after the operand.
func convOperand(in []uint32, p int, out *[]uint32) (int, error) {
	if p >= len(in) {
		return 0, errors.New("operand past the end")
	}
	tok := in[p]
	p++
	var ext []uint32
	for e := tok; e&0x80000000 != 0; {
		if p >= len(in) {
			return 0, errors.New("extended operand past the end")
		}
		e = in[p]
		ext = append(ext, e)
		p++
	}
	typ := tok >> 12 & 0xff
	dim := int(tok >> 20 & 3)
	reps := [3]uint32{tok >> 22 & 7, tok >> 25 & 7, tok >> 28 & 7}
	var imm []uint32
	if typ == 4 || typ == 5 {
		n := 0
		switch tok & 3 {
		case 1:
			n = 1
		case 2:
			n = 4
		default:
			return 0, fmt.Errorf("immediate operand with component mode %d", tok&3)
		}
		if typ == 5 {
			n *= 2
		}
		if p+n > len(in) {
			return 0, errors.New("immediate past the end")
		}
		imm = in[p : p+n]
		p += n
	}
	idx := make([][]uint32, dim)
	for d := 0; d < dim; d++ {
		var o []uint32
		var err error
		take := func(n int) error {
			if p+n > len(in) {
				return errors.New("index past the end")
			}
			o = append(o, in[p:p+n]...)
			p += n
			return nil
		}
		switch reps[d] {
		case 0:
			err = take(1)
		case 1:
			err = take(2)
		case 2:
			p, err = convOperand(in, p, &o)
		case 3:
			if err = take(1); err == nil {
				p, err = convOperand(in, p, &o)
			}
		case 4:
			if err = take(2); err == nil {
				p, err = convOperand(in, p, &o)
			}
		default:
			err = fmt.Errorf("index representation %d", reps[d])
		}
		if err != nil {
			return 0, err
		}
		idx[d] = o
	}
	want := 0
	switch typ {
	case opndSampler, opndResource, opndUAV:
		want = 2
	case opndCB:
		want = 3
	}
	if want != 0 {
		if dim != want || reps[0] != 0 {
			return 0, fmt.Errorf("operand type %d with %d indices (first representation %d)", typ, dim, reps[0])
		}
		idx = idx[1:]
		reps = [3]uint32{reps[1], reps[2], 0}
		dim--
	}
	nt := tok&^(0x7ff<<20) | uint32(dim)<<20 | reps[0]<<22 | reps[1]<<25 | reps[2]<<28
	*out = append(*out, nt)
	*out = append(*out, ext...)
	*out = append(*out, imm...)
	for _, o := range idx {
		*out = append(*out, o...)
	}
	return p, nil
}

// convCode rewrites a SHEX chunk.
func convCode(code []byte) ([]byte, error) {
	if len(code) < 8 || len(code)%4 != 0 {
		return nil, errors.New("bad code chunk")
	}
	in := make([]uint32, len(code)/4)
	for i := range in {
		in[i] = le.Uint32(code[i*4:])
	}
	if in[0]&0xff != 0x51 {
		return nil, fmt.Errorf("shader model %x, not 5.1", in[0]&0xff)
	}
	if int(in[1]) != len(in) {
		return nil, errors.New("code length field does not match the chunk")
	}
	out := []uint32{in[0]&^0xff | 0x50, 0}
	for i := 2; i < len(in); {
		t := in[i]
		op := t & 0x7ff
		ln := int(t >> 24 & 0x7f)
		if op == 0x35 {
			ln = int(in[i+1])
			if ln < 2 || i+ln > len(in) {
				return nil, errors.New("custom data past the end")
			}
			out = append(out, in[i:i+ln]...)
			i += ln
			continue
		}
		if ln == 0 || i+ln > len(in) {
			return nil, fmt.Errorf("instruction %#x at %d has length %d", op, i, ln)
		}
		ins := in[i : i+ln]
		p := 1
		for e := t; e&0x80000000 != 0; p++ {
			if p >= ln {
				return nil, errors.New("extended opcode past the instruction")
			}
			e = ins[p]
		}
		start := len(out)
		out = append(out, ins[:p]...)
		// declOperand rewrites "id[lb:ub]" to "lb" and returns the tokens after it.
		declOperand := func(keep int) ([]uint32, error) {
			if p+4 > ln {
				return nil, errors.New("short declaration")
			}
			ot := ins[p]
			if ot>>20&3 != 3 || ot>>22&0x1ff != 0 || ot&0x80000000 != 0 {
				return nil, fmt.Errorf("declaration operand %#x is not id[lb:ub]", ot)
			}
			lb, ub := ins[p+2], ins[p+3]
			if lb != ub {
				return nil, fmt.Errorf("register range %d..%d", lb, ub)
			}
			rest := ins[p+4:]
			if len(rest) != keep+1 {
				return nil, fmt.Errorf("declaration %#x has %d trailing tokens, want %d", op, len(rest), keep+1)
			}
			if rest[keep] != 0 {
				return nil, fmt.Errorf("register space %d", rest[keep])
			}
			return append([]uint32{lb}, rest[:keep]...), nil
		}
		var err error
		switch {
		case op == 0x59: // dcl_constantbuffer: id[lb:ub], size, space -> [lb][size]
			var r []uint32
			if r, err = declOperand(1); err == nil {
				out = append(out, ins[p]&^(0x7ff<<20)|2<<20, r[0], r[1])
			}
		case op == 0x5a, op == 0xa1, op == 0x9d: // sampler, raw resource, raw UAV: id[lb:ub], space -> [lb]
			var r []uint32
			if r, err = declOperand(0); err == nil {
				out = append(out, ins[p]&^(0x7ff<<20|0xfff)|1<<20, r[0])
			}
		case op == 0x58, op == 0x9c, op == 0xa2, op == 0x9e: // typed resource/UAV (+return type), structured (+stride)
			var r []uint32
			if r, err = declOperand(1); err == nil {
				out = append(out, ins[p]&^(0x7ff<<20|0xfff)|1<<20, r[0], r[1])
			}
		case op >= 0x5b && op <= 0x6a, op >= 0x8f && op <= 0xa0:
			out = append(out, ins[p:]...) // other declarations name no resources
		case op == 0x78:
			return nil, errors.New("interface call not handled")
		default:
			for p < ln {
				if p, err = convOperand(ins, p, &out); err != nil {
					break
				}
			}
			if err == nil && p != ln {
				err = errors.New("operands run past the instruction")
			}
		}
		if err != nil {
			return nil, fmt.Errorf("opcode %#x at token %d: %w", op, i, err)
		}
		n := len(out) - start
		if n > 0x7f {
			return nil, errors.New("instruction too long")
		}
		out[start] = out[start]&^(0x7f<<24) | uint32(n)<<24
		i += ln
	}
	out[1] = uint32(len(out))
	b := make([]byte, len(out)*4)
	for i, v := range out {
		le.PutUint32(b[i*4:], v)
	}
	return b, nil
}

// buildDXBC writes a container with a valid checksum.
func buildDXBC(chunks []dxChunk) []byte {
	size := 0x20 + 4*len(chunks)
	for _, c := range chunks {
		size += 8 + len(c.data)
	}
	b := make([]byte, size)
	copy(b, "DXBC")
	le.PutUint32(b[0x14:], 1)
	le.PutUint32(b[0x18:], uint32(size))
	le.PutUint32(b[0x1c:], uint32(len(chunks)))
	o := 0x20 + 4*len(chunks)
	for i, c := range chunks {
		le.PutUint32(b[0x20+4*i:], uint32(o))
		copy(b[o:], c.tag)
		le.PutUint32(b[o+4:], uint32(len(c.data)))
		copy(b[o+8:], c.data)
		o += 8 + len(c.data)
	}
	sum := dxbcChecksum(b)
	copy(b[4:], sum[:])
	return b
}

// shader51to50 converts a PC shader build into the console form.
func shader51to50(b []byte) ([]byte, error) {
	chunks := dxbcChunks(b)
	if len(chunks) == 0 {
		return nil, errors.New("not a DXBC container")
	}
	var out []dxChunk
	done := false
	for _, c := range chunks {
		switch c.tag {
		case "ISGN", "OSGN", "PCSG", "ISG1", "OSG1", "PSG1":
			out = append(out, c)
		case "SHEX", "SHDR":
			code, err := convCode(c.data)
			if err != nil {
				return nil, err
			}
			out = append(out, dxChunk{"SHEX", code})
			h := sha256.Sum256(code)
			out = append(out, dxChunk{"XHSH", h[:8]})
			done = true
		case "RDEF", "STAT":
		default:
			return nil, fmt.Errorf("unexpected chunk %s", c.tag)
		}
	}
	if !done {
		return nil, errors.New("no code chunk")
	}
	// console order: signatures, code, hash
	var sig, rest []dxChunk
	for _, c := range out {
		if c.tag == "SHEX" || c.tag == "XHSH" {
			rest = append(rest, c)
		} else {
			sig = append(sig, c)
		}
	}
	return buildDXBC(append(sig, rest...)), nil
}
