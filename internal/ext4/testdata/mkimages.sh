#!/usr/bin/env bash
# Rebuilds the ext4 test images in this directory with e2fsprogs (mke2fs,
# debugfs). See README.md for what each image exercises.
#
# usage: ./mkimages.sh [OUTDIR]     (default: this directory)
#
# The file contents come from the committed images, so a rebuild keeps the
# sizes and SHA-256 sums that ext4_test.go checks; only the layout is redone.
# Block numbers, UUIDs and timestamps can differ from the committed images
# unless the same e2fsprogs version is used (the committed ones: 1.47).
set -euo pipefail

here=$(cd "$(dirname "$0")" && pwd)
out=$(mkdir -p "${1:-$here}" && cd "${1:-$here}" && pwd)
work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT

for tool in mke2fs debugfs gzip; do
	command -v "$tool" >/dev/null || { echo "mkimages.sh: $tool not found (install e2fsprogs)" >&2; exit 1; }
done

# Fixed times and seeds so two runs with the same e2fsprogs give the same bytes.
export E2FSPROGS_FAKE_TIME=1793353432 SOURCE_DATE_EPOCH=1793353432
uuid=6b0c1a54-6c1b-4a4e-9a5e-0f7a3d2c1b00

# 1. The source tree: wrapper-lite's data directory with a made-up token,
#    plus the extra files some images add, all taken from the committed images.
img() { gzip -dc "$here/$1.img.gz" >"$work/$1.img"; echo "$work/$1.img"; }
dump() { # dump IMAGE PATH OUT; debugfs dumps a whole block for inline files, so trim to i_size
	debugfs -R "dump $2 $3" "$1" 2>/dev/null
	truncate -s "$(debugfs -R "stat $2" "$1" 2>/dev/null | sed -n 's/.*Size: \([0-9]*\)$/\1/p' | head -1)" "$3"
}
src=$work/src
mkdir -p "$src"
debugfs -R "rdump / $src" "$(img std4k)" 2>/dev/null
rm -rf "$src/lost+found"
extras=$work/extras
mkdir -p "$extras"
dump "$(img deepextent)" /big.bin "$extras/big.bin"
dump "$(img ext2)" /blockmap.bin "$extras/blockmap.bin"
dump "$(img ext2)" /blockmap_small.bin "$extras/blockmap_small.bin"
dump "$(img inline)" /tiny.txt "$extras/tiny.txt"
dump "$(img inline)" /small100.txt "$extras/small100.txt"

tree() { # tree NAME [extra files...]: a copy of the source tree plus extras
	local dir=$work/tree-$1
	shift
	rm -rf "$dir"
	cp -a "$src" "$dir"
	for f in "$@"; do cp -a "$extras/$f" "$dir/"; done
	echo "$dir"
}

# Feature lists are spelled out ("none" first) so the host's mke2fs.conf
# defaults do not leak in.
ext4="has_journal,ext_attr,resize_inode,dir_index,filetype,extent,flex_bg,sparse_super,large_file,huge_file,dir_nlink,extra_isize"
std="$ext4,64bit,metadata_csum" # the features of upstream wrapper-lite's data.img

mk() { # mk NAME SIZE FEATURES [mke2fs args...]  (SIZE like 8M)
	local name=$1 size=$2 features=$3
	shift 3
	rm -f "$work/$name.out"
	mke2fs -q -F -U "$uuid" -E hash_seed="$uuid" -O "none,$features" "$@" "$work/$name.out" "$size"
}

finish() { gzip -9n <"$work/$1.out" >"$out/$1.img.gz"; }

# 2. The images.
#    std4k      ext4, 4 KiB blocks, the features of upstream's data.img
mk std4k 8M "$std" -b 4096 -N 2048 -d "$(tree std4k)"; finish std4k
#    std1k      the same with 1 KiB blocks
mk std1k 4M "$std" -b 1024 -N 1024 -d "$(tree std1k)"; finish std1k
#    nocsum32   no 64bit and no metadata_csum: 32-byte group descriptors
mk nocsum32 8M "$ext4" -b 4096 -N 2048 -d "$(tree nocsum32)"; finish nocsum32
#    modern4k   mke2fs defaults, adding orphan_file and metadata_csum_seed
mk modern4k 8M "$std,orphan_file,metadata_csum_seed" -b 4096 -N 2048 -d "$(tree modern4k)"; finish modern4k
#    bigdir     3000 more (empty) files in the root, which is htree-indexed
dir=$(tree bigdir)
for i in $(seq -f %05g 1 3000); do : >"$dir/f$i"; done
mk bigdir 8M "$std" -b 4096 -N 4096 -d "$dir"; finish bigdir
#    deepextent /big.bin in 400 one-block extents under an index node (depth
#               1): fill the disk with one-block files, free every other one,
#               then write big.bin into the holes.
mk deepextent 16M "$std" -b 4096 -N 4096 -d "$(tree deepextent)"
head -c 4096 /dev/zero | tr '\0' 'x' >"$work/block" # not zeros: debugfs writes those as holes
{
	echo "mkdir /fill"
	echo "cd /fill"
	for i in $(seq -f %05g 0 2751); do echo "write $work/block f$i"; done
	for i in $(seq -f %05g 0 2 2751); do echo "rm f$i"; done
	echo "cd /"
	echo "write $extras/big.bin big.bin"
} >"$work/deepextent.cmds"
debugfs -w -f "$work/deepextent.cmds" "$work/deepextent.out" >/dev/null 2>&1
finish deepextent
#    ext2       block maps, /blockmap.bin through a double indirect block
mk ext2 4M "ext_attr,resize_inode,dir_index,filetype,sparse_super,large_file" -b 1024 -N 1024 \
	-d "$(tree ext2 blockmap.bin blockmap_small.bin)"
finish ext2
#    inline     inline_data: /tiny.txt and the /mpl_db directory are inline
mk inline 8M "$std,inline_data" -b 4096 -N 2048 -d "$(tree inline tiny.txt small100.txt)"; finish inline
#    nofile     no MUSIC_TOKEN
dir=$(tree nofile)
rm "$dir/MUSIC_TOKEN"
mk nofile 8M "$std" -b 4096 -N 2048 -d "$dir"; finish nofile

echo "wrote $(ls "$out"/*.img.gz | wc -l) images to $out"
