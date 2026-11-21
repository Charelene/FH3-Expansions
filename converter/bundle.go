package main

// "burG" bundles: the container of .swatchbin/.modelbin files and of the
// records inside track GeoChunk minizips.
//
//   0x00 "burG", u8 1, u8 1, u16 0
//   0x08 u32 header size (= offset of the first blob's data)
//   0x0c u32 total size
//   0x10 u32 blob count
//   0x14 blob table, 0x18 bytes each:
//        tag[4], u8 major, u8 minor, u16 metadata count,
//        u32 metadata offset, u32 data offset, u32 data size, u32 data size again
//   metadata records: tag[4], u16 (payload size << 4 | low bits), u16, payload

import (
	"encoding/binary"
	"errors"
	"fmt"
)

var le = binary.LittleEndian

type meta struct {
	tag     string
	lo4     uint16 // low 4 bits of the size word
	word2   uint16
	payload []byte
}

type blob struct {
	tag          string
	major, minor byte
	metas        []meta
	data         []byte
	size2        uint32
}

type bundle struct {
	blobs []blob
}

func parseBundle(b []byte) (*bundle, error) {
	// burG 1.0 (Forza Horizon 2) keeps the blob count as a u16 at 6 and starts the blob table at 0x10; the table itself is the same.
	if len(b) < 0x14 || string(b[:4]) != "burG" || b[4] != 1 || b[5] > 1 {
		return nil, errors.New("not a burG 1.0 or 1.1 bundle")
	}
	n, table := int(le.Uint32(b[0x10:])), 0x14
	if b[5] == 0 {
		n, table = int(le.Uint16(b[6:])), 0x10
	}
	bu := &bundle{}
	for i := 0; i < n; i++ {
		e := table + i*0x18
		if e+0x18 > len(b) {
			return nil, errors.New("blob table past end")
		}
		bl := blob{tag: string(b[e : e+4]), major: b[e+4], minor: b[e+5]}
		mc := int(le.Uint16(b[e+6:]))
		mo := int(le.Uint32(b[e+8:]))
		do := int(le.Uint32(b[e+12:]))
		ds := int(le.Uint32(b[e+16:]))
		bl.size2 = le.Uint32(b[e+20:])
		for k := 0; k < mc; k++ {
			if mo+8 > len(b) {
				return nil, errors.New("metadata past end")
			}
			w := le.Uint16(b[mo+4:])
			sz := int(w >> 4)
			if mo+8+sz > len(b) {
				return nil, errors.New("metadata payload past end")
			}
			bl.metas = append(bl.metas, meta{tag: string(b[mo : mo+4]), lo4: w & 0xf, word2: le.Uint16(b[mo+6:]), payload: b[mo+8 : mo+8+sz]})
			mo += 8 + sz
		}
		if do+ds > len(b) {
			return nil, fmt.Errorf("blob %s data past end", bl.tag)
		}
		bl.data = b[do : do+ds]
		bu.blobs = append(bu.blobs, bl)
	}
	return bu, nil
}

// serialize lays the bundle out the way the cooker does: blob table, then all
// metadata in blob order, then each blob's data in order, no padding.
func (bu *bundle) serialize() []byte {
	n := len(bu.blobs)
	metaStart := 0x14 + n*0x18
	hdr := metaStart
	for _, bl := range bu.blobs {
		for _, m := range bl.metas {
			hdr += 8 + len(m.payload)
		}
	}
	total := hdr
	for _, bl := range bu.blobs {
		total += len(bl.data)
	}
	out := make([]byte, total)
	copy(out, "burG")
	out[4], out[5] = 1, 1
	le.PutUint32(out[8:], uint32(hdr))
	le.PutUint32(out[0xc:], uint32(total))
	le.PutUint32(out[0x10:], uint32(n))
	mo, do := metaStart, hdr
	for i, bl := range bu.blobs {
		e := 0x14 + i*0x18
		copy(out[e:], bl.tag)
		out[e+4], out[e+5] = bl.major, bl.minor
		le.PutUint16(out[e+6:], uint16(len(bl.metas)))
		le.PutUint32(out[e+8:], uint32(mo))
		le.PutUint32(out[e+12:], uint32(do))
		le.PutUint32(out[e+16:], uint32(len(bl.data)))
		le.PutUint32(out[e+20:], uint32(len(bl.data)))
		for _, m := range bl.metas {
			copy(out[mo:], m.tag)
			le.PutUint16(out[mo+4:], uint16(len(m.payload))<<4|m.lo4)
			le.PutUint16(out[mo+6:], m.word2)
			copy(out[mo+8:], m.payload)
			mo += 8 + len(m.payload)
		}
		copy(out[do:], bl.data)
		do += len(bl.data)
	}
	return out
}
