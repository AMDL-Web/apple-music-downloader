package fairplay

import "encoding/binary"

// t4 and ctxb are the table readers that Temari's rounds_gen.rs implements
// with unchecked indexing. Every caller passes a constant base and a byte-wide
// index, so all reads stay inside ctxTable.

func t4(ctx *ctxTable, off uint, idx uint32) uint32 {
	i := off + uint(idx&0xFF)<<2
	return binary.LittleEndian.Uint32(ctx[i : i+4])
}

func ctxb(ctx *ctxTable, off uint) uint32 {
	return uint32(ctx[off])
}
