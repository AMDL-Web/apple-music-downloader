// Package ext4 reads one regular file out of an ext4 filesystem image without
// mounting it. The managed wrapper's account disk is such an image, owned by a
// running guest, and the backend needs a file from it.
//
// Only what the filesystem has written in place is read; the journal is not
// replayed. A change the kernel has not yet written back, typically one under
// half a minute old, is not seen, and the version before it is. Nothing is
// cached, so each call sees the image as it is then.
package ext4

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"strings"
)

var (
	// ErrNotExt4 means the image has no ext2/3/4 superblock.
	ErrNotExt4 = errors.New("not an ext4 filesystem")
	// ErrUnsupported names a feature this reader does not decode, rather
	// than guessing at it.
	ErrUnsupported = errors.New("unsupported ext4 feature")
	// ErrCorrupt means a structure points outside the image or contradicts
	// itself; a live image read while being written can show this.
	ErrCorrupt = errors.New("corrupt ext4 filesystem")
	// ErrTooLarge means the file is larger than the caller's limit.
	ErrTooLarge = errors.New("file larger than the limit")
)

// Incompatible features: those a reader must understand to read correctly.
const (
	incompatFiletype   = 0x2
	incompatRecover    = 0x4 // journal needs replay; in-place data is read as is
	incompatExtents    = 0x40
	incompat64Bit      = 0x80
	incompatMMP        = 0x100
	incompatFlexBG     = 0x200
	incompatEAInode    = 0x400
	incompatCsumSeed   = 0x2000
	incompatLargeDir   = 0x4000
	incompatInlineData = 0x8000 // rejected per inode, below

	incompatKnown = incompatFiletype | incompatRecover | incompatExtents | incompat64Bit | incompatMMP |
		incompatFlexBG | incompatEAInode | incompatCsumSeed | incompatLargeDir | incompatInlineData
)

const (
	rootInode       = 2
	inodeFlagExtent = 0x80000
	inodeFlagInline = 0x10000000
	modeType        = 0xf000
	modeDir         = 0x4000
	modeRegular     = 0x8000
	extentMagic     = 0xf30a
	maxExtentDepth  = 5
	// maxDirBytes bounds a directory read into memory.
	maxDirBytes  = 16 << 20
	maxPathDepth = 64
)

type image struct {
	r              io.ReaderAt
	size           int64
	blockSize      int64
	blocks         uint64
	firstDataBlock uint64
	inodesCount    uint32
	inodesPerGroup uint32
	inodeSize      int64
	descSize       int64
	groups         uint64
}

type inode struct {
	mode  uint16
	size  uint64
	flags uint32
	block [60]byte
}

// ReadFile returns the contents of the regular file at name, a slash-separated
// path from the root of the filesystem in r, which is size bytes long. A file
// over max bytes is ErrTooLarge. A missing path is fs.ErrNotExist. Symbolic
// links are not followed.
func ReadFile(r io.ReaderAt, size int64, name string, max int64) ([]byte, error) {
	img, err := open(r, size)
	if err != nil {
		return nil, err
	}
	parts := strings.FieldsFunc(name, func(c rune) bool { return c == '/' })
	if len(parts) == 0 || len(parts) > maxPathDepth {
		return nil, fmt.Errorf("ext4: bad path %q", name)
	}
	ino := uint32(rootInode)
	for i, part := range parts {
		dir, err := img.inode(ino)
		if err != nil {
			return nil, err
		}
		if dir.mode&modeType != modeDir {
			return nil, fmt.Errorf("%s: %w", "/"+strings.Join(parts[:i], "/"), fs.ErrNotExist)
		}
		if ino, err = img.lookup(dir, part); err != nil {
			return nil, err
		}
		if ino == 0 {
			return nil, fmt.Errorf("%s: %w", name, fs.ErrNotExist)
		}
	}
	f, err := img.inode(ino)
	if err != nil {
		return nil, err
	}
	if f.mode&modeType != modeRegular {
		return nil, fmt.Errorf("%s: not a regular file", name)
	}
	if f.size > uint64(max) {
		return nil, fmt.Errorf("%s: %d bytes: %w", name, f.size, ErrTooLarge)
	}
	return img.contents(f)
}

