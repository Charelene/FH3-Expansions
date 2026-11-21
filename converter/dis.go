package main

// A small DXBC disassembler, enough to compare PC (5.1) and console (5.0)
// builds. Resource-class operands print as <class><space>:<register>.

import (
	"fmt"
	"strings"
)

var opNames = strings.Fields("add and break breakc call callc case continue continuec cut default deriv_rtx deriv_rty discard div dp2 dp3 dp4 else emit emitthencut endif endloop endswitch eq exp frc ftoi ftou ge iadd if ieq ige ilt imad imax imin imul ine ineg ishl ishr itof label ld ld_ms log loop lt mad min max customdata mov movc mul ne nop not or resinfo ret retc round_ne round_ni round_pi round_z rsq sample sample_c sample_c_lz sample_l sample_d sample_b sqrt switch sincos udiv ult uge umul umad umax umin ushr utof xor dcl_resource dcl_constantbuffer dcl_sampler dcl_index_range dcl_gs_output_topology dcl_gs_input_primitive dcl_max_output_vertex_count dcl_input dcl_input_sgv dcl_input_siv dcl_input_ps dcl_input_ps_sgv dcl_input_ps_siv dcl_output dcl_output_sgv dcl_output_siv dcl_temps dcl_indexable_temp dcl_global_flags reserved0 lod gather4 sample_pos sample_info reserved1 hs_decls hs_control_point_phase hs_fork_phase hs_join_phase emit_stream cut_stream emitthencut_stream interface_call bufinfo deriv_rtx_coarse deriv_rtx_fine deriv_rty_coarse deriv_rty_fine gather4_c gather4_po gather4_po_c rcp f32tof16 f16tof32 uaddc usubb countbits firstbit_hi firstbit_lo firstbit_shi ubfe ibfe bfi bfrev swapc dcl_stream dcl_function_body dcl_function_table dcl_interface dcl_input_control_point_count dcl_output_control_point_count dcl_tess_domain dcl_tess_partitioning dcl_tess_output_primitive dcl_hs_max_tessfactor dcl_hs_fork_phase_instance_count dcl_hs_join_phase_instance_count dcl_thread_group dcl_uav_typed dcl_uav_raw dcl_uav_structured dcl_tls_raw dcl_tls_structured dcl_resource_raw dcl_resource_structured ld_uav_typed store_uav_typed ld_raw store_raw ld_structured store_structured")

type disState struct {
	v51 bool
	ids map[uint32]map[uint32]string // class -> id -> "space:register" (5.1)
}

var operandNames = map[uint32]string{0: "r", 1: "v", 2: "o", 3: "x", 6: "s", 7: "t", 8: "cb", 9: "icb", 10: "label", 11: "vPrim", 12: "oDepth", 13: "null", 14: "rasterizer", 15: "oMask", 30: "u", 23: "vCoverage"}

func (d *disState) operand(in []uint32, p int) (string, int) {
	tok := in[p]
	p++
	for e := tok; e&0x80000000 != 0; p++ {
		e = in[p]
	}
	typ := tok >> 12 & 0xff
	dim := int(tok >> 20 & 3)
	reps := [3]uint32{tok >> 22 & 7, tok >> 25 & 7, tok >> 28 & 7}
	comp := ""
	if tok&3 == 2 {
		c := "xyzw"
		switch tok >> 2 & 3 {
		case 0:
			for i := 0; i < 4; i++ {
				if tok>>(4+uint(i))&1 != 0 {
					comp += string(c[i])
				}
			}
		case 1:
			for i := 0; i < 4; i++ {
				comp += string(c[tok>>(4+2*uint(i))&3])
			}
		case 2:
			comp = string(c[tok>>4&3])
		}
		comp = "." + comp
	}
	if typ == 4 {
		n := 1
		if tok&3 == 2 {
			n = 4
		}
		vals := []string{}
		for i := 0; i < n; i++ {
			vals = append(vals, fmt.Sprintf("%#x", in[p+i]))
		}
		return "l(" + strings.Join(vals, ",") + ")", p + n
	}
	if typ == 5 {
		n := 2
		if tok&3 == 2 {
			n = 8
		}
		return "d(...)", p + n
	}
	idx := make([]string, dim)
	raw := make([]uint32, dim)
	for i := 0; i < dim; i++ {
		switch reps[i] {
		case 0:
			raw[i] = in[p]
			idx[i] = fmt.Sprint(in[p])
			p++
		case 1:
			idx[i] = "imm64"
			p += 2
		case 2:
			var s string
			s, p = d.operand(in, p)
			idx[i] = s
			raw[i] = 0xffffffff
		case 3:
			imm := in[p]
			p++
			var s string
			s, p = d.operand(in, p)
			idx[i] = fmt.Sprintf("%s+%d", s, imm)
			raw[i] = 0xffffffff
		case 4:
			p += 2
			var s string
			s, p = d.operand(in, p)
			idx[i] = s + "+imm64"
		}
	}
	name, ok := operandNames[typ]
	if !ok {
		name = fmt.Sprintf("op%d_", typ)
	}
	s := name
	switch typ {
	case 6, 7, 8, 30:
		if d.v51 && dim >= 2 {
			loc := d.ids[typ][raw[0]]
			if reps[1] == 0 && strings.HasSuffix(loc, fmt.Sprintf(":%d", raw[1])) {
				s += loc
			} else {
				s += loc + "{" + idx[1] + "}"
			}
			idx = idx[2:]
		} else if dim >= 1 {
			s += "0:" + idx[0]
			idx = idx[1:]
		}
	}
	for _, x := range idx {
		if typ == 0 || typ == 1 || typ == 2 {
			s += x
		} else {
			s += "[" + x + "]"
		}
	}
	return s + comp, p
}

