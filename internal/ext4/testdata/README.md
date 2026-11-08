# ext4 test images

Small gzipped file system images read by `ext4_test.go`. All hold the same
tree, shaped like wrapper-lite's data directory with a made-up token
(`MUSIC_TOKEN`, `STOREFRONT_ID`, `adi.pb`, `token_cache.json`, `mpl_db/`,
`data/com.example/files/deep/nested.txt`); each varies one layout feature:

| image | what it exercises |
|---|---|
| std4k | ext4, 4 KiB blocks, the features of upstream's data.img |
| std1k | the same with 1 KiB blocks |
| nocsum32 | no 64bit and no metadata_csum: 32-byte group descriptors |
| modern4k | adds orphan_file and metadata_csum_seed |
| bigdir | 3000 more empty files in the root, which is htree-indexed |
| deepextent | /big.bin in 400 one-block extents under an index node (depth 1) |
| ext2 | block maps, /blockmap.bin through a double indirect block |
| inline | inline_data: /tiny.txt and the /mpl_db directory are inline |
| nofile | no MUSIC_TOKEN |

## Rebuilding

    ./mkimages.sh [OUTDIR]

needs e2fsprogs (mke2fs, debugfs; the committed images were made with 1.47).
The script takes the file contents from the committed images, so the sizes and
SHA-256 sums in `ext4_test.go` stay valid; it redoes only the layout, with
fixed UUIDs, hash seed and time, so two runs give identical bytes. With
e2fsprogs 1.47.4 the rebuilt images pass the tests and have the same extent
layout as the committed ones (UUIDs and timestamps differ). To add a layout,
add an `mk` line to the script and a case to `TestReadFile`.
