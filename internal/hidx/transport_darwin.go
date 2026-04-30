//go:build darwin

package hidx

import (
	"errors"
	"fmt"
	"sync"

	"github.com/sstallion/go-hid"
)

// darwinBackend wraps github.com/sstallion/go-hid (a hidapi binding) for
// the macOS build only. hidapi calls into IOKit at link time via CGO; this
// is the only file in the repository that requires CGO.
//
// We use hidapi's GetInputReport / SendOutputReport which map directly to
// IOHIDDeviceGetReport(kIOHIDReportTypeInput) and SetReport(Output) — the
// USB control transfers the CM108B datasheet §7.4 documents. The Feature
// variants do not work: macOS returns kIOReturnUnsupported (0xE0005000)
// because the CM108B's HID descriptor declares Input/Output reports, not
// Feature reports.
type darwinBackend struct {
	mu    sync.Mutex
	inits int
}

//nolint:gochecknoglobals // single backend instance per process
var darwinSingleton = &darwinBackend{}

func newBackend() Backend { return darwinSingleton }

// init lazily initializes hidapi exactly once per process. hidapi requires
// a paired Init/Exit and silently leaks if Init is never called.
func (b *darwinBackend) init() error {
	b.mu.Lock()
	defer b.mu.Unlock()

	if b.inits > 0 {
		b.inits++

		return nil
	}

	if err := hid.Init(); err != nil {
		return fmt.Errorf("hidx: hidapi init: %w", err)
	}

	b.inits++

	return nil
}

func (b *darwinBackend) Enumerate(vendorID, productID uint16) ([]DeviceInfo, error) {
	if err := b.init(); err != nil {
		return nil, err
	}

	results := make([]DeviceInfo, 0, 4)

	walk := func(info *hid.DeviceInfo) error {
		results = append(results, DeviceInfo{
			Path:             info.Path,
			SerialNumber:     info.SerialNbr,
			ProductName:      info.ProductStr,
			ManufacturerName: info.MfrStr,
			VendorID:         info.VendorID,
			ProductID:        info.ProductID,
		})

		return nil
	}

	if err := hid.Enumerate(vendorID, productID, walk); err != nil {
		return nil, fmt.Errorf("hidx: hid.Enumerate: %w", err)
	}

	return results, nil
}

func (b *darwinBackend) Open(path string) (Transport, error) {
	if err := b.init(); err != nil {
		return nil, err
	}

	dev, err := hid.OpenPath(path)
	if err != nil {
		return nil, fmt.Errorf("hidx: hid.OpenPath %s: %w", path, err)
	}

	return &darwinDevice{dev: dev, path: path}, nil
}

type darwinDevice struct {
	dev  *hid.Device
	path string
}

func (d *darwinDevice) GetInputReport(reportID byte, buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, errors.New("hidx: GetInputReport called with empty buffer")
	}

	buf[0] = reportID

	n, err := d.dev.GetInputReport(buf)
	if err != nil {
		return 0, fmt.Errorf("hidx: GetInputReport %s: %w", d.path, err)
	}

	return n, nil
}

func (d *darwinDevice) SetOutputReport(reportID byte, buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, errors.New("hidx: SetOutputReport called with empty buffer")
	}

	buf[0] = reportID

	n, err := d.dev.SendOutputReport(buf)
	if err != nil {
		return 0, fmt.Errorf("hidx: SendOutputReport %s: %w", d.path, err)
	}

	return n, nil
}

func (d *darwinDevice) Close() error {
	if d.dev == nil {
		return nil
	}

	err := d.dev.Close()
	d.dev = nil

	if err != nil {
		return fmt.Errorf("hidx: close %s: %w", d.path, err)
	}

	return nil
}
