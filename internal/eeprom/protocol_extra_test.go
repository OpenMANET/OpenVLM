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

// TestReadWord_AddressOutOfRange (Phase B1) ensures the address guard fires
// before any bytes hit the transport.
func TestReadWord_AddressOutOfRange(t *testing.T) {
	t.Parallel()

	tr := &countingTransport{}

	_, err := eeprom.ReadWord(tr, eeprom.WordCount)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
	assert.Equal(t, 0, tr.setCount, "transport must not be invoked when addr is out of range")
	assert.Equal(t, 0, tr.getCount, "transport must not be invoked when addr is out of range")
}

// TestWriteWord_AddressOutOfRange mirrors B1 for the write side.
func TestWriteWord_AddressOutOfRange(t *testing.T) {
	t.Parallel()

	tr := &countingTransport{}

	err := eeprom.WriteWord(tr, eeprom.WordCount, 0x1234)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "out of range")
	assert.Equal(t, 0, tr.setCount)
}

// TestReadWord_RejectsBadEcho (Phase B2) confirms ReadWord refuses an
// input report whose IR0 does not have HID_OR0[7:6] echoed back. Per
// datasheet §7.4 the chip echoes the EEPROM-mode bits when it has staged
// the addressed word; absence of the echo means the host should not trust
// IR1/IR2 as data.
func TestReadWord_RejectsBadEcho(t *testing.T) {
	t.Parallel()

	tr := &echoFailTransport{}

	_, err := eeprom.ReadWord(tr, 0x05)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "did not echo EEPROM mode")
}

// TestReadWord_TransportSetError (Phase B3a) propagates the transport's
// SetOutputReport error with the offending address mentioned.
func TestReadWord_TransportSetError(t *testing.T) {
	t.Parallel()

	want := errors.New("set boom")
	tr := &injectErrTransport{setErr: want}

	_, err := eeprom.ReadWord(tr, 0x12)
	require.Error(t, err)
	assert.True(t, errors.Is(err, want), "transport error must be wrapped")
	assert.Contains(t, err.Error(), "0x12", "error must name the address")
}

// TestReadWord_TransportGetError (Phase B3b) propagates the GetInputReport
// error path.
func TestReadWord_TransportGetError(t *testing.T) {
	t.Parallel()

	want := errors.New("get boom")
	tr := &injectErrTransport{getErr: want}

	_, err := eeprom.ReadWord(tr, 0x07)
	require.Error(t, err)
	assert.True(t, errors.Is(err, want))
	assert.Contains(t, err.Error(), "0x07")
}

// TestWriteWord_TransportSetError (Phase B3c) mirrors B3a on the write path.
func TestWriteWord_TransportSetError(t *testing.T) {
	t.Parallel()

	want := errors.New("write boom")
	tr := &injectErrTransport{setErr: want}

	err := eeprom.WriteWord(tr, 0x33, 0x1234)
	require.Error(t, err)
	assert.True(t, errors.Is(err, want))
	assert.Contains(t, err.Error(), "0x33")
}

// TestWriteImage_RejectsVIDMatchPIDWrong (Phase B4) covers the third
// VID/PID guard combination missing from protocol_test.go: VID is correct
// but PID is corrupted. The existing tests cover wrong-VID-correct-PID and
// wrong-PID; this confirms the guard triggers when the failure straddles
// only the second word.
func TestWriteImage_RejectsVIDMatchPIDWrong(t *testing.T) {
	t.Parallel()

	tr := newFakeTransport(t)

	var img eeprom.Image
	img.SetWord(0x00, 0x670D)
	img.SetWord(0x01, cm108.OpenVLMVendorID)
	img.SetWord(0x02, 0x1234)

	err := eeprom.WriteImage(tr.transport, img)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "PID")
	assert.Contains(t, err.Error(), "write-locked")
}

// TestWriteImage_VerifyMismatchAddressIsAccurate (Phase B5) constructs a
// transport that flips exactly one specific word's read-back. The
// surfaced VerifyError must name THAT address — not any later word.
func TestWriteImage_VerifyMismatchAddressIsAccurate(t *testing.T) {
	t.Parallel()

	const flipAddr = uint8(0x0F)

	// Start with a fake that holds a writable copy of OpenVLMDefaults.
	tr := newFakeTransport(t)

	// Build a wrapper that lies on read-back of one specific address.
	wrapper := &readBackLiar{inner: tr.transport, lieAddr: flipAddr}

	var tail [eeprom.WordCount - 0x33]uint16
	img := eeprom.OpenVLMDefaults.Encode(cm108.OpenVLMVendorID, cm108.OpenVLMProductID, tail)

	err := eeprom.WriteImage(wrapper, img)
	require.Error(t, err)

	var verr *eeprom.VerifyError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, flipAddr, verr.Addr,
		"VerifyError must point at the lying word, not at any subsequent address")
}

