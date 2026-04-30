//go:build linux

package hidx

import (
	"errors"
	"io/fs"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestHidioc_Encoding (Phase C3) pins the ioctl-number computation against
// hand-computed values. _IOC(_IOC_READ|_IOC_WRITE, 'H', NR, len) per
// <asm-generic/ioctl.h>:
//
//	dir   = 3 (read|write)  → << 30
//	size  = len             → << 16
//	type  = 'H' = 0x48      → << 8
//	nr    = NR
//
// HIDIOCGINPUT(5)  = 0xC0054807
// HIDIOCSOUTPUT(5) = 0xC005480B
//
// A regression here means every Linux EEPROM read/write fails with EINVAL
// at runtime, so the test pinning is worth its weight.
func TestHidioc_Encoding(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		got  uintptr
		want uintptr
	}{
		{"HIDIOCGINPUT(5)", hidiocginput(5), 0xC0054807},
		{"HIDIOCSOUTPUT(5)", hidiocsoutput(5), 0xC005480B},
		{"HIDIOCGINPUT(8)", hidiocginput(8), 0xC0084807},
		{"HIDIOCSOUTPUT(8)", hidiocsoutput(8), 0xC008480B},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equalf(t, tc.want, tc.got,
				"%s encoded as 0x%X, want 0x%X", tc.name, tc.got, tc.want)
		})
	}
}

// TestLinuxBackend_EnumerateMatchingDevice (Phase C2) drives the
// /sys/bus/usb/devices walk over a fstest.MapFS so the sysfs layout
// regression catcher exists without needing real hardware. A single
// matching device with a hidraw child must surface as one DeviceInfo.
func TestLinuxBackend_EnumerateMatchingDevice(t *testing.T) {
	t.Parallel()

	sys := fstest.MapFS{
		// Top-level USB device directory (no ':' in name).
		"bus/usb/devices/1-1.2/idVendor":     &fstest.MapFile{Data: []byte("0d8c\n")},
		"bus/usb/devices/1-1.2/idProduct":    &fstest.MapFile{Data: []byte("0012\n")},
		"bus/usb/devices/1-1.2/serial":       &fstest.MapFile{Data: []byte("OpenVLM-1\n")},
		"bus/usb/devices/1-1.2/product":      &fstest.MapFile{Data: []byte("OpenVLM\n")},
		"bus/usb/devices/1-1.2/manufacturer": &fstest.MapFile{Data: []byte("BuildsByShane\n")},
		// Interface directory (':' in name) → contains the hidraw child.
		"bus/usb/devices/1-1.2/1-1.2:1.3/0003:0D8C:0012.0001/hidraw/hidraw7": &fstest.MapFile{Mode: fs.ModeDir},
	}

	b := &linuxBackend{sysRoot: sys, devRoot: "/dev"}

	got, err := b.Enumerate(0x0D8C, 0x0012)
	require.NoError(t, err)
	require.Len(t, got, 1)

	d := got[0]
	assert.Equal(t, "/dev/hidraw7", d.Path)
	assert.Equal(t, uint16(0x0D8C), d.VendorID)
	assert.Equal(t, uint16(0x0012), d.ProductID)
	assert.Equal(t, "OpenVLM-1", d.SerialNumber)
	assert.Equal(t, "OpenVLM", d.ProductName)
	assert.Equal(t, "BuildsByShane", d.ManufacturerName)
}

// TestLinuxBackend_EnumerateFiltersOutNonMatchingVID confirms the VID
// filter is applied before the (more expensive) hidraw walk.
func TestLinuxBackend_EnumerateFiltersOutNonMatchingVID(t *testing.T) {
	t.Parallel()

	sys := fstest.MapFS{
		"bus/usb/devices/2-1/idVendor":  &fstest.MapFile{Data: []byte("1234\n")},
		"bus/usb/devices/2-1/idProduct": &fstest.MapFile{Data: []byte("5678\n")},
	}

	b := &linuxBackend{sysRoot: sys, devRoot: "/dev"}

	got, err := b.Enumerate(0x0D8C, 0x0012)
	require.NoError(t, err)
	assert.Empty(t, got)
}

// TestLinuxBackend_EnumerateSkipsDevicesWithoutHidraw confirms the
// matching-but-unreachable case is silently skipped (the user can't talk
// to a device with no hidraw node anyway, so surfacing it would just
// confuse).
func TestLinuxBackend_EnumerateSkipsDevicesWithoutHidraw(t *testing.T) {
	t.Parallel()

	sys := fstest.MapFS{
		"bus/usb/devices/3-1/idVendor":  &fstest.MapFile{Data: []byte("0d8c\n")},
		"bus/usb/devices/3-1/idProduct": &fstest.MapFile{Data: []byte("0012\n")},
		// No interface dir, no hidraw child.
	}

	b := &linuxBackend{sysRoot: sys, devRoot: "/dev"}

	got, err := b.Enumerate(0x0D8C, 0x0012)
	require.NoError(t, err)
	assert.Empty(t, got, "matching VID/PID with no hidraw node must be skipped")
}

// TestLinuxBackend_EnumerateMissingSysfs surfaces a typed error when
// /sys/bus/usb/devices is absent (e.g. running inside a chroot without
// sysfs).
func TestLinuxBackend_EnumerateMissingSysfs(t *testing.T) {
	t.Parallel()

	b := &linuxBackend{sysRoot: fstest.MapFS{}, devRoot: "/dev"}

	_, err := b.Enumerate(0x0D8C, 0x0012)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "sysfs")
}

// TestLinuxBackend_EnumerateSkipsMalformedIDs proves a corrupt
// idVendor/idProduct file does not abort the whole enumeration — the
// affected device is silently skipped, others are still returned.
func TestLinuxBackend_EnumerateSkipsMalformedIDs(t *testing.T) {
	t.Parallel()

	sys := fstest.MapFS{
		"bus/usb/devices/bad/idVendor":                                     &fstest.MapFile{Data: []byte("not-hex\n")},
		"bus/usb/devices/bad/idProduct":                                    &fstest.MapFile{Data: []byte("0012\n")},
		"bus/usb/devices/good/idVendor":                                    &fstest.MapFile{Data: []byte("0d8c\n")},
		"bus/usb/devices/good/idProduct":                                   &fstest.MapFile{Data: []byte("0012\n")},
		"bus/usb/devices/good/good:1.0/0003:0D8C:0012.0001/hidraw/hidraw3": &fstest.MapFile{Mode: fs.ModeDir},
	}

	b := &linuxBackend{sysRoot: sys, devRoot: "/dev"}

	got, err := b.Enumerate(0x0D8C, 0x0012)
	require.NoError(t, err)
	require.Len(t, got, 1)
	assert.Equal(t, "/dev/hidraw3", got[0].Path)
}

// silence unused error import on platforms that strip tests.
var _ = errors.New
