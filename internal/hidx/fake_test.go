package hidx_test

import (
	"testing"

	"github.com/openmanet/openvlm/internal/hidx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestFake_EnumerateFiltersByVIDPID confirms RegisterDevice + Enumerate
// implement the documented filter contract: 0 means 'unconstrained' on
// each axis.
func TestFake_EnumerateFiltersByVIDPID(t *testing.T) {
	t.Parallel()

	b := hidx.NewFakeBackend()
	b.RegisterDevice(hidx.DeviceInfo{Path: "/dev/a", VendorID: 0x0D8C, ProductID: 0x0012}, [64]uint16{}, true)
	b.RegisterDevice(hidx.DeviceInfo{Path: "/dev/b", VendorID: 0x0D8C, ProductID: 0x9999}, [64]uint16{}, false)
	b.RegisterDevice(hidx.DeviceInfo{Path: "/dev/c", VendorID: 0xBAD0, ProductID: 0x0012}, [64]uint16{}, true)

	all, err := b.Enumerate(0, 0)
	require.NoError(t, err)
	assert.Len(t, all, 3, "VID=0 PID=0 must return every device")

	openvlm, err := b.Enumerate(0x0D8C, 0x0012)
	require.NoError(t, err)
	require.Len(t, openvlm, 1)
	assert.Equal(t, "/dev/a", openvlm[0].Path)

	byVID, err := b.Enumerate(0x0D8C, 0)
	require.NoError(t, err)
	assert.Len(t, byVID, 2, "VID-only filter returns every matching VID")
}

// TestFake_OpenUnknownPath surfaces a typed error rather than a nil
// transport.
func TestFake_OpenUnknownPath(t *testing.T) {
	t.Parallel()

	b := hidx.NewFakeBackend()
	_, err := b.Open("/dev/nope")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no device")
}

// TestFake_GPIOProbeReadsStrap exercises the documented GPIO-input mode
// path: when HID_OR0 was last set to 0, GetInputReport returns the
// configured GPIO1 strap state in IR1[0].
func TestFake_GPIOProbeReadsStrap(t *testing.T) {
	t.Parallel()

	b := hidx.NewFakeBackend()
	b.RegisterDevice(hidx.DeviceInfo{Path: "/dev/strap-high"}, [64]uint16{}, true)
	b.RegisterDevice(hidx.DeviceInfo{Path: "/dev/strap-low"}, [64]uint16{}, false)

	for _, tc := range []struct {
		path     string
		wantHigh bool
	}{
		{"/dev/strap-high", true},
		{"/dev/strap-low", false},
	} {
		tc := tc
		t.Run(tc.path, func(t *testing.T) {
			t.Parallel()

			tr, err := b.Open(tc.path)
			require.NoError(t, err)
			t.Cleanup(func() { _ = tr.Close() })

			// Force GPIO-input mode (HID_OR0 = 0).
			_, err = tr.SetOutputReport(0, []byte{0, 0, 0, 0, 0})
			require.NoError(t, err)

			buf := make([]byte, 5)
			_, err = tr.GetInputReport(0, buf)
			require.NoError(t, err)

			gotHigh := buf[2]&0x01 != 0
			assert.Equal(t, tc.wantHigh, gotHigh)
		})
	}
}

// TestFake_EEPROMReadWriteRoundtrip exercises the EEPROM-mode transfer
// pair end-to-end (Set_Output write → Set_Output read addr → Get_Input).
func TestFake_EEPROMReadWriteRoundtrip(t *testing.T) {
	t.Parallel()

	b := hidx.NewFakeBackend()
	b.RegisterDevice(hidx.DeviceInfo{Path: "/dev/x"}, [64]uint16{}, true)

	tr, err := b.Open("/dev/x")
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })

	// Write 0xBEEF to addr 0x05.
	_, err = tr.SetOutputReport(0, []byte{
		0,
		0x80,
		0xEF, // OR1 = data low
		0xBE, // OR2 = data high
		(0b11 << 6) | 0x05,
	})
	require.NoError(t, err)

	// Read addr 0x05 — first stage the address...
	_, err = tr.SetOutputReport(0, []byte{0, 0x80, 0, 0, (0b10 << 6) | 0x05})
	require.NoError(t, err)
	// ...then sample the staged data.
	buf := make([]byte, 5)
	_, err = tr.GetInputReport(0, buf)
	require.NoError(t, err)

	assert.Equal(t, byte(0x80), buf[1]&0xC0,
		"IR0[7:6] must echo EEPROM-mode bits so the host trusts IR1/IR2")
	got := uint16(buf[2]) | uint16(buf[3])<<8
	assert.Equal(t, uint16(0xBEEF), got)
}

// TestFake_CountsTrackCalls exposes the FakeDeviceState.Counts helper that
// the protocol tests rely on. A single round-trip should bump both counters
// monotonically.
func TestFake_CountsTrackCalls(t *testing.T) {
	t.Parallel()

	b := hidx.NewFakeBackend()
	state := b.RegisterDevice(hidx.DeviceInfo{Path: "/dev/y"}, [64]uint16{}, false)

	tr, err := b.Open("/dev/y")
	require.NoError(t, err)

	_, _ = tr.SetOutputReport(0, []byte{0, 0, 0, 0, 0})

	buf := make([]byte, 5)
	_, _ = tr.GetInputReport(0, buf)

	require.NoError(t, tr.Close())

	writes, reads, closes := state.Counts()
	assert.Equal(t, 1, writes)
	assert.Equal(t, 1, reads)
	assert.Equal(t, 1, closes)
}
