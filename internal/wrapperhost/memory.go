package wrapperhost

import (
	"encoding/json"
	"errors"
	"io/fs"
	"os"

	"amdl/internal/ext4"
)

// MaxAccounts is the manager's account cap, including degraded instances.
const MaxAccounts = 10

// guestBaseMemoryMB is the guest without accounts: the kernel, the
// initramfs held in RAM, init, and the manager, which was measured at 15 to
// 50 MiB and downloads the 50 MB wrapper-lite zip into memory on first boot.
const guestBaseMemoryMB = 384

// guestAccountMemoryMB is one account's launcher and wrapper-lite. Measured
// on Cloudflare Containers: about 56 MiB RSS at start and about 69 MiB after
// decrypting 11 tracks; 96 leaves headroom above that.
const guestAccountMemoryMB = 96

// GuestMemoryMB sizes the next boot; a running guest is never hot-resized.
func GuestMemoryMB(accounts int) int {
	return guestBaseMemoryMB + guestAccountMemoryMB*max(0, min(accounts, MaxAccounts))
}

// accountCount reads only the registry while QEMU is stopped. The manager
// runs from /data and stores data/instances.json there; HTTP is unavailable
// until the guest has booted. Tokens are always read through HTTP instead.
func (h Home) accountCount() (int, error) {
	f, err := os.Open(h.ImagePath())
	if err != nil {
		return 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, err
	}
	raw, err := ext4.ReadFile(f, st.Size(), "/data/instances.json", 64<<10)
	if errors.Is(err, fs.ErrNotExist) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return registryCount(raw)
}

func registryCount(raw []byte) (int, error) {
	var instances []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(raw, &instances); err != nil {
		return 0, errors.New("invalid manager registry")
	}
	return min(len(instances), MaxAccounts), nil
}
