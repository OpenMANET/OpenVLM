// Package hidx is the cross-platform HID transport seam for openvlm.
//
// Every operation the rest of the CLI needs from a HID device — enumerating
// attached units that match a VID/PID pair, opening one by path, and
// performing the two USB-Audio-class control transfers required by the
// CM108B (Get_Input_Report and Set_Output_Report) — is expressed by the
// Transport interface below.
//
// Three implementations live alongside this file under build tags:
//
//   - transport_linux.go    pure Go, hidraw ioctls via golang.org/x/sys/unix
//   - transport_windows.go  pure Go, hid.dll + setupapi.dll via x/sys/windows
//   - transport_darwin.go   CGO via github.com/sstallion/go-hid (IOKit)
//
// The fake.go implementation is platform-neutral and exists for tests so the
// rest of the codebase exercises the same interface in unit tests as in
// production.
package hidx

import "errors"

// DeviceInfo describes one HID device discovered by Enumerate. Fields are
// best-effort: SerialNumber and Path are guaranteed populated for any device
// the caller can subsequently Open(); ProductName and ManufacturerName are
// populated when the OS exposes them (always on Linux/macOS, generally on
// Windows).
type DeviceInfo struct {
	Path             string
	SerialNumber     string
	ProductName      string
	ManufacturerName string
	VendorID         uint16
	ProductID        uint16
}

// Transport is one open HID device. It is single-threaded; callers must not
// invoke methods concurrently from multiple goroutines.
type Transport interface {
	// GetInputReport issues a USB Get_Report(Input) control transfer for the
	// numbered report (use 0 for unnumbered reports, which is what the CM108B
	// uses). Buf must be sized to (1 + report payload length); buf[0] is set
	// to reportID by the implementation and buf[1:] receives the payload.
	// Returns the total number of bytes filled (including the report-ID byte).
	GetInputReport(reportID byte, buf []byte) (int, error)

	// SetOutputReport issues a USB Set_Report(Output) control transfer for
	// the numbered report. Buf must contain [reportID, payload...]; the
	// implementation uses buf[0] as the report ID and sends buf[1:] as the
	// report data. Returns the number of bytes accepted.
	SetOutputReport(reportID byte, buf []byte) (int, error)

	// Close releases the underlying OS handle. Safe to call multiple times.
	Close() error
}

// Backend is the platform-neutral entry point that the rest of the CLI uses
// to enumerate and open devices. Each per-OS file provides exactly one
// implementation named newBackend(), constructed by NewBackend().
type Backend interface {
	// Enumerate returns every HID device whose VendorID and ProductID match
	// the requested values. Pass 0 for vendorID or productID to leave it
	// unconstrained. Devices that the OS won't let the caller open (e.g.
	// permission errors) are still returned so the user can be told why a
	// known device is unreachable.
	Enumerate(vendorID, productID uint16) ([]DeviceInfo, error)

	// Open opens the HID device whose path matches one returned from
	// Enumerate. The returned Transport must be Closed by the caller.
	Open(path string) (Transport, error)
}

// NewBackend returns the per-OS Backend implementation. There is no choice of
// backend; the build tag selects exactly one.
func NewBackend() Backend { return newBackend() }

// ErrShortReport is returned when a HID report transfer succeeds but returns
// fewer bytes than the caller asked for. The CM108B always returns 5 bytes
// (report ID + 4 register bytes); a shorter response indicates a kernel or
// driver bug worth surfacing.
var ErrShortReport = errors.New("hidx: HID report shorter than expected")
