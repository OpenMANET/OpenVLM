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

// TestWipeAll_PatternFFInvalidatesMagic confirms WipeAll with 0xFFFF
// writes every word to the all-1s pattern and the resulting image is
// 'unprogrammed' (magic word does not match).
func TestWipeAll_PatternFFInvalidatesMagic(t *testing.T) {
	t.Parallel()

	tr := newFakeTransport(t)

	require.NoError(t, eeprom.WipeAll(tr.transport, 0xFFFF))

	got := tr.state.EEPROM()
	for i, b := range got {
		require.Equalf(t, byte(0xFF), b, "byte %d after wipe", i)
	}

	var img eeprom.Image
	copy(img[:], got[:])
	assert.False(t, img.IsProgrammed(),
		"a wiped chip must read as unprogrammed (magic word invalid)")
}

// TestWipeAll_PatternZeroInvalidatesMagic mirrors the above for 0x0000.
func TestWipeAll_PatternZeroInvalidatesMagic(t *testing.T) {
	t.Parallel()

	tr := newFakeTransport(t)

	require.NoError(t, eeprom.WipeAll(tr.transport, 0x0000))

	got := tr.state.EEPROM()
	for i, b := range got {
		require.Equalf(t, byte(0x00), b, "byte %d after wipe", i)
	}

	var img eeprom.Image
	copy(img[:], got[:])
	assert.False(t, img.IsProgrammed())
}

// TestWipeAll_VerifyMismatchSurfaces ensures a flaky read-back during
// wipe is reported as VerifyError just like a normal write.
func TestWipeAll_VerifyMismatchSurfaces(t *testing.T) {
	t.Parallel()

	tr := newFakeTransport(t)
	wrapper := &readBackLiar{inner: tr.transport, lieAddr: 0x10}

	err := eeprom.WipeAll(wrapper, 0xFFFF)
	require.Error(t, err)

	var verr *eeprom.VerifyError
	require.ErrorAs(t, err, &verr)
	assert.Equal(t, uint8(0x10), verr.Addr)
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

// TestReadWord_RetriesTransientSetOutputError confirms a transient
// SetOutputReport error (the macOS IOKit kIOReturnError class) is absorbed
// by the protocol-layer retry. The transport fails N-1 times, succeeds on
// attempt N, and ReadWord returns the expected value.
func TestReadWord_RetriesTransientSetOutputError(t *testing.T) {
	t.Parallel()

	tr := newFakeTransport(t)
	wrapper := &flakySetTransport{
		inner:        tr.transport,
		failsRemain:  2, // the 3rd attempt succeeds
		transientErr: errors.New("simulated kIOReturnError"),
	}

	tr.state.SetEEPROMWord(0x05, 0xBEEF)

	got, err := eeprom.ReadWord(wrapper, 0x05)
	require.NoError(t, err, "ReadWord must absorb 2 transient SetOutputReport errors")
	assert.Equal(t, uint16(0xBEEF), got)
	assert.Equal(t, 2, wrapper.failedCalls,
		"both transient failures must have been encountered")
}

// TestReadWord_FailsAfterRetryBudget proves the retry loop is bounded —
// a transport that never recovers surfaces the underlying error after
// the budget is spent.
func TestReadWord_FailsAfterRetryBudget(t *testing.T) {
	t.Parallel()

	tr := newFakeTransport(t)
	persistentErr := errors.New("permanent fault")
	wrapper := &flakySetTransport{
		inner:        tr.transport,
		failsRemain:  100, // far more than transferRetries
		transientErr: persistentErr,
	}

	_, err := eeprom.ReadWord(wrapper, 0x05)
	require.Error(t, err)
	assert.True(t, errors.Is(err, persistentErr),
		"the underlying error must propagate once retries are exhausted")
}

// TestWriteAll_RetriesVerifyOnTransientReadFailure simulates the exact
// failure mode from the user's bug report: the verify-read after
// WriteWord(0x06) errored once with kIOReturnError. The new retry path
// must absorb the single transient and let WriteAll complete.
func TestWriteAll_RetriesVerifyOnTransientReadFailure(t *testing.T) {
	t.Parallel()

	tr := newFakeTransport(t)
	wrapper := &flakyVerifyTransport{
		inner:       tr.transport,
		flakyAddr:   0x06,
		failsRemain: 1, // matches the user's "once" report
		err:         errors.New("simulated kIOReturnError"),
	}

	var tail [eeprom.WordCount - 0x33]uint16

	img := eeprom.OpenVLMDefaults.Encode(cm108.OpenVLMVendorID, cm108.OpenVLMProductID, tail)

	require.NoError(t, eeprom.WriteImage(wrapper, img),
		"WriteImage must absorb one transient verify-read failure")
	assert.Equal(t, 1, wrapper.failedCalls,
		"the injected failure must have actually fired once")
}

// flakySetTransport wraps a real transport and fails the first
// `failsRemain` SetOutputReport calls before passing through.
type flakySetTransport struct {
	inner        hidx.Transport
	transientErr error
	failsRemain  int
	failedCalls  int
}

func (f *flakySetTransport) SetOutputReport(reportID byte, buf []byte) (int, error) {
	if f.failsRemain > 0 {
		f.failsRemain--
		f.failedCalls++

		return 0, f.transientErr
	}

	return f.inner.SetOutputReport(reportID, buf) //nolint:wrapcheck // pass-through to wrapped transport
}

func (f *flakySetTransport) GetInputReport(reportID byte, buf []byte) (int, error) {
	return f.inner.GetInputReport(reportID, buf) //nolint:wrapcheck // pass-through to wrapped transport
}
func (f *flakySetTransport) Close() error { return f.inner.Close() }

// flakyVerifyTransport fails GetInputReport on a specific addr the first
// `failsRemain` times the host sends a read for that addr. Used to
// simulate the user-reported macOS IOKit blip on a single verify-read.
type flakyVerifyTransport struct {
	inner       hidx.Transport
	err         error
	lastAddr    uint8
	flakyAddr   uint8
	failsRemain int
	failedCalls int
}

func (f *flakyVerifyTransport) SetOutputReport(reportID byte, buf []byte) (int, error) {
	if len(buf) >= 5 {
		f.lastAddr = buf[4] & 0x3F
	}

	return f.inner.SetOutputReport(reportID, buf) //nolint:wrapcheck // pass-through to wrapped transport
}

func (f *flakyVerifyTransport) GetInputReport(reportID byte, buf []byte) (int, error) {
	if f.lastAddr == f.flakyAddr && f.failsRemain > 0 {
		f.failsRemain--
		f.failedCalls++

		return 0, f.err
	}

	return f.inner.GetInputReport(reportID, buf) //nolint:wrapcheck // pass-through to wrapped transport
}
func (f *flakyVerifyTransport) Close() error { return f.inner.Close() }

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

	return l.inner.SetOutputReport(reportID, buf) //nolint:wrapcheck // pass-through to wrapped transport
}

func (l *readBackLiar) GetInputReport(reportID byte, buf []byte) (int, error) {
	n, err := l.inner.GetInputReport(reportID, buf)
	if err != nil {
		return n, err //nolint:wrapcheck // pass-through to wrapped transport
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

	return o.inner.SetOutputReport(reportID, buf) //nolint:wrapcheck // pass-through to wrapped transport
}

func (o *orderTracker) GetInputReport(reportID byte, buf []byte) (int, error) {
	return o.inner.GetInputReport(reportID, buf) //nolint:wrapcheck // pass-through to wrapped transport
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
