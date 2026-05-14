package eeprom

import (
	"errors"
	"fmt"
	"time"

	"github.com/openmanet/openvlm/internal/cm108"
	"github.com/openmanet/openvlm/internal/hidx"
)

// CM108B HID protocol constants — datasheet §7.4.
//
//	Set_Output_Report (5 bytes):
//	  byte0 = report ID (0)
//	  byte1 = HID_OR0  (0x80 = EEPROM access mode)
//	  byte2 = HID_OR1  (write data low byte)
//	  byte3 = HID_OR2  (write data high byte)
//	  byte4 = HID_OR3  ((op<<6) | addr)   op = 0b10 READ, 0b11 WRITE
const (
	or0EEPROMMode byte = 0x80

	opEEPROMRead  byte = 0b10
	opEEPROMWrite byte = 0b11
	addrMask      byte = 0x3F

	reportLen = 5

	// tWP — datasheet "Write Cycle Time": 3 ms typical, 10 ms max. We sleep
	// the worst-case figure; this path is rarely exercised so the cost is
	// negligible (128 bytes / 2 = 64 word writes × 10 ms = 640 ms total).
	writeCycleDelay = 10 * time.Millisecond

	// readPaceDelay paces back-to-back ReadWord calls. macOS IOKit returns
	// kIOReturnError (0xE00002BC, "general error") when a flood of
	// Set/Get_Report control transfers arrives faster than the host
	// controller can drain them. 1 ms is enough to avoid the failure on
	// the hardware seen so far without making a 64-word ReadAll noticeably
	// slow.
	readPaceDelay = 1 * time.Millisecond

	// readExecuteDelay is the gap between the SetOutputReport that issues
	// the EEPROM-read command and the GetInputReport that fetches the
	// result. The CM108B's internal SPI bus must clock 16 bits out of the
	// 93C46 before HID_IR2/IR3 hold the requested word — without this
	// delay, GetInputReport races the chip and returns stale buffer data
	// (different bytes on every read; reproducible on Windows). The
	// Linux hid-cm108 driver uses 1..2 ms here; we use 2 ms for margin.
	readExecuteDelay = 2 * time.Millisecond

	// preVerifyDelay paces the gap between WriteWord's tWP sleep and the
	// next verify ReadWord transfer in WriteAll. Same IOKit blip class as
	// readPaceDelay but on the write→read transition; without this, a
	// 64-word WriteImage can fail mid-loop with kIOReturnError on Mac.
	preVerifyDelay = 2 * time.Millisecond

	// transferRetries is the maximum number of SetOutputReport /
	// GetInputReport attempts inside ReadWord / WriteWord. The first
	// attempt is the success case; the remaining attempts handle macOS
	// IOKit's transient kIOReturnError under host-controller pressure.
	// 3 was chosen as the smallest value that comfortably absorbed every
	// flake observed on the bench; total worst-case latency is bounded at
	// transferBackoff*(2^N - 1) ~= 35 ms per call.
	transferRetries = 3

	// transferBackoff is the initial sleep between transient retries; it
	// doubles each attempt.
	transferBackoff = 5 * time.Millisecond

	// verifyRetries is the maximum number of verify-read attempts after
	// a WriteWord. Covers BOTH the IOKit blip and chip-tWP variance —
	// either a transport error or a wrong value triggers the retry. The
	// inner ReadWord already retries transport errors via transferRetries,
	// so this loop's retries are mostly insurance against chip commit
	// timing on the slow end of the datasheet's 0.1..10 ms tWP range.
	verifyRetries = 3
)

// ErrVerifyMismatch is returned when WriteImage's read-back loop sees a
// different value than what was written. Carries the offending word
// address so the caller can include it in a helpful error message.
var ErrVerifyMismatch = errors.New("eeprom: read-back mismatch after write")

// VerifyError augments ErrVerifyMismatch with the offending word address
// and the values seen.
type VerifyError struct {
	Addr     uint8
	Wanted   uint16
	GotValue uint16
}

func (e *VerifyError) Error() string {
	return fmt.Sprintf("eeprom: word 0x%02X read-back mismatch: wrote 0x%04X, read 0x%04X",
		e.Addr, e.Wanted, e.GotValue)
}

