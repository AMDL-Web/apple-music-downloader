package wrapperhost

import (
	"bytes"
	"io"
	"os"
	"strconv"
	"strings"
)

// serveArgs are lite's arguments after a login's --login user:pass.
func serveArgs() []string {
	return []string{"--base-dir", "/data", "--host", "0.0.0.0", "--port", strconv.Itoa(guestPort)}
}

// loginArgs are a login boot's arguments, one per line in its args file.
func loginArgs(user, pass string) []string {
	return append([]string{"--login", user + ":" + pass}, serveArgs()...)
}

// maxCredentialLine bounds the user:pass line scrubLogin looks across.
const maxCredentialLine = 4096

// scrubLogin zeroes, in place, every copy of a login's arguments on the guest
// disk at path, and returns how many it found. Upstream's init copies the
// arguments to /data/.lite-qemu-args and deletes that file on the next boot,
// which frees its blocks without clearing them. A copy is "--login\n", one
// line of at most maxCredentialLine bytes, then exactly the rest of
// loginArgs. The guest must not be running.
func scrubLogin(path string) (int, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return 0, err
	}
	defer f.Close()
	head := []byte("--login\n")
	tail := []byte("\n" + strings.Join(serveArgs(), "\n") + "\n")
	const chunk = 1 << 20
	overlap := len(head) + maxCredentialLine + len(tail)
	buf := make([]byte, chunk+overlap)
	found := 0
	for off := int64(0); ; off += chunk {
		n, err := f.ReadAt(buf, off)
		if err != nil && err != io.EOF {
			return found, err
		}
		b := buf[:n]
		// A copy that starts in the overlap is read whole with the next chunk.
		for i := 0; i < min(n, chunk); {
			at := bytes.Index(b[i:min(n, chunk)], head)
			if at < 0 {
				break
			}
			start := i + at
			line := start + len(head)
			end := bytes.IndexByte(b[line:min(n, line+maxCredentialLine+1)], '\n')
			if end >= 0 && bytes.HasPrefix(b[line+end:], tail) {
				stop := line + end + len(tail)
				if _, err := f.WriteAt(make([]byte, stop-start), off+int64(start)); err != nil {
					return found, err
				}
				clear(b[start:stop])
				found++
				i = stop
				continue
			}
			i = start + 1
		}
		if n < len(buf) {
			break
		}
	}
	return found, f.Sync()
}
