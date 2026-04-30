package eeprom

import (
	"errors"
	"fmt"
	"strings"
)

// Field identifies one user-facing EEPROM field by its YAML/CLI name. Every
// validation error and every error message returned by `update <field>`
// uses one of these values so the messages stay consistent and discoverable.
type Field string

// All user-facing field names. Keep these in lockstep with the YAML tags on
// View / PartialView and the kebab-case flag names registered in cmd/flags.go.
//
//nolint:revive // exported constants form the API surface
const (
	FieldExtendedFieldsValid  Field = "extended-fields-valid"
	FieldSerialEnable         Field = "serial-enable"
	FieldSerial               Field = "serial"
	FieldProductString        Field = "product-string"
	FieldManufacturerString   Field = "manufacturer-string"
	FieldDACInitVolume        Field = "dac-init-volume"
	FieldADCInitVolume        Field = "adc-init-volume"
	FieldDACMaxMinVolumeValid Field = "dac-max-min-volume-valid"
	FieldADCMaxMinVolumeValid Field = "adc-max-min-volume-valid"
	FieldAAMaxMinVolumeValid  Field = "aa-max-min-volume-valid"
	FieldAAInitVolume         Field = "aa-init-volume"
	FieldBoostMode            Field = "boost-mode"
	FieldDACShutdown          Field = "dac-shutdown"
	FieldTotalPowerControl    Field = "total-power-control"
	FieldMicHighPassFilter    Field = "mic-high-pass-filter" //nolint:gosec // not a credential, just a kebab-case field name
	FieldMicPLLAdjust         Field = "mic-pll-adjust"
	FieldMicBoost             Field = "mic-boost"
	FieldDACOutput            Field = "dac-output"
	FieldHIDEnable            Field = "hid-enable"
	FieldRemoteWakeup         Field = "remote-wakeup"
	FieldDACMinVolume         Field = "dac-min-volume"
	FieldDACMaxVolume         Field = "dac-max-volume"
	FieldADCMinVolume         Field = "adc-min-volume"
	FieldADCMaxVolume         Field = "adc-max-volume"
	FieldAAMinVolume          Field = "aa-min-volume"
	FieldAAMaxVolume          Field = "aa-max-volume"
)

// Documented field ranges. Volumes are integer dB. The min/max overrides
// (DACMinVolume etc.) are entered in dB and encoded into the high byte of
// a 16-bit word, so their hard ceiling is the int8 range.
//
// Init-volume ranges are the intersection of (a) the dB range the datasheet
// documents in §7.1.3 / §8.3 and (b) what the current two's-complement
// encoder in image.go can faithfully represent given the bit-field width.
// Until a hardware bench gate confirms whether the chip uses two's
// complement or offset-binary for these fields (see plan Phase G), the
// validator refuses any input the encoder would silently corrupt.
//
//   - DAC init: 7-bit signed → -64..63; datasheet doc range -37..0 is
//     the intersection.
//   - ADC init: 6-bit signed → -32..31; datasheet doc range -12..+23 is
//     the intersection.
//   - AA  init: 5-bit signed → -16..15; datasheet doc range is -23..+8.
//     Intersection is -16..8 (loses access to -17..-23 until the
//     encoding is locked).
const (
	dacInitMin = -37
	dacInitMax = 0
	adcInitMin = -12
	adcInitMax = 23
	aaInitMin  = -16
	aaInitMax  = 8

	minMaxFloor = -128
	minMaxCeil  = 127
)

