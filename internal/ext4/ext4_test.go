package ext4

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

// The testdata images were made with mke2fs -d (e2fsprogs 1.47) from one
// small tree shaped like wrapper-lite's data directory, with a made-up token.
//
//	std4k      ext4, 4 KiB blocks, the features of upstream's data.img
//	std1k      the same with 1 KiB blocks
//	nocsum32   no 64bit and no metadata_csum: 32-byte group descriptors
//	modern4k   mke2fs defaults, adding orphan_file and metadata_csum_seed
//	bigdir     3000 more files in the root, which is htree-indexed
//	deepextent /big.bin in 400 extents under an index node (depth 1)
//	ext2       block maps, /blockmap.bin through a double indirect block
//	inline     inline_data: /tiny.txt and the /mpl_db directory are inline
//	nofile     no MUSIC_TOKEN
var files = map[string]struct {
	size   int
	sha256 string
}{
	"/MUSIC_TOKEN":        {244, "5c0c8438c1e7f56a6dbb654912c95188d3704fcd2540ba3db3a14bfec78a55bb"},
	"/STOREFRONT_ID":      {6, "e30cdb6f11b39dea6a17dbd92e78d81c2fe13639a25f93aaf42fb86eb7a09aec"},
	"/adi.pb":             {333, "b89d01e90df9566a3611175312d972b20796229b9f795fd631179fd144cf88c4"},
	"/big.bin":            {1638277, "a0e4008641c8d4b3d3a600d3f1688d5170eae833dca7fc147ca348cdc567f1f4"},
	"/blockmap.bin":       {307123, "69f8902bdc0c1ed2d43fe7f37dfe53df4b48c46e8b5db63f294306b977e92a9c"},
	"/blockmap_small.bin": {20480, "8fd77afa5c41dbbea5f13d4f659ae01a2dd5cdbf67d7ff2aff196b0233521b9a"},
	"/f01500":             {0, "e3b0c44298fc1c149afbf4c8996fb92427ae41e4649b934ca495991b7852b855"},
	"/mpl_db/a.db":        {140, "239a78f5c1a127927a112a90276702095e20a06c5ec452461a51a696d7e640fb"},
	"/mpl_db/b.db":        {2100, "f06df82a380856df427b8f0f2b7d9147c787f563fe47cd2dc45ec28cd6331733"},
	"/token_cache.json":   {587, "a5e62279298f419a7d583e8efda4cc498016d582140d801ee859779d8a226414"},
	"/data/com.example/files/deep/nested.txt": {25, "4af7deb11c3a6d7fb1ed09d368ef780c7e22ac854ae72f4d069bc473d56e4a55"},
}

var tree = []string{"/MUSIC_TOKEN", "/STOREFRONT_ID", "/adi.pb", "/mpl_db/a.db", "/mpl_db/b.db",
	"/token_cache.json", "/data/com.example/files/deep/nested.txt"}

func load(t testing.TB, name string) []byte {
	t.Helper()
	f, err := os.Open(filepath.Join("testdata", name+".img.gz"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	z, err := gzip.NewReader(f)
	if err != nil {
		t.Fatal(err)
	}
	img, err := io.ReadAll(z)
	if err != nil {
		t.Fatal(err)
	}
	return img
}

func read(img []byte, name string, max int64) ([]byte, error) {
	return ReadFile(bytes.NewReader(img), int64(len(img)), name, max)
}

func TestReadFile(t *testing.T) {
	cases := map[string][]string{
		"std4k":      tree,
		"std1k":      tree,
		"nocsum32":   tree,
		"modern4k":   tree,
		"bigdir":     append([]string{"/f01500"}, tree...),
		"deepextent": append([]string{"/big.bin"}, tree...),
		"ext2":       append([]string{"/blockmap.bin", "/blockmap_small.bin"}, tree...),
		"inline":     {"/MUSIC_TOKEN", "/adi.pb", "/token_cache.json"},
		"nofile":     tree[1:],
	}
	for image, paths := range cases {
		img := load(t, image)
		for _, p := range paths {
			got, err := read(img, p, 4<<20)
			if err != nil {
				t.Errorf("%s %s: %v", image, p, err)
				continue
			}
			sum := sha256.Sum256(got)
			if want := files[p]; len(got) != want.size || hex.EncodeToString(sum[:]) != want.sha256 {
				t.Errorf("%s %s: %d bytes, sha256 %x; want %d bytes, %s", image, p, len(got), sum, want.size, want.sha256)
			}
		}
	}
}

func TestReadFileErrors(t *testing.T) {
	std := load(t, "std4k")
	inline := load(t, "inline")
	for _, c := range []struct {
		img  []byte
		path string
		max  int64
		want error
	}{
		{load(t, "nofile"), "/MUSIC_TOKEN", 4096, fs.ErrNotExist},
		{std, "/nope", 4096, fs.ErrNotExist},
		{std, "/nope/MUSIC_TOKEN", 4096, fs.ErrNotExist},
		{std, "/MUSIC_TOKEN/x", 4096, fs.ErrNotExist},
		{std, "/MUSIC_TOKEN", 243, ErrTooLarge},
		{inline, "/tiny.txt", 4096, ErrUnsupported},
		{inline, "/mpl_db/a.db", 4096, ErrUnsupported},
		{make([]byte, 1<<20), "/MUSIC_TOKEN", 4096, ErrNotExt4},
	} {
		if _, err := read(c.img, c.path, c.max); !errors.Is(err, c.want) {
			t.Errorf("%s: %v, want %v", c.path, err, c.want)
		}
	}
	for _, p := range []string{"/", "", "/mpl_db"} {
		if _, err := read(std, p, 4096); err == nil {
			t.Errorf("%q: no error", p)
		}
	}
}

func TestUnknownIncompatFeature(t *testing.T) {
	img := load(t, "std4k")
	incompat := binary.LittleEndian.Uint32(img[1024+0x60:])
	binary.LittleEndian.PutUint32(img[1024+0x60:], incompat|0x10000) // encrypt
	if _, err := read(img, "/MUSIC_TOKEN", 4096); !errors.Is(err, ErrUnsupported) {
		t.Fatalf("got %v, want ErrUnsupported", err)
	}
}

// TestTornImage reads from images with one byte of metadata changed, as a
// read of a disk the guest is writing might see: errors are fine, a panic or
// a hang is not.
func TestTornImage(t *testing.T) {
	for _, name := range []string{"std4k", "std1k", "bigdir", "ext2"} {
		img := load(t, name)
		// The superblock, group descriptors, inode tables and the first
		// directory and extent blocks all sit in the first 1 MiB.
		for off := 1024; off < 1<<20; off += 97 {
			was := img[off]
			for _, v := range []byte{0x00, 0xff, was ^ 0x80} {
				img[off] = v
				_, _ = read(img, "/MUSIC_TOKEN", 4096)
				_, _ = read(img, "/data/com.example/files/deep/nested.txt", 4096)
			}
			img[off] = was
		}
	}
}

func FuzzReadFile(f *testing.F) {
	orig := load(f, "std1k")
	f.Add(uint32(1024+0x60), byte(0xff))
	f.Add(uint32(2048+8), byte(0x01))
	f.Fuzz(func(t *testing.T, off uint32, v byte) {
		img := bytes.Clone(orig)
		img[int(off)%len(img)] = v
		_, _ = read(img, "/mpl_db/b.db", 1<<20)
	})
}
