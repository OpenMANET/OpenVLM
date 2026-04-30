package cm108_test

import (
	"errors"
	"testing"

	"github.com/openmanet/openvlm/internal/cm108"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCheckOpenVLMIdentity_StrapHigh confirms the GPIO1=high case is
// reported as OpenVLM.
func TestCheckOpenVLMIdentity_StrapHigh(t *testing.T) {
	t.Parallel()

	tr := &fakeIdentityTransport{ir0: 0x00, ir1: 0x01}
	got, err := cm108.CheckOpenVLMIdentity(tr)
	require.NoError(t, err)
	assert.True(t, got)
}

// TestCheckOpenVLMIdentity_StrapLow confirms the generic-CM108 case.
func TestCheckOpenVLMIdentity_StrapLow(t *testing.T) {
	t.Parallel()

	tr := &fakeIdentityTransport{ir0: 0x00, ir1: 0x00}
	got, err := cm108.CheckOpenVLMIdentity(tr)
	require.NoError(t, err)
	assert.False(t, got)
}

// TestCheckOpenVLMIdentity_OtherGPIOsIgnored proves we only look at GPIO1
// (bit 0 of IR1) — GPIO2..4 high but GPIO1 low → not OpenVLM.
func TestCheckOpenVLMIdentity_OtherGPIOsIgnored(t *testing.T) {
	t.Parallel()

	tr := &fakeIdentityTransport{ir0: 0x00, ir1: 0x0E}
	got, err := cm108.CheckOpenVLMIdentity(tr)
	require.NoError(t, err)
	assert.False(t, got)
}

// TestCheckOpenVLMIdentity_NotInGPIOMode reports an error when IR0[7:6]
// indicates the chip is mid-EEPROM transaction. IR1 in that case is the
// EEPROM data buffer, not the GPIO state, and a true/false answer would
// be a lie.
func TestCheckOpenVLMIdentity_NotInGPIOMode(t *testing.T) {
	t.Parallel()

	tr := &fakeIdentityTransport{ir0: 0x80, ir1: 0x01}
	_, err := cm108.CheckOpenVLMIdentity(tr)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "GPIO-input mode")
}

// TestCheckOpenVLMIdentity_TransportError surfaces backend errors via
// errors.Is so callers can match on the underlying cause.
func TestCheckOpenVLMIdentity_TransportError(t *testing.T) {
	t.Parallel()

	want := errors.New("backend boom")
	tr := &fakeIdentityTransport{err: want}

	_, err := cm108.CheckOpenVLMIdentity(tr)
	require.Error(t, err)
	assert.True(t, errors.Is(err, want))
}

// TestPick_AutoSelectStrapped confirms the documented selection rule:
// exactly one OpenVLM-strapped device → auto-pick it.
func TestPick_AutoSelectStrapped(t *testing.T) {
	t.Parallel()

	descs := []cm108.Descriptor{
		{Path: "/dev/a", IsOpenVLM: false},
		{Path: "/dev/b", IsOpenVLM: true},
	}
	picked, err := cm108.Pick(descs, cm108.PickOptions{})
	require.NoError(t, err)
	assert.Equal(t, "/dev/b", picked.Path)
}

// TestPick_BySerial uses the --serial path.
func TestPick_BySerial(t *testing.T) {
	t.Parallel()

	descs := []cm108.Descriptor{
		{Path: "/dev/a", SerialNumber: "abc"},
		{Path: "/dev/b", SerialNumber: "xyz"},
	}
	picked, err := cm108.Pick(descs, cm108.PickOptions{Serial: "xyz"})
	require.NoError(t, err)
	assert.Equal(t, "/dev/b", picked.Path)
}

// TestPick_AmbiguousMultipleStrapped errors when two devices both claim to
// be OpenVLM and no --serial was given.
func TestPick_AmbiguousMultipleStrapped(t *testing.T) {
	t.Parallel()

	descs := []cm108.Descriptor{
		{Path: "/dev/a", IsOpenVLM: true},
		{Path: "/dev/b", IsOpenVLM: true},
	}
	_, err := cm108.Pick(descs, cm108.PickOptions{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, cm108.ErrAmbiguousDevice))
}

// TestPick_NoStrappedDevices errors with ErrNoOpenVLMStrapped when none of
// the matched devices have the GPIO1 strap and there's more than one CM108.
func TestPick_NoStrappedDevices(t *testing.T) {
	t.Parallel()

	descs := []cm108.Descriptor{
		{Path: "/dev/a"},
		{Path: "/dev/b"},
	}
	_, err := cm108.Pick(descs, cm108.PickOptions{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, cm108.ErrNoOpenVLMStrapped))
}

// TestPick_SingleUnstrappedDevice still picks the device when it's the only
// CM108 attached — useful for `provision --force` on a fresh dongle.
func TestPick_SingleUnstrappedDevice(t *testing.T) {
	t.Parallel()

	descs := []cm108.Descriptor{{Path: "/dev/a"}}
	picked, err := cm108.Pick(descs, cm108.PickOptions{})
	require.NoError(t, err)
	assert.Equal(t, "/dev/a", picked.Path)
}

// TestPick_NoDevices reports ErrNoDevice for the empty-list case.
func TestPick_NoDevices(t *testing.T) {
	t.Parallel()

	_, err := cm108.Pick(nil, cm108.PickOptions{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, cm108.ErrNoDevice))
}

// fakeIdentityTransport synthesizes one input report for identity tests.
type fakeIdentityTransport struct {
	ir0 byte
	ir1 byte
	err error
}

func (f *fakeIdentityTransport) GetInputReport(_ byte, buf []byte) (int, error) {
	if f.err != nil {
		return 0, f.err
	}

	if len(buf) < 5 {
		return 0, errors.New("buf too small")
	}

	buf[0] = 0
	buf[1] = f.ir0
	buf[2] = f.ir1
	buf[3] = 0
	buf[4] = 0

	return 5, nil
}

func (f *fakeIdentityTransport) SetOutputReport(_ byte, _ []byte) (int, error) { return 5, nil }
func (f *fakeIdentityTransport) Close() error                                  { return nil }
