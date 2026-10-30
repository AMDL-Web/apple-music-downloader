package wrapperhost

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// QEMU is the x86_64 system emulator the guest runs in: the guest is x86_64
// whatever the host.
const qemuName = "qemu-system-x86_64"

// FindQEMU returns the QEMU to run: configured when set, else the first of
// PATH and the places installers put it.
func FindQEMU(configured, goos string, lookPath func(string) (string, error), exists func(string) bool) (string, error) {
	if configured != "" {
		if exists(configured) {
			return configured, nil
		}
		return "", fmt.Errorf("the configured QEMU %s does not exist", configured)
	}
	name := qemuName
	var dirs []string
	switch goos {
	case "darwin":
		dirs = []string{"/opt/homebrew/bin", "/usr/local/bin", "/opt/local/bin"}
	case "windows":
		name += ".exe"
		dirs = []string{`C:\Program Files\qemu`}
	default:
		dirs = []string{"/usr/bin", "/usr/local/bin"}
	}
	if p, err := lookPath(name); err == nil {
		return p, nil
	}
	for _, d := range dirs {
		if p := filepath.Join(d, name); exists(p) {
			return p, nil
		}
	}
	return "", ErrNoQEMU
}

// ErrNoQEMU means QEMU is not installed.
var ErrNoQEMU = fmt.Errorf("%s is not installed", qemuName)

// QEMUInstall is how to install QEMU on goos: a command, and whether amdl may
// run it for the user after they agree (it needs no sudo).
func QEMUInstall(goos string, has func(string) bool) (command string, runnable bool) {
	switch goos {
	case "darwin":
		if has("brew") {
			return "brew install qemu", true
		}
		return "install Homebrew from https://brew.sh, then brew install qemu", false
	case "windows":
		if has("winget") {
			return "winget install --id SoftwareFreedomConservancy.QEMU -e", true
		}
		return "install QEMU from https://www.qemu.org/download/#windows", false
	}
	for _, w := range []struct{ manager, cmd string }{
		{"apt-get", "sudo apt-get install qemu-system-x86"},
		{"dnf", "sudo dnf install qemu-system-x86"},
		{"pacman", "sudo pacman -S qemu-system-x86"},
		{"apk", "sudo apk add qemu-system-x86_64"},
	} {
		if has(w.manager) {
			return w.cmd, false
		}
	}
	return "install QEMU from https://www.qemu.org/download/#linux", false
}

// Accel picks the accelerator for the x86_64 guest: KVM on Linux when
// /dev/kvm opens, WHPX on Windows, HVF on an Intel Mac (an x86_64 guest on an
// x86_64 host), and TCG, plain emulation, otherwise. HVF cannot run an x86_64
// guest on Apple Silicon, so those Macs emulate.
func Accel(goos, goarch string, kvmUsable bool) string {
	switch {
	case goos == "linux" && kvmUsable:
		return "kvm"
	case goos == "windows":
		return "whpx"
	case goos == "darwin" && goarch == "amd64":
		return "hvf"
	}
	return "tcg"
}

// KVMUsable reports whether this process may open /dev/kvm.
func KVMUsable() bool {
	if runtime.GOOS != "linux" {
		return false
	}
	f, err := os.OpenFile("/dev/kvm", os.O_RDWR, 0)
	if err != nil {
		return false
	}
	f.Close()
	return true
}

// RunOpts describes one boot of the guest.
type RunOpts struct {
	Accel          string
	Kernel, Initrd string
	Image          string
	HostPort       int
	// GuestPort is where the guest's lite listens: guestPort for a boot
	// without arguments.
	GuestPort int
	MemoryMB  int
	CPUs      int
	// ArgsFile, when set, passes lite's arguments through fw_cfg; only a
	// login boot uses it.
	ArgsFile string
}

// Args are QEMU's arguments for o: buildQemuArgs of upstream's
// wrapper-lite-qemu.cpp at bde3335, for a QEMU the user installed (so no -L
// or library paths), with the kernel command line fixed (never lite_args_b64)
// and the forwarded port bound to loopback only.
func Args(o RunOpts) []string {
	accel, cpu := o.Accel, "max"
	switch o.Accel {
	case "kvm", "hvf":
		cpu = "host"
	case "whpx":
		accel, cpu = "whpx,kernel-irqchip=off", "qemu64-v1"
	}
	args := []string{
		"-accel", accel, "-cpu", cpu,
		"-m", strconv.Itoa(o.MemoryMB), "-smp", strconv.Itoa(o.CPUs),
		"-kernel", o.Kernel, "-initrd", o.Initrd,
		"-append", "console=ttyS0 quiet net.ifnames=0 biosdevname=0",
		"-display", "none", "-serial", "stdio", "-no-reboot",
		"-nic", fmt.Sprintf("user,model=e1000,hostfwd=tcp:127.0.0.1:%d-:%d", o.HostPort, o.GuestPort),
		"-drive", "file=" + optValue(o.Image) + ",format=raw,if=virtio",
	}
	if o.ArgsFile != "" {
		args = append(args, "-fw_cfg", "name=lite_args,file="+optValue(o.ArgsFile))
	}
	return args
}

// optValue escapes a path inside a QEMU option list, where a comma separates
// options and ",," is a literal comma.
func optValue(s string) string { return strings.ReplaceAll(s, ",", ",,") }