// myDisasm returns one line per instruction.
func myDisasm(b []byte) ([]string, error) {
	var code []byte
	for _, c := range dxbcChunks(b) {
		if c.tag == "SHEX" || c.tag == "SHDR" {
			code = c.data
		}
	}
	if code == nil {
		return nil, fmt.Errorf("no code")
	}
	in := make([]uint32, len(code)/4)
	for i := range in {
		in[i] = le.Uint32(code[i*4:])
	}
	d := &disState{v51: in[0]&0xff == 0x51, ids: map[uint32]map[uint32]string{6: {}, 7: {}, 8: {}, 30: {}}}
	var out []string
	for i := 2; i < len(in); {
		t := in[i]
		op := t & 0x7ff
		ln := int(t >> 24 & 0x7f)
		if op == 0x35 {
			ln = int(in[i+1])
			out = append(out, fmt.Sprintf("customdata(%d words)", ln))
			i += ln
			continue
		}
		if ln == 0 {
			return out, fmt.Errorf("zero length at %d", i)
		}
		ins := in[i : i+ln]
		p := 1
		for e := t; e&0x80000000 != 0; p++ {
			e = ins[p]
		}
		name := fmt.Sprintf("op_%#x", op)
		if int(op) < len(opNames) {
			name = opNames[op]
		}
		line := name
		isDecl := (op >= 0x58 && op <= 0x6a) || (op >= 0x8f && op <= 0xa2)
		switch {
		case op == 0x58 || op == 0x59 || op == 0x5a || op == 0xa1 || op == 0xa2 || op == 0x9c || op == 0x9d || op == 0x9e:
			cls := map[uint32]uint32{0x58: 7, 0x59: 8, 0x5a: 6, 0xa1: 7, 0xa2: 7, 0x9c: 30, 0x9d: 30, 0x9e: 30}[op]
			pre := map[uint32]string{6: "s", 7: "t", 8: "cb", 30: "u"}[cls]
			if d.v51 {
				id, lb, ub := ins[p+1], ins[p+2], ins[p+3]
				sp := ins[ln-1]
				loc := fmt.Sprintf("%d:%d", sp, lb)
				d.ids[cls][id] = loc
				line += " " + pre + loc
				if ub != lb {
					line += fmt.Sprintf("..%#x", ub)
				}
				if op == 0x59 {
					line += fmt.Sprintf("[%d]", ins[p+4])
				}
			} else {
				line += fmt.Sprintf(" %s0:%d", pre, ins[p+1])
				if op == 0x59 {
					line += fmt.Sprintf("[%d]", ins[p+2])
				}
			}
			if op == 0x58 {
				line += fmt.Sprintf(" dim%d", t>>11&0x1f)
			}
		case isDecl:
			for k := p; k < ln; k++ {
				line += fmt.Sprintf(" %08x", ins[k])
			}
		default:
			var parts []string
			for p < ln {
				var s string
				s, p = d.operand(ins, p)
				parts = append(parts, s)
			}
			if len(parts) > 0 {
				line += " " + strings.Join(parts, ", ")
			}
			if p != ln {
				line += " <parse mismatch>"
			}
		}
		out = append(out, line)
		i += ln
	}
	return out, nil
}