func (e *VerifyError) Unwrap() error { return ErrVerifyMismatch }

// setOutputReportRetry wraps Transport.SetOutputReport with a bounded
// retry loop. macOS IOKit occasionally returns kIOReturnError under load;
// the failure is transient and clears within milliseconds, so a small
// retry budget is enough to make WriteImage / ReadAll resilient without
// masking real transport failures.
func setOutputReportRetry(t hidx.Transport, reportID byte, buf []byte) error {
	var (
		err     error
		backoff = transferBackoff
	)

	for attempt := 0; attempt < transferRetries; attempt++ {
		if _, err = t.SetOutputReport(reportID, buf); err == nil {
			return nil
		}

		if attempt < transferRetries-1 {
			time.Sleep(backoff)

			backoff *= 2
		}
	}

	return err //nolint:wrapcheck // caller wraps with the address context
}

// getInputReportRetry mirrors setOutputReportRetry for the read path.
func getInputReportRetry(t hidx.Transport, reportID byte, buf []byte) error {
	var (
		err     error
		backoff = transferBackoff
	)

	for attempt := 0; attempt < transferRetries; attempt++ {
		if _, err = t.GetInputReport(reportID, buf); err == nil {
			return nil
		}

		if attempt < transferRetries-1 {
			time.Sleep(backoff)

			backoff *= 2
		}
	}

	return err //nolint:wrapcheck // caller wraps with the address context
}

// ReadWord issues one EEPROM-read transfer for the given word address and
// returns the 16-bit result. Address must be in [0, WordCount). Both the
// Set_Output and Get_Input control transfers are retried up to
// transferRetries times to absorb transient macOS IOKit errors.
func ReadWord(t hidx.Transport, addr uint8) (uint16, error) {
	if int(addr) >= WordCount {
		return 0, fmt.Errorf("eeprom: ReadWord: address 0x%02X out of range", addr)
	}

	out := []byte{0, or0EEPROMMode, 0, 0, opEEPROMRead<<6 | (addr & addrMask)}
	if err := setOutputReportRetry(t, 0, out); err != nil {
		return 0, fmt.Errorf("eeprom: ReadWord 0x%02X: send output: %w", addr, err)
	}

	// Wait for the chip to clock the 16-bit word out of the 93C46 SPI
	// EEPROM into HID_IR2/IR3 before fetching it. See readExecuteDelay.
	time.Sleep(readExecuteDelay)

	in := make([]byte, reportLen)
	if err := getInputReportRetry(t, 0, in); err != nil {
		return 0, fmt.Errorf("eeprom: ReadWord 0x%02X: get input: %w", addr, err)
	}

	if in[1]&0xC0 == 0 {
		return 0, fmt.Errorf("eeprom: ReadWord 0x%02X: chip did not echo EEPROM mode (IR0=0x%02X)",
			addr, in[1])
	}

	return uint16(in[2]) | (uint16(in[3]) << 8), nil
}

// WriteWord issues one EEPROM-write transfer for the given word address.
// Sleeps tWP after the transfer so the next operation does not start until
// the chip has committed the value. The Set_Output transfer is retried
// up to transferRetries times to absorb macOS IOKit transients.
func WriteWord(t hidx.Transport, addr uint8, value uint16) error {
	if int(addr) >= WordCount {
		return fmt.Errorf("eeprom: WriteWord: address 0x%02X out of range", addr)
	}

	out := []byte{0,
		or0EEPROMMode,
		byte(value & 0xFF),
		byte(value >> 8),
		opEEPROMWrite<<6 | (addr & addrMask),
	}
	if err := setOutputReportRetry(t, 0, out); err != nil {
		return fmt.Errorf("eeprom: WriteWord 0x%02X: send output: %w", addr, err)
	}

	time.Sleep(writeCycleDelay)

	return nil
}