func open(r io.ReaderAt, size int64) (*image, error) {
	sb := make([]byte, 1024)
	if size < 2048 {
		return nil, ErrNotExt4
	}
	if _, err := r.ReadAt(sb, 1024); err != nil && err != io.EOF {
		return nil, err
	}
	le := binary.LittleEndian
	if le.Uint16(sb[0x38:]) != 0xef53 {
		return nil, ErrNotExt4
	}
	logBlock := le.Uint32(sb[0x18:])
	if logBlock > 6 {
		return nil, fmt.Errorf("%w: block size 1024<<%d", ErrCorrupt, logBlock)
	}
	img := &image{
		r:              r,
		size:           size,
		blockSize:      1024 << logBlock,
		blocks:         uint64(le.Uint32(sb[0x04:])),
		firstDataBlock: uint64(le.Uint32(sb[0x14:])),
		inodesCount:    le.Uint32(sb[0x00:]),
		inodesPerGroup: le.Uint32(sb[0x28:]),
		inodeSize:      128,
		descSize:       32,
	}
	if le.Uint32(sb[0x4c:]) >= 1 {
		img.inodeSize = int64(le.Uint16(sb[0x58:]))
	}
	incompat := le.Uint32(sb[0x60:])
	if extra := incompat &^ incompatKnown; extra != 0 {
		return nil, fmt.Errorf("%w: incompat 0x%x", ErrUnsupported, extra)
	}
	if incompat&incompat64Bit != 0 {
		img.blocks |= uint64(le.Uint32(sb[0x150:])) << 32
		img.descSize = int64(le.Uint16(sb[0xfe:]))
		if img.descSize < 64 {
			return nil, fmt.Errorf("%w: 64-bit group descriptor of %d bytes", ErrCorrupt, img.descSize)
		}
	}
	perGroup := uint64(le.Uint32(sb[0x20:]))
	switch {
	case img.inodeSize < 128 || img.inodeSize > img.blockSize || img.inodeSize&(img.inodeSize-1) != 0,
		img.descSize > img.blockSize, img.inodesPerGroup == 0, perGroup == 0,
		img.firstDataBlock >= img.blocks:
		return nil, ErrCorrupt
	}
	img.groups = (img.blocks - img.firstDataBlock + perGroup - 1) / perGroup
	return img, nil
}

// read returns n bytes at off, which must lie inside the image.
func (img *image) read(off int64, n int64) ([]byte, error) {
	if off < 0 || n < 0 || off > img.size || n > img.size-off {
		return nil, ErrCorrupt
	}
	b := make([]byte, n)
	if _, err := img.r.ReadAt(b, off); err != nil && err != io.EOF {
		return nil, err
	}
	return b, nil
}

func (img *image) block(n uint64) ([]byte, error) {
	if n >= img.blocks || n > uint64(img.size/img.blockSize) {
		return nil, fmt.Errorf("%w: block %d", ErrCorrupt, n)
	}
	return img.read(int64(n)*img.blockSize, img.blockSize)
}

func (img *image) inode(ino uint32) (inode, error) {
	if ino == 0 || ino > img.inodesCount {
		return inode{}, fmt.Errorf("%w: inode %d", ErrCorrupt, ino)
	}
	group := uint64(ino-1) / uint64(img.inodesPerGroup)
	index := int64(ino-1) % int64(img.inodesPerGroup)
	if group >= img.groups {
		return inode{}, fmt.Errorf("%w: inode %d", ErrCorrupt, ino)
	}
	gdt := int64(img.firstDataBlock+1)*img.blockSize + int64(group)*img.descSize
	desc, err := img.read(gdt, img.descSize)
	if err != nil {
		return inode{}, err
	}
	le := binary.LittleEndian
	table := uint64(le.Uint32(desc[0x08:]))
	if img.descSize >= 64 {
		table |= uint64(le.Uint32(desc[0x28:])) << 32
	}
	if table >= img.blocks {
		return inode{}, fmt.Errorf("%w: inode table of group %d", ErrCorrupt, group)
	}
	raw, err := img.read(int64(table)*img.blockSize+index*img.inodeSize, 128)
	if err != nil {
		return inode{}, err
	}
	in := inode{
		mode:  le.Uint16(raw[0x00:]),
		size:  uint64(le.Uint32(raw[0x04:])) | uint64(le.Uint32(raw[0x6c:]))<<32,
		flags: le.Uint32(raw[0x20:]),
	}
	copy(in.block[:], raw[0x28:0x28+60])
	return in, nil
}

// contents reads a file's bytes: holes and unwritten extents read as zeros.
func (img *image) contents(in inode) ([]byte, error) {
	if in.flags&inodeFlagInline != 0 {
		return nil, fmt.Errorf("%w: inline data", ErrUnsupported)
	}
	if in.size > uint64(img.size) {
		return nil, fmt.Errorf("%w: file of %d bytes in a %d-byte image", ErrCorrupt, in.size, img.size)
	}
	out := make([]byte, in.size)
	place := func(logical, physical, count uint64) error {
		for i := uint64(0); i < count; i++ {
			at := (logical + i) * uint64(img.blockSize)
			if at >= in.size {
				return nil
			}
			b, err := img.block(physical + i)
			if err != nil {
				return err
			}
			copy(out[at:], b)
		}
		return nil
	}
	if in.flags&inodeFlagExtent != 0 {
		budget := 4096
		return out, img.walkExtents(in.block[:], maxExtentDepth, &budget, place)
	}
	return out, img.walkBlockMap(in.block[:], in.size, place)
}

