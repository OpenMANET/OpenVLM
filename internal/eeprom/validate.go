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
// Init-volume fields are attenuation-encoded (bits = maxDB - value, bits 0
// = loudest), bench-confirmed 2026-04-30. Two's complement silenced the
// DAC; plain offset-binary inverted the direction. Maximum-dB constants
// live in layout.go as dac/adc/aaInitMaxDB. Encoder and validator must
// move together — see CLAUDE.md "memory bug class to watch for".
const (
	dacInitMin = -37 // datasheet §8.3: -37..0 dB, 38 steps in 7-bit field
	dacInitMax = 0
	adcInitMin = -12 // datasheet §8.3: -12..+23 dB, 36 steps in 6-bit field
	adcInitMax = 23
	aaInitMin  = -23 // datasheet §8.3: -23..+8 dB, 32 steps in 5-bit field
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