// ReadAll reads every word into a fresh Image. The caller can then Decode()
// the image into a View.
func ReadAll(t hidx.Transport) (Image, error) {
	var img Image

	for addr := uint8(0); int(addr) < WordCount; addr++ {
		w, err := ReadWord(t, addr)
		if err != nil {
			return img, err
		}

		img.SetWord(addr, w)
		time.Sleep(readPaceDelay)
	}

	return img, nil
}

// WriteAll writes every word of the image and verifies each by reading it
// back. A mismatch returns *VerifyError wrapping ErrVerifyMismatch.
//
// Each verify read is attempted up to verifyRetries times with growing
// backoff, treating both transport errors and value mismatches as
// retryable. The inner ReadWord already retries transport errors via
// transferRetries; this outer loop adds tolerance for chip-tWP variance
// (datasheet says 0.1..10 ms; some chips need longer than the
// writeCycleDelay we sleep) and is the documented safety net behind the
// macOS IOKit blip the original `provision` failure surfaced.
func WriteAll(t hidx.Transport, img Image) error {
	for addr := uint8(0); int(addr) < WordCount; addr++ {
		want := img.Word(addr)
		if err := WriteWord(t, addr, want); err != nil {
			return err
		}

		// Pre-verify pace: gives macOS IOKit a small breather between
		// the write transfer and the verify-read transfer.
		time.Sleep(preVerifyDelay)

		var (
			got     uint16
			lastErr error
			backoff = transferBackoff
		)

		ok := false

		for attempt := 0; attempt < verifyRetries; attempt++ {
			if attempt > 0 {
				time.Sleep(backoff)

				backoff *= 2
			}

			got, lastErr = ReadWord(t, addr)
			if lastErr == nil && got == want {
				ok = true

				break
			}
		}

		if !ok {
			if lastErr != nil {
				return fmt.Errorf("verify read 0x%02X: %w", addr, lastErr)
			}

			return &VerifyError{Addr: addr, Wanted: want, GotValue: got}
		}
	}

	return nil
}

// WipeAll programs every word of the EEPROM to `pattern`, then verifies
// each word read back. Bypasses WriteImage's VID/PID guard because the
// whole point of a wipe is to invalidate the chip's identity bytes back
// to a virgin-like state; callers must explicitly opt in via this helper
// rather than handing arbitrary all-0xFF/all-0x00 images to WriteImage.
//
// `pattern` is the 16-bit word value written to every address. Typical
// choices:
//
//   - 0xFFFF — matches a virgin 93C46 (which ships erased to all-1s),
//     so post-wipe `openvlm read` looks identical to a fresh-from-factory
//     chip.
//   - 0x0000 — clears bits, also invalidates the magic word.
//
// Either choice clears word 0x00's magic nibble and causes the CM108B to
// fall back to its internal-ROM USB descriptors on the next enumeration.
func WipeAll(t hidx.Transport, pattern uint16) error {
	var img Image
	for addr := uint8(0); int(addr) < WordCount; addr++ {
		img.SetWord(addr, pattern)
	}

	return WriteAll(t, img)
}

// WriteImage is the high-level write that the CLI uses. It enforces the
// VID/PID write-lock (rejects images whose words 0x01/0x02 do not match
// the OpenVLM constants), then calls WriteAll.
//
// This is the single chokepoint for the VID/PID guard — both raw `.bin`
// uploads and YAML-derived images go through here, so a mismatch is
// impossible to bypass without modifying this function. The wipe verb
// uses WipeAll instead, which bypasses the guard for the documented
// reset-to-virgin use case.
func WriteImage(t hidx.Transport, img Image) error {
	if vid := img.VID(); vid != cm108.OpenVLMVendorID {
		return fmt.Errorf("eeprom: image VID 0x%04X does not match OpenVLM 0x%04X "+
			"(VID is write-locked; this CLI refuses to reprogram it)",
			vid, cm108.OpenVLMVendorID)
	}

	if pid := img.PID(); pid != cm108.OpenVLMProductID {
		return fmt.Errorf("eeprom: image PID 0x%04X does not match OpenVLM 0x%04X "+
			"(PID is write-locked; this CLI refuses to reprogram it)",
			pid, cm108.OpenVLMProductID)
	}

	return WriteAll(t, img)
}
