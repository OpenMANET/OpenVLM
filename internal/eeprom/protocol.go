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

// ReadWord issues one EEPROM-read transfer for the given word address and
// returns the 16-bit result. Address must be in [0, WordCount).
func ReadWord(t hidx.Transport, addr uint8) (uint16, error) {
	if int(addr) >= WordCount {
		return 0, fmt.Errorf("eeprom: ReadWord: address 0x%02X out of range", addr)
	}

	out := []byte{0, or0EEPROMMode, 0, 0, opEEPROMRead<<6 | (addr & addrMask)}
	if _, err := t.SetOutputReport(0, out); err != nil {
		return 0, fmt.Errorf("eeprom: ReadWord 0x%02X: send output: %w", addr, err)
	}

	in := make([]byte, reportLen)
	if _, err := t.GetInputReport(0, in); err != nil {
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
// the chip has committed the value.
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
	if _, err := t.SetOutputReport(0, out); err != nil {
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
func WriteAll(t hidx.Transport, img Image) error {
	for addr := uint8(0); int(addr) < WordCount; addr++ {
		want := img.Word(addr)
		if err := WriteWord(t, addr, want); err != nil {
			return err
		}

		got, err := ReadWord(t, addr)
		if err != nil {
			return fmt.Errorf("verify read 0x%02X: %w", addr, err)
		}

		if got != want {
			return &VerifyError{Addr: addr, Wanted: want, GotValue: got}
		}
	}

	return nil
}

// WriteImage is the high-level write that the CLI uses. It enforces the
// VID/PID write-lock (rejects images whose words 0x01/0x02 do not match
// the OpenVLM constants), then calls WriteAll.
//
// This is the single chokepoint for the VID/PID guard — both raw `.bin`
// uploads and YAML-derived images go through here, so a mismatch is
// impossible to bypass without modifying this function.
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
