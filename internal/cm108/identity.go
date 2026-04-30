package cm108

import (
	"fmt"

	"github.com/openmanet/openvlm/internal/hidx"
)

// CheckOpenVLMIdentity returns true when the GPIO1 hardware strap reads high
// (= OpenVLM).
//
// Per CM108B datasheet §7.4, the four-byte input report contains IR0..IR3.
// GPIO1 is bit 0 of IR1, but only when HID_OR0[7:6] == 0b00 (default
// GPIO-input mode). The chip's HID register state PERSISTS across USB
// handle close — if a prior session left it in EEPROM-access mode (e.g. an
// `openvlm read` that didn't reset), IR1 would be the EEPROM data buffer,
// not GPIO state. To make this probe deterministic regardless of prior
// state, we first send a Set_Output_Report with HID_OR0=0 to force the
// chip back into GPIO-input mode, then issue the input-report read.
func CheckOpenVLMIdentity(t hidx.Transport) (bool, error) {
	// Force HID_OR0 = 0 so IR1 reflects GPIO state, not EEPROM data.
	reset := []byte{0, 0, 0, 0, 0}
	if _, err := t.SetOutputReport(0, reset); err != nil {
		return false, fmt.Errorf("cm108: reset HID_OR0 for GPIO probe: %w", err)
	}

	buf := make([]byte, 5)
	if _, err := t.GetInputReport(0, buf); err != nil {
		return false, fmt.Errorf("cm108: probe GPIO1 strap: %w", err)
	}

	if buf[1]&0xC0 != 0 {
		return false, fmt.Errorf("cm108: HID_IR0[7:6]=0x%x after reset, chip is not in GPIO-input mode", (buf[1]>>6)&0x3)
	}

	return buf[2]&0x01 != 0, nil
}
