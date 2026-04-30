//go:build linux

package hidx

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

// linuxBackend walks /sys/bus/usb/devices to find every CM108-family device
// whose VID/PID matches, resolves the associated /dev/hidrawN node, and
// opens it with O_RDWR. HID transfers go through HIDIOCGINPUT/HIDIOCSOUTPUT
// ioctls — the same path openmanetd uses for the GPIO1 strap probe.
//
// No CGO. /sys is mandatory; if it is absent (e.g. running inside a chroot
// without /sys mounted), Enumerate returns an empty slice and a clear error.
type linuxBackend struct {
	sysRoot fs.FS
	devRoot string
}

func newBackend() Backend {
	return &linuxBackend{
		sysRoot: os.DirFS("/sys"),
		devRoot: "/dev",
	}
}

const usbDevicesDir = "bus/usb/devices"

func (b *linuxBackend) Enumerate(vendorID, productID uint16) ([]DeviceInfo, error) {
	entries, err := fs.ReadDir(b.sysRoot, usbDevicesDir)
	if err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return nil, fmt.Errorf("hidx: /sys/%s not present (is sysfs mounted?)", usbDevicesDir)
		}

		return nil, fmt.Errorf("hidx: read /sys/%s: %w", usbDevicesDir, err)
	}

	results := make([]DeviceInfo, 0, 4)

	for _, e := range entries {
		if !e.IsDir() {
			continue
		}

		name := e.Name()
		// Interface entries look like "1-1.2:1.0" — skip them; we want the
		// top-level USB device directories that hold idVendor/idProduct.
		if strings.ContainsRune(name, ':') {
			continue
		}

		devPath := usbDevicesDir + "/" + name

		vid, vidOK := readHexID(b.sysRoot, devPath+"/idVendor")
		if !vidOK {
			continue
		}

		if vendorID != 0 && vid != vendorID {
			continue
		}

		pid, pidOK := readHexID(b.sysRoot, devPath+"/idProduct")
		if !pidOK {
			continue
		}

		if productID != 0 && pid != productID {
			continue
		}

		hidrawName := findHidraw(b.sysRoot, devPath)
		if hidrawName == "" {
			// Match by IDs but no hidraw exposed — skip silently rather
			// than confuse the user with a device they can't talk to.
			continue
		}

		results = append(results, DeviceInfo{
			Path:             b.devRoot + "/" + hidrawName,
			SerialNumber:     strings.TrimSpace(readString(b.sysRoot, devPath+"/serial")),
			ProductName:      strings.TrimSpace(readString(b.sysRoot, devPath+"/product")),
			ManufacturerName: strings.TrimSpace(readString(b.sysRoot, devPath+"/manufacturer")),
			VendorID:         vid,
			ProductID:        pid,
		})
	}

	return results, nil
}

func (b *linuxBackend) Open(path string) (Transport, error) {
	// O_RDWR is required: HIDIOCGINPUT and HIDIOCSOUTPUT both encode the
	// _IOC_READ|_IOC_WRITE direction bits, so the hidraw driver enforces a
	// read/write open.
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("hidx: open %s: %w", path, err)
	}

	return &linuxDevice{f: f}, nil
}

// linuxDevice is one open hidraw fd plus the helpers to issue the two CM108B
// control transfers.
type linuxDevice struct {
	f *os.File
}

func (d *linuxDevice) GetInputReport(reportID byte, buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, errors.New("hidx: GetInputReport called with empty buffer")
	}

	buf[0] = reportID

	ioc := hidiocginput(uint32(len(buf)))
	if err := ioctlPtr(d.f.Fd(), ioc, unsafe.Pointer(&buf[0])); err != nil {
		return 0, fmt.Errorf("hidx: HIDIOCGINPUT %s: %w", d.f.Name(), err)
	}

	return len(buf), nil
}