// Validate runs every documented per-field range check and every cross-field
// constraint. It is the single gate every write path goes through —
// `provision`, `write -i image.bin`, and `update` all call it before any
// HID transfer happens, so a bad value never reaches the chip.
//
// The returned error is either nil or a multi-error (use errors.Join /
// errors.Is to inspect) that names every offending field.
func (v *View) Validate() error {
	var errs []error

	check := func(field Field, ok bool, msg string) {
		if !ok {
			errs = append(errs, fmt.Errorf("eeprom: %s: %s", field, msg))
		}
	}

	checkRange := func(field Field, value, low, high int) {
		if value < low || value > high {
			errs = append(errs, fmt.Errorf("eeprom: %s: value %d out of range [%d, %d]",
				field, value, low, high))
		}
	}

	checkASCII := func(field Field, s string, max int) {
		if len(s) > max {
			errs = append(errs, fmt.Errorf("eeprom: %s: %d characters exceeds %d-character limit",
				field, len(s), max))

			return
		}

		for i, b := range []byte(s) {
			if b < 0x20 || b > 0x7E {
				errs = append(errs, fmt.Errorf("eeprom: %s: byte %d (0x%02X) is not printable ASCII",
					field, i, b))

				return
			}
		}
	}

	// String checks.
	checkASCII(FieldSerial, v.Serial, maxSerialBytes)
	checkASCII(FieldProductString, v.ProductString, maxProductBytes)
	checkASCII(FieldManufacturerString, v.ManufacturerString, maxMfrBytes)

	// Init-volume range checks.
	checkRange(FieldDACInitVolume, v.DACInitVolume, dacInitMin, dacInitMax)
	checkRange(FieldADCInitVolume, v.ADCInitVolume, adcInitMin, adcInitMax)
	checkRange(FieldAAInitVolume, v.AAInitVolume, aaInitMin, aaInitMax)

	// Min/max override range checks.
	checkRange(FieldDACMinVolume, v.DACMinVolume, minMaxFloor, minMaxCeil)
	checkRange(FieldDACMaxVolume, v.DACMaxVolume, minMaxFloor, minMaxCeil)
	checkRange(FieldADCMinVolume, v.ADCMinVolume, minMaxFloor, minMaxCeil)
	checkRange(FieldADCMaxVolume, v.ADCMaxVolume, minMaxFloor, minMaxCeil)
	checkRange(FieldAAMinVolume, v.AAMinVolume, minMaxFloor, minMaxCeil)
	checkRange(FieldAAMaxVolume, v.AAMaxVolume, minMaxFloor, minMaxCeil)

	// Cross-field: min ≤ max.
	check(FieldDACMaxVolume, v.DACMinVolume <= v.DACMaxVolume,
		fmt.Sprintf("dac-min-volume (%d) > dac-max-volume (%d)", v.DACMinVolume, v.DACMaxVolume))
	check(FieldADCMaxVolume, v.ADCMinVolume <= v.ADCMaxVolume,
		fmt.Sprintf("adc-min-volume (%d) > adc-max-volume (%d)", v.ADCMinVolume, v.ADCMaxVolume))
	check(FieldAAMaxVolume, v.AAMinVolume <= v.AAMaxVolume,
		fmt.Sprintf("aa-min-volume (%d) > aa-max-volume (%d)", v.AAMinVolume, v.AAMaxVolume))

	// Cross-field: init within [min, max].
	check(FieldDACInitVolume,
		v.DACInitVolume >= v.DACMinVolume && v.DACInitVolume <= v.DACMaxVolume,
		fmt.Sprintf("dac-init-volume (%d) not in [%d, %d]",
			v.DACInitVolume, v.DACMinVolume, v.DACMaxVolume))
	check(FieldADCInitVolume,
		v.ADCInitVolume >= v.ADCMinVolume && v.ADCInitVolume <= v.ADCMaxVolume,
		fmt.Sprintf("adc-init-volume (%d) not in [%d, %d]",
			v.ADCInitVolume, v.ADCMinVolume, v.ADCMaxVolume))
	check(FieldAAInitVolume,
		v.AAInitVolume >= v.AAMinVolume && v.AAInitVolume <= v.AAMaxVolume,
		fmt.Sprintf("aa-init-volume (%d) not in [%d, %d]",
			v.AAInitVolume, v.AAMinVolume, v.AAMaxVolume))

	// Enums.
	check(FieldBoostMode, IsBoostMode(v.BoostMode),
		fmt.Sprintf("boost-mode %q is not one of: %s, %s",
			v.BoostMode, Boost12dB, Boost22dB))
	check(FieldDACOutput, IsDACOutput(v.DACOutput),
		fmt.Sprintf("dac-output %q is not one of: %s, %s",
			v.DACOutput, DACOutputSpeaker, DACOutputHeadset))

	if len(errs) == 0 {
		return nil
	}

	// Aggregate so the user sees every problem at once, not one per run.
	return errors.Join(errs...)
}

// AllFields returns every user-facing field name. Used by `update --help`,
// `flags.go`, and tests that need to ensure new fields aren't missed.
func AllFields() []Field {
	return []Field{
		FieldExtendedFieldsValid,
		FieldSerialEnable,
		FieldSerial,
		FieldDACInitVolume,
		FieldADCInitVolume,
		FieldDACMaxMinVolumeValid,
		FieldADCMaxMinVolumeValid,
		FieldAAMaxMinVolumeValid,
		FieldAAInitVolume,
		FieldBoostMode,
		FieldDACShutdown,
		FieldTotalPowerControl,
		FieldMicHighPassFilter,
		FieldMicPLLAdjust,
		FieldMicBoost,
		FieldDACOutput,
		FieldHIDEnable,
		FieldRemoteWakeup,
		FieldDACMinVolume,
		FieldDACMaxVolume,
		FieldADCMinVolume,
		FieldADCMaxVolume,
		FieldAAMinVolume,
		FieldAAMaxVolume,
	}
}

// FieldList returns a comma-separated list of every field name. Convenience
// for help text.
func FieldList() string {
	names := make([]string, 0, len(AllFields()))

	for _, f := range AllFields() {
		names = append(names, string(f))
	}

	return strings.Join(names, ", ")
}
