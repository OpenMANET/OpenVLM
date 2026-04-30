package hidx

import (
	"errors"
	"fmt"
	"sync"
	"time"
)

// FakeBackend is an in-memory Backend used by every test in the repository.
// It models the CM108B's documented HID protocol closely enough that the
// rest of the CLI exercises identical code paths in tests as in production:
//
//   - Enumerate returns whatever DeviceInfo entries were registered with
//     RegisterDevice, optionally filtered by VID/PID.
//   - Open returns a FakeDevice tied to the device's underlying memory state.
//   - FakeDevice.SetOutputReport interprets HID_OR0/OR3 per CM108B §7.4 and
//     mutates a 64-word EEPROM array on EEPROM-mode writes; on EEPROM-mode
//     reads, the fake stages the addressed word into IR1/IR2.
//   - FakeDevice.GetInputReport returns the staged IR0..IR3 register bytes.
//
// FakeBackend is safe for concurrent use across goroutines, but each
// FakeDevice (one device handle) is single-threaded — same contract as a
// real Transport.
type FakeBackend struct {
	devices []*fakeDeviceState
	mu      sync.Mutex
}

// NewFakeBackend constructs an empty fake. Call RegisterDevice to add devices.
func NewFakeBackend() *FakeBackend { return &FakeBackend{} }

// RegisterDevice adds a virtual HID device with the given identifying info
// and a 64-word (128-byte) EEPROM contents. The device's GPIO1 strap state
// (true = pulled high) drives what GetInputReport returns when the chip is
// in default GPIO-input mode. The returned handle lets tests poke at the
// device's state directly (e.g. to assert the EEPROM contents after a
// write).
func (b *FakeBackend) RegisterDevice(info DeviceInfo, eeprom [64]uint16, gpio1High bool) *FakeDeviceState {
	b.mu.Lock()
	defer b.mu.Unlock()

	st := &fakeDeviceState{
		info:      info,
		eeprom:    eeprom,
		gpio1High: gpio1High,
	}
	b.devices = append(b.devices, st)

	return (*FakeDeviceState)(st)
}

// Enumerate implements Backend.
func (b *FakeBackend) Enumerate(vendorID, productID uint16) ([]DeviceInfo, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	out := make([]DeviceInfo, 0, len(b.devices))

	for _, d := range b.devices {
		if vendorID != 0 && d.info.VendorID != vendorID {
			continue
		}

		if productID != 0 && d.info.ProductID != productID {
			continue
		}

		out = append(out, d.info)
	}

	return out, nil
}

// Open implements Backend.
func (b *FakeBackend) Open(path string) (Transport, error) {
	b.mu.Lock()
	defer b.mu.Unlock()

	for _, d := range b.devices {
		if d.info.Path == path {
			return &FakeDevice{state: d}, nil
		}
	}

	return nil, fmt.Errorf("hidx: fake: no device at path %q", path)
}

// fakeDeviceState is the unexported live state of one virtual device.
// FakeDeviceState is the exported alias so tests can reach in.
type fakeDeviceState struct {
	info        DeviceInfo
	WriteDelay  time.Duration
	closeCalled int
	ReadCount   int
	WriteCount  int
	mu          sync.Mutex
	eeprom      [64]uint16
	ir0         byte
	gpio1High   bool
	or0         byte
	ir3         byte
	ir2         byte
	ir1         byte
}

// FakeDeviceState is the exported view tests get back from RegisterDevice.
type FakeDeviceState fakeDeviceState

// EEPROM returns a copy of the device's current 128-byte EEPROM image.
func (s *FakeDeviceState) EEPROM() [128]byte {
	st := (*fakeDeviceState)(s)

	st.mu.Lock()
	defer st.mu.Unlock()

	var out [128]byte

	for i, w := range st.eeprom {
		out[i*2] = byte(w & 0xFF)
		out[i*2+1] = byte(w >> 8)
	}

	return out
}

