package eeprom_test

import (
	"errors"
	"testing"

	"github.com/openmanet/openvlm/internal/cm108"
	"github.com/openmanet/openvlm/internal/eeprom"
	"github.com/openmanet/openvlm/internal/hidx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// newFakeDevice constructs a single-device fake backend pre-populated with
// the OpenVLMDefaults image. Every protocol-level test starts here.
func newFakeDevice(t *testing.T) (hidx.Transport, *hidx.FakeDeviceState) {
	t.Helper()

	var tail [eeprom.WordCount - 0x33]uint16

	img := eeprom.OpenVLMDefaults.Encode(cm108.OpenVLMVendorID, cm108.OpenVLMProductID, tail)

	var initial [64]uint16
	for i := 0; i < 64; i++ {
		initial[i] = img.Word(uint8(i))
	}

	b := hidx.NewFakeBackend()
	state := b.RegisterDevice(hidx.DeviceInfo{
		Path:      "/dev/fake0",
		VendorID:  cm108.OpenVLMVendorID,
		ProductID: cm108.OpenVLMProductID,
	}, initial, true)

	t.Cleanup(func() {})

	tr, err := b.Open("/dev/fake0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })

	return tr, state
}

// TestReadAll_Roundtrip confirms ReadAll reads back exactly what the fake
// holds. Together with TestEncodeDefaults_Roundtrip this proves the full
// "encode → write → read → decode" loop is consistent.
func TestReadAll_Roundtrip(t *testing.T) {
	t.Parallel()

	tr, state := newFakeDevice(t)

	got, err := eeprom.ReadAll(tr)
	require.NoError(t, err)

	want := state.EEPROM()
	assert.Equal(t, want, [eeprom.ByteCount]byte(got))

	view, _, err := got.Decode()
	require.NoError(t, err)
	assert.Equal(t, eeprom.OpenVLMDefaults, view)
}

// TestWriteImage_Roundtrip writes a modified image and verifies the bytes
// landed in the fake's EEPROM.
func TestWriteImage_Roundtrip(t *testing.T) {
	t.Parallel()

	tr, state := newFakeDevice(t)

	v := eeprom.OpenVLMDefaults
	v.ProductString = "OpenVLM v2"
	v.DACInitVolume = -20

	require.NoError(t, v.Validate())

	var tail [eeprom.WordCount - 0x33]uint16

	img := v.Encode(cm108.OpenVLMVendorID, cm108.OpenVLMProductID, tail)

	require.NoError(t, eeprom.WriteImage(tr, img))

	got := state.EEPROM()
	assert.Equal(t, [eeprom.ByteCount]byte(img), got)
}

// TestWriteImage_RejectsWrongVID is the single-source-of-truth test for the
// VID write-lock at the protocol layer. Even with --force, the chip never
// receives a write whose VID/PID don't match.
func TestWriteImage_RejectsWrongVID(t *testing.T) {
	t.Parallel()

	tr, state := newFakeDevice(t)

	beforeBytes := state.EEPROM()

	var img eeprom.Image
	img.SetWord(0x00, 0x6701) // valid magic + reserved bits
	img.SetWord(0x01, 0x1234) // wrong VID
	img.SetWord(0x02, cm108.OpenVLMProductID)

	err := eeprom.WriteImage(tr, img)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "VID")
	assert.Contains(t, err.Error(), "write-locked")
	assert.Equal(t, beforeBytes, state.EEPROM(), "device must be untouched")
}

// TestWriteImage_RejectsWrongPID is the matching guard for PID.
func TestWriteImage_RejectsWrongPID(t *testing.T) {
	t.Parallel()

	tr, state := newFakeDevice(t)

	beforeBytes := state.EEPROM()

	var img eeprom.Image
	img.SetWord(0x00, 0x6701)
	img.SetWord(0x01, cm108.OpenVLMVendorID)
	img.SetWord(0x02, 0xBEEF)

	err := eeprom.WriteImage(tr, img)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PID")
	assert.Contains(t, err.Error(), "write-locked")
	assert.Equal(t, beforeBytes, state.EEPROM(), "device must be untouched")
}

// TestVerifyMismatch_Surfaces ensures a read-back failure is reported with
// the offending word address and the values seen.
func TestVerifyMismatch_Surfaces(t *testing.T) {
	t.Parallel()

	// Construct a transport whose read always returns the wrong bytes.
	tr := &flakyTransport{}

	err := eeprom.WriteWord(tr, 0x05, 0xAA55)
	require.NoError(t, err)

	w, err := eeprom.ReadWord(tr, 0x05)
	require.NoError(t, err)
	assert.Equal(t, uint16(0xCAFE), w, "flaky transport returns the wrong value")

	// Now a full WriteAll should surface a VerifyError.
	var tail [eeprom.WordCount - 0x33]uint16

	img := eeprom.OpenVLMDefaults.Encode(cm108.OpenVLMVendorID, cm108.OpenVLMProductID, tail)
	werr := eeprom.WriteImage(tr, img)

	var verifyErr *eeprom.VerifyError
	require.ErrorAs(t, werr, &verifyErr)
	assert.True(t, errors.Is(werr, eeprom.ErrVerifyMismatch))
}

// flakyTransport always reports 0xCAFE on read regardless of write input.
type flakyTransport struct{}

func (f *flakyTransport) GetInputReport(_ byte, buf []byte) (int, error) {
	if len(buf) < 5 {
		return 0, errors.New("buf too small")
	}

	buf[0] = 0
	buf[1] = 0x80 // EEPROM-mode echo so eeprom.ReadWord doesn't reject the response
	buf[2] = 0xFE
	buf[3] = 0xCA
	buf[4] = 0

	return 5, nil
}

func (f *flakyTransport) SetOutputReport(_ byte, _ []byte) (int, error) { return 5, nil }
func (f *flakyTransport) Close() error                                  { return nil }