func (d *linuxDevice) SetOutputReport(reportID byte, buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, errors.New("hidx: SetOutputReport called with empty buffer")
	}

	buf[0] = reportID

	ioc := hidiocsoutput(uint32(len(buf)))
	if err := ioctlPtr(d.f.Fd(), ioc, unsafe.Pointer(&buf[0])); err != nil {
		return 0, fmt.Errorf("hidx: HIDIOCSOUTPUT %s: %w", d.f.Name(), err)
	}

	return len(buf), nil
}

func (d *linuxDevice) Close() error {
	if d.f == nil {
		return nil
	}

	err := d.f.Close()
	d.f = nil

	return err //nolint:wrapcheck // os.File.Close is allow-listed in .golangci.yml
}

// HIDIOCGINPUT(len) and HIDIOCSOUTPUT(len) encodings from <linux/hidraw.h>.
//
//	HIDIOCGINPUT(len)  = _IOC(_IOC_READ|_IOC_WRITE, 'H', 0x07, len)
//	HIDIOCSOUTPUT(len) = _IOC(_IOC_READ|_IOC_WRITE, 'H', 0x0B, len)
//
// We open-code the macros so we don't drag in any C headers.
const (
	iocRead    = 2
	iocWrite   = 1
	iocDirBits = uint32(iocRead | iocWrite) // bidirectional
	iocTypeH   = uint32('H')
	hidiocNRGI = uint32(0x07)
	hidiocNRSO = uint32(0x0B)
)

func hidioc(nr, size uint32) uintptr {
	const (
		nrShift   = 0
		typeShift = 8
		sizeShift = 16
		dirShift  = 30
		sizeMask  = 0x3FFF
	)

	return uintptr(iocDirBits<<dirShift | (size&sizeMask)<<sizeShift | iocTypeH<<typeShift | nr<<nrShift)
}

func hidiocginput(size uint32) uintptr  { return hidioc(hidiocNRGI, size) }
func hidiocsoutput(size uint32) uintptr { return hidioc(hidiocNRSO, size) }

func ioctlPtr(fd uintptr, req uintptr, arg unsafe.Pointer) error {
	_, _, errno := unix.Syscall(unix.SYS_IOCTL, fd, req, uintptr(arg))
	if errno != 0 {
		return errno
	}

	return nil
}

// ─── /sys helpers (port from openmanetd, simplified for one VID/PID) ─────────

func readHexID(fsys fs.FS, path string) (uint16, bool) {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return 0, false
	}

	s := strings.TrimSpace(string(data))
	if s == "" {
		return 0, false
	}

	n, err := strconv.ParseUint(s, 16, 16)
	if err != nil {
		return 0, false
	}

	return uint16(n), true
}

func readString(fsys fs.FS, path string) string {
	data, err := fs.ReadFile(fsys, path)
	if err != nil {
		return ""
	}

	return string(data)
}

// findHidraw returns the bare hidraw node name (e.g. "hidraw3") for the
// device at devPath, or "" if no hidraw child exists.
func findHidraw(fsys fs.FS, devPath string) string {
	ifaces, err := fs.ReadDir(fsys, devPath)
	if err != nil {
		return ""
	}

	for _, iface := range ifaces {
		if !iface.IsDir() || !strings.Contains(iface.Name(), ":") {
			continue
		}

		ifacePath := devPath + "/" + iface.Name()

		hidChildren, err := fs.ReadDir(fsys, ifacePath)
		if err != nil {
			continue
		}

		for _, child := range hidChildren {
			if !child.IsDir() {
				continue
			}

			candidates := []string{
				ifacePath + "/" + child.Name() + "/hidraw",
				ifacePath + "/hidraw",
			}

			for _, candidate := range candidates {
				rawEntries, err := fs.ReadDir(fsys, candidate)
				if err != nil {
					continue
				}

				for _, raw := range rawEntries {
					if strings.HasPrefix(raw.Name(), "hidraw") {
						return raw.Name()
					}
				}
			}
		}
	}

	return ""
}