// SetEEPROMWord overwrites a single 16-bit word in the fake's EEPROM. Used
// by tests to stage starting state.
func (s *FakeDeviceState) SetEEPROMWord(addr uint8, value uint16) {
	st := (*fakeDeviceState)(s)

	st.mu.Lock()
	defer st.mu.Unlock()

	if int(addr) < len(st.eeprom) {
		st.eeprom[addr] = value
	}
}

// SetGPIO1 toggles the simulated GPIO1 strap state.
func (s *FakeDeviceState) SetGPIO1(high bool) {
	st := (*fakeDeviceState)(s)

	st.mu.Lock()
	defer st.mu.Unlock()

	st.gpio1High = high
}

// Counts returns how many SetOutputReport / GetInputReport calls were made
// against this device. Useful for verifying retry behavior in tests.
func (s *FakeDeviceState) Counts() (writes, reads, closes int) {
	st := (*fakeDeviceState)(s)

	st.mu.Lock()
	defer st.mu.Unlock()

	return st.WriteCount, st.ReadCount, st.closeCalled
}

// FakeDevice is one open handle into a FakeDeviceState.
type FakeDevice struct {
	state *fakeDeviceState
}

func (d *FakeDevice) GetInputReport(reportID byte, buf []byte) (int, error) {
	if len(buf) < 5 {
		return 0, errors.New("hidx: fake: GetInputReport buffer too small")
	}

	d.state.mu.Lock()
	defer d.state.mu.Unlock()

	d.state.ReadCount++
	buf[0] = reportID

	// In default GPIO-input mode (HID_IR0[7:6] == 0b00) the fake reports
	// GPIO1..GPIO4 strap state in IR1[3:0]. Otherwise it reports the staged
	// EEPROM read result.
	if d.state.or0&0x80 == 0 {
		buf[1] = 0 // IR0[7:6] = 00

		buf[2] = 0 // IR1: only GPIO bits we care about
		if d.state.gpio1High {
			buf[2] |= 0x01
		}

		buf[3] = 0
		buf[4] = 0
	} else {
		buf[1] = d.state.ir0
		buf[2] = d.state.ir1
		buf[3] = d.state.ir2
		buf[4] = d.state.ir3
	}

	return 5, nil
}

func (d *FakeDevice) SetOutputReport(reportID byte, buf []byte) (int, error) {
	if len(buf) < 5 {
		return 0, errors.New("hidx: fake: SetOutputReport buffer too small")
	}

	d.state.mu.Lock()

	d.state.WriteCount++

	or0 := buf[1]
	or1 := buf[2]
	or2 := buf[3]
	or3 := buf[4]

	d.state.or0 = or0

	if or0&0x80 != 0 {
		// EEPROM access mode. OR3 = (op<<6) | addr. op=0b10 read, 0b11 write.
		op := or3 >> 6
		addr := or3 & 0x3F

		switch op {
		case 0b10:
			// Read addr → stage data into IR1/IR2.
			data := uint16(0)
			if int(addr) < len(d.state.eeprom) {
				data = d.state.eeprom[addr]
			}
			// IR0 echoes EEPROM-mode bits (top bit set so the helper can
			// detect EEPROM-mode echo when validating).
			d.state.ir0 = 0x80
			d.state.ir1 = byte(data & 0xFF)
			d.state.ir2 = byte(data >> 8)
			d.state.ir3 = or3
		case 0b11:
			// Write data into addr.
			if int(addr) < len(d.state.eeprom) {
				d.state.eeprom[addr] = uint16(or1) | (uint16(or2) << 8)
			}

			d.state.ir0 = 0x80
			d.state.ir1 = or1
			d.state.ir2 = or2
			d.state.ir3 = or3
		default:
			// Unknown op — just echo IR3 so the host sees the response.
			d.state.ir0 = 0x80
			d.state.ir3 = or3
		}

		delay := d.state.WriteDelay
		d.state.mu.Unlock()

		if delay > 0 {
			time.Sleep(delay)
		}

		return 5, nil
	}

	d.state.mu.Unlock()

	return 5, nil
}

func (d *FakeDevice) Close() error {
	d.state.mu.Lock()
	defer d.state.mu.Unlock()

	d.state.closeCalled++

	return nil
}