// walkExtents visits the initialized leaf extents of the tree rooted in node.
func (img *image) walkExtents(node []byte, depthLeft int, budget *int, place func(logical, physical, count uint64) error) error {
	le := binary.LittleEndian
	if len(node) < 12 || le.Uint16(node[0:]) != extentMagic {
		return fmt.Errorf("%w: extent header", ErrCorrupt)
	}
	entries := int(le.Uint16(node[2:]))
	depth := int(le.Uint16(node[6:]))
	if 12+12*entries > len(node) || depth > depthLeft {
		return fmt.Errorf("%w: extent node", ErrCorrupt)
	}
	for i := 0; i < entries; i++ {
		*budget--
		if *budget < 0 {
			return fmt.Errorf("%w: extent tree too large", ErrCorrupt)
		}
		e := node[12+12*i:]
		if depth == 0 {
			length := uint64(le.Uint16(e[4:]))
			if length > 32768 {
				continue // unwritten: reads as zeros
			}
			start := uint64(le.Uint16(e[6:]))<<32 | uint64(le.Uint32(e[8:]))
			if err := place(uint64(le.Uint32(e[0:])), start, length); err != nil {
				return err
			}
			continue
		}
		child, err := img.block(uint64(le.Uint32(e[4:])) | uint64(le.Uint16(e[8:]))<<32)
		if err != nil {
			return err
		}
		if err := img.walkExtents(child, depth-1, budget, place); err != nil {
			return err
		}
	}
	return nil
}

// walkBlockMap visits the blocks of an ext2/3-style file: twelve direct
// pointers, then single, double and triple indirect blocks.
func (img *image) walkBlockMap(ptrs []byte, size uint64, place func(logical, physical, count uint64) error) error {
	le := binary.LittleEndian
	perBlock := uint64(img.blockSize / 4)
	want := (size + uint64(img.blockSize) - 1) / uint64(img.blockSize)
	var walk func(ptr uint64, level int, logical uint64) error
	walk = func(ptr uint64, level int, logical uint64) error {
		if ptr == 0 || logical >= want {
			return nil
		}
		if level == 0 {
			return place(logical, ptr, 1)
		}
		b, err := img.block(ptr)
		if err != nil {
			return err
		}
		span := uint64(1)
		for i := 1; i < level; i++ {
			span *= perBlock
		}
		for i := uint64(0); i < perBlock; i++ {
			if err := walk(uint64(le.Uint32(b[4*i:])), level-1, logical+i*span); err != nil {
				return err
			}
		}
		return nil
	}
	logical := uint64(0)
	for i := 0; i < 15; i++ {
		level := max(0, i-11)
		if err := walk(uint64(le.Uint32(ptrs[4*i:])), level, logical); err != nil {
			return err
		}
		span := uint64(1)
		for j := 0; j < level; j++ {
			span *= perBlock
		}
		logical += span
	}
	return nil
}

// lookup returns the inode of the entry called name in dir, or 0. It scans
// every block linearly, which also reads an htree-indexed directory: its
// index blocks look like empty entries.
func (img *image) lookup(dir inode, name string) (uint32, error) {
	if dir.size > maxDirBytes {
		return 0, fmt.Errorf("%w: directory of %d bytes", ErrTooLarge, dir.size)
	}
	data, err := img.contents(dir)
	if err != nil {
		return 0, err
	}
	le := binary.LittleEndian
	bs := int(img.blockSize)
	for blk := 0; blk+bs <= len(data); blk += bs {
		for off := 0; off < bs; {
			if bs-off < 8 {
				return 0, fmt.Errorf("%w: directory entry", ErrCorrupt)
			}
			e := data[blk+off:]
			recLen := int(le.Uint16(e[4:]))
			nameLen := int(e[6])
			if recLen < 8 || recLen%4 != 0 || recLen > bs-off || nameLen > recLen-8 {
				return 0, fmt.Errorf("%w: directory entry", ErrCorrupt)
			}
			if ino := le.Uint32(e[0:]); ino != 0 && string(e[8:8+nameLen]) == name {
				return ino, nil
			}
			off += recLen
		}
	}
	return 0, nil
}
