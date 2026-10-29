package fairplay

// DecryptSample decrypts sample in place: the 16-byte-aligned prefix runs
// through the white-box rounds with the CBC chain, a shorter tail is left as
// is. Safe for concurrent use; each call starts from the template state.
func (t *Template) DecryptSample(sample []byte) {
	n := len(sample) / 16
	if n == 0 {
		return
	}
	ct := sample[:n*16]
	st := t.stInit
	for bi := 0; bi < n; bi++ {
		t.decryptBlock(&st, ct, bi)
	}
}

// decryptBlock is Temari's process_block. The rounds read ciphertext only
// from block bi, so writing each plaintext block back over ct is safe;
// tools/temaridiff checks that against Temari's out-of-place decrypt.
func (t *Template) decryptBlock(st *state, ct []byte, bi int) {
	regs := round1Regs{
		rdi: 0x1EB2C6B4 ^ uint32(bi)<<4,
		rsi: 0x8 + uint32(bi)<<4,
		rdx: t.r1.rdx,
		rcx: t.r1.rcx,
		r9:  t.r1.r9,
		rax: t.r1.rax,
		rbp: t.r1.rbp,
	}
	ctx := &t.ctx

	mid := round1_mid(ctx, st, ct, &regs)
	r2 := round1_tail(ctx, st, mid.rax, mid.r13&0xFF, mid.r15&0xFF, mid.r8&0xFF, mid.r14&0xFF)
	r2v := round2_sub6400(ctx, st, r2.rdi, r2.rsi, r2.rdx, r2.rcx, r2.r8, r2.r9, r2.rax, r2.rbx,
		r2.r10, r2.r11, r2.r13, r2.r14, r2.r15, 0)

	// cp12 holds st[0x48], st[0x250], st[0x290] as captured inside round 2.
	cp12 := r2v.cp12
	r8p := cp12[2] ^ st[0x5B8] ^ cp12[1]
	v6 := t4(ctx, 0x46a0, st[0x390]^0x2B) ^
		st[0x5F0] ^
		t4(ctx, 0x4ac0, ((r8p>>24)&0xFF)^0x29) ^
		t4(ctx, 0x2ff0, ((st[0x298]>>16)&0xFF)^0xD6)
	v9 := t4(ctx, 0x4ac0, cp12[0]) ^
		st[0x540] ^
		t4(ctx, 0x2ff0, ((r2v.v171>>16)&0xFF)^0x69)
	v11 := t4(ctx, 0x3950, (st[0x298]&0xFF)^0x57) ^
		st[0x538] ^
		t4(ctx, 0x46a0, ((r8p>>8)&0xFF)^0x2F)

	r3v := round3_sub8000(ctx, st, st[0x270], r2v.v189, r8p&0xFF, (r8p>>16)&0xFFFF, st[0x280],
		v6, (r8p>>16)&0xFF, v9, r2v.v187&0xFF, v11, r2v.v189)

	// Temari writes into a zeroed buffer; bytes the reconstruction misses stay 0.
	var blk [16]byte
	reconstructBlock(&r3v.pt, &blk)
	copy(ct[bi*16:bi*16+16], blk[:])

	st[0x108] = st[0x180]
	st[0x220] += 0x10
}

// reconstructBlock places each plaintext byte at its offset relative to the
// lowest offset in pt.
func reconstructBlock(pt *[16]tupleUint32Uint8, blk *[16]byte) {
	base := pt[0].f0
	for _, p := range pt {
		if p.f0 < base {
			base = p.f0
		}
	}
	for _, p := range pt {
		if idx := p.f0 - base; idx < 16 {
			blk[idx] = p.f1
		}
	}
}
