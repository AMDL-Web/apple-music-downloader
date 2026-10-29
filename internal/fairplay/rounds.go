package fairplay

// t4 and ctxb are the table readers that Temari's rounds_gen.rs implements
// with unchecked indexing. Every caller passes a constant base and a byte-wide
// index, so all reads stay inside ctxTable, and the compiler drops the bounds
// check wherever it can see that. t4 reads the little-endian words
// ParseTemplate decoded, which needs a 4-aligned base; TestT4BasesAligned
// checks every base.

func t4(ctx *ctxTable, off uint, idx uint32) uint32 {
	return ctx.w[off>>2+uint(idx&0xFF)]
}

func ctxb(ctx *ctxTable, off uint) uint32 {
	return uint32(ctx.b[off])
}
