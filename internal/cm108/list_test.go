package cm108_test

import (
	"errors"
	"testing"

	"github.com/openmanet/openvlm/internal/cm108"
	"github.com/openmanet/openvlm/internal/hidx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestList_StrappedAndUnstrapped (Phase D2) confirms List enumerates every
// device matching the OpenVLM VID/PID and probes each one's GPIO1 strap.
// A strapped device must come back with IsOpenVLM=true; an unstrapped one
// with IsOpenVLM=false. Per-device probe failures must NOT abort the
// listing.
func TestList_StrappedAndUnstrapped(t *testing.T) {
	t.Parallel()

	b := hidx.NewFakeBackend()
	b.RegisterDevice(hidx.DeviceInfo{
		Path:      "/dev/strapped",
		VendorID:  cm108.OpenVLMVendorID,
		ProductID: cm108.OpenVLMProductID,
	}, [64]uint16{}, true)
	b.RegisterDevice(hidx.DeviceInfo{
		Path:      "/dev/unstrapped",
		VendorID:  cm108.OpenVLMVendorID,
		ProductID: cm108.OpenVLMProductID,
	}, [64]uint16{}, false)

	descs, err := cm108.List(b)
	require.NoError(t, err)
	require.Len(t, descs, 2)

	got := map[string]bool{}
	for _, d := range descs {
		got[d.Path] = d.IsOpenVLM
		assert.NoErrorf(t, d.ProbeError, "ProbeError on %s must be nil for a healthy fake", d.Path)
	}

	assert.True(t, got["/dev/strapped"])
	assert.False(t, got["/dev/unstrapped"])
}

// TestList_FiltersByVIDPID confirms List only surfaces OpenVLM-VID/PID
// devices, not arbitrary HID hardware on the host.
func TestList_FiltersByVIDPID(t *testing.T) {
	t.Parallel()

	b := hidx.NewFakeBackend()
	b.RegisterDevice(hidx.DeviceInfo{
		Path:      "/dev/openvlm",
		VendorID:  cm108.OpenVLMVendorID,
		ProductID: cm108.OpenVLMProductID,
	}, [64]uint16{}, true)
	b.RegisterDevice(hidx.DeviceInfo{
		Path:      "/dev/random-hid",
		VendorID:  0xBEEF,
		ProductID: 0xCAFE,
	}, [64]uint16{}, true)

	descs, err := cm108.List(b)
	require.NoError(t, err)
	require.Len(t, descs, 1)
	assert.Equal(t, "/dev/openvlm", descs[0].Path)
}

// TestList_PerDeviceOpenErrorIsNonFatal proves that one device whose
// Open() fails does not poison the rest of the listing — the failed
// device shows up with a populated ProbeError, the others come back
// healthy.
func TestList_PerDeviceOpenErrorIsNonFatal(t *testing.T) {
	t.Parallel()

	openErr := errors.New("permission denied")

	b := &errBackend{
		inner:    hidx.NewFakeBackend(),
		failPath: "/dev/locked",
		failErr:  openErr,
	}
	b.inner.RegisterDevice(hidx.DeviceInfo{
		Path:      "/dev/locked",
		VendorID:  cm108.OpenVLMVendorID,
		ProductID: cm108.OpenVLMProductID,
	}, [64]uint16{}, false)
	b.inner.RegisterDevice(hidx.DeviceInfo{
		Path:      "/dev/healthy",
		VendorID:  cm108.OpenVLMVendorID,
		ProductID: cm108.OpenVLMProductID,
	}, [64]uint16{}, true)

	descs, err := cm108.List(b)
	require.NoError(t, err, "List itself must not fail just because one device errored")
	require.Len(t, descs, 2)

	by := map[string]cm108.Descriptor{}
	for _, d := range descs {
		by[d.Path] = d
	}

	require.Contains(t, by, "/dev/locked")
	require.Error(t, by["/dev/locked"].ProbeError, "locked device must surface its open error")
	assert.True(t, errors.Is(by["/dev/locked"].ProbeError, openErr))

	require.Contains(t, by, "/dev/healthy")
	assert.NoError(t, by["/dev/healthy"].ProbeError)
	assert.True(t, by["/dev/healthy"].IsOpenVLM)
}

// TestPick_SerialNotFoundCarriesValue (Phase D3) ensures the
// ErrSerialNotFound error message includes the serial the user typed —
// helps debug shell-script callers.
func TestPick_SerialNotFoundCarriesValue(t *testing.T) {
	t.Parallel()

	descs := []cm108.Descriptor{
		{Path: "/dev/a", SerialNumber: "abc"},
		{Path: "/dev/b", SerialNumber: "xyz"},
	}

	_, err := cm108.Pick(descs, cm108.PickOptions{Serial: "missing"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, cm108.ErrSerialNotFound))
	assert.Contains(t, err.Error(), "missing")
}

// errBackend wraps a FakeBackend and forces Open() of a single configured
// path to return a specific error — used to simulate a permission-denied
// device for List_PerDeviceOpenErrorIsNonFatal.
type errBackend struct {
	inner    *hidx.FakeBackend
	failPath string
	failErr  error
}

func (e *errBackend) Enumerate(vid, pid uint16) ([]hidx.DeviceInfo, error) {
	return e.inner.Enumerate(vid, pid) //nolint:wrapcheck // pass-through
}

func (e *errBackend) Open(path string) (hidx.Transport, error) {
	if path == e.failPath {
		return nil, e.failErr
	}

	return e.inner.Open(path) //nolint:wrapcheck // pass-through
}