// TestWriteImage_WritesAllWordsInOrder (Phase B6) instruments the fake to
// record the address sequence WriteImage emits. Confirms 0..63 in order
// (no off-by-one, no skip, no duplicate).
func TestWriteImage_WritesAllWordsInOrder(t *testing.T) {
	t.Parallel()

	tr := newFakeTransport(t)
	tracker := &orderTracker{inner: tr.transport}

	var tail [eeprom.WordCount - 0x33]uint16
	img := eeprom.OpenVLMDefaults.Encode(cm108.OpenVLMVendorID, cm108.OpenVLMProductID, tail)

	require.NoError(t, eeprom.WriteImage(tracker, img))

	require.Len(t, tracker.writeAddrs, eeprom.WordCount,
		"WriteImage must issue exactly WordCount write transfers")

	for i, got := range tracker.writeAddrs {
		assert.Equalf(t, uint8(i), got,
			"write transfer #%d targeted addr 0x%02X, expected 0x%02X",
			i, got, i)
	}
}

// ─── test doubles ───────────────────────────────────────────────────────

// countingTransport records call counts; never errors.
type countingTransport struct {
	setCount int
	getCount int
}

func (c *countingTransport) SetOutputReport(_ byte, _ []byte) (int, error) {
	c.setCount++
	return 5, nil
}

func (c *countingTransport) GetInputReport(_ byte, buf []byte) (int, error) {
	c.getCount++
	if len(buf) >= 5 {
		// Always return EEPROM-mode echo so the protocol code does not
		// short-circuit on missing-echo before we count the call.
		buf[1] = 0x80
	}
	return 5, nil
}
func (c *countingTransport) Close() error { return nil }

// echoFailTransport responds successfully but with IR0 bits 7:6 = 00 (i.e.
// the chip never entered EEPROM mode), so ReadWord must reject the result.
type echoFailTransport struct{}

func (e *echoFailTransport) SetOutputReport(_ byte, _ []byte) (int, error) { return 5, nil }

func (e *echoFailTransport) GetInputReport(_ byte, buf []byte) (int, error) {
	if len(buf) < 5 {
		return 0, errors.New("buf too small")
	}

	buf[0] = 0
	buf[1] = 0 // IR0[7:6] == 00 → not in EEPROM mode
	buf[2] = 0
	buf[3] = 0
	buf[4] = 0

	return 5, nil
}
func (e *echoFailTransport) Close() error { return nil }

// injectErrTransport returns a configurable error from either method.
type injectErrTransport struct {
	setErr error
	getErr error
}

func (i *injectErrTransport) SetOutputReport(_ byte, _ []byte) (int, error) {
	if i.setErr != nil {
		return 0, i.setErr
	}
	return 5, nil
}

func (i *injectErrTransport) GetInputReport(_ byte, buf []byte) (int, error) {
	if i.getErr != nil {
		return 0, i.getErr
	}
	if len(buf) >= 5 {
		buf[1] = 0x80
	}
	return 5, nil
}
func (i *injectErrTransport) Close() error { return nil }

// readBackLiar wraps a real transport and, on a single targeted word
// address, returns garbage on the verify-read after a write. All other
// addresses behave normally.
type readBackLiar struct {
	inner    hidx.Transport
	lieAddr  uint8
	lastAddr uint8
}

func (l *readBackLiar) SetOutputReport(reportID byte, buf []byte) (int, error) {
	// Stash the addr from the OR3 byte so GetInputReport knows whether to lie.
	if len(buf) >= 5 {
		l.lastAddr = buf[4] & 0x3F
	}

	return l.inner.SetOutputReport(reportID, buf)
}

func (l *readBackLiar) GetInputReport(reportID byte, buf []byte) (int, error) {
	n, err := l.inner.GetInputReport(reportID, buf)
	if err != nil {
		return n, err
	}
	if l.lastAddr == l.lieAddr && len(buf) >= 5 {
		buf[2] ^= 0xFF
		buf[3] ^= 0xFF
	}
	return n, nil
}
func (l *readBackLiar) Close() error { return l.inner.Close() }

// orderTracker records the address every WriteImage write transfer targets.
// Read transfers (verifies) are intentionally not tracked.
type orderTracker struct {
	inner      hidx.Transport
	writeAddrs []uint8
}

func (o *orderTracker) SetOutputReport(reportID byte, buf []byte) (int, error) {
	if len(buf) >= 5 {
		or3 := buf[4]
		op := or3 >> 6
		// Only count actual writes (op = 0b11), not address-staging for reads.
		if op == 0b11 {
			o.writeAddrs = append(o.writeAddrs, or3&0x3F)
		}
	}
	return o.inner.SetOutputReport(reportID, buf)
}

func (o *orderTracker) GetInputReport(reportID byte, buf []byte) (int, error) {
	return o.inner.GetInputReport(reportID, buf)
}
func (o *orderTracker) Close() error { return o.inner.Close() }

// fakeTransport is the helper bundle the new tests share — backs onto the
// hidx.FakeBackend, pre-loaded with OpenVLMDefaults so a successful
// WriteImage round-trip is a one-liner.
type fakeTransport struct {
	transport hidx.Transport
	state     *hidx.FakeDeviceState
}

func newFakeTransport(t *testing.T) *fakeTransport {
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

	tr, err := b.Open("/dev/fake0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = tr.Close() })

	return &fakeTransport{transport: tr, state: state}
}
