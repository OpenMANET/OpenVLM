package eeprom

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
)

// Sentinel errors from update parsing so callers can map them to specific
// CLI exit codes if desired.
var (
	// ErrFieldLocked is returned when the user tries to `update vid` or
	// `update pid`. Those fields are not user-programmable; see the plan's
	// "VID/PID are write-locked" section.
	ErrFieldLocked = errors.New("eeprom: field is not user-programmable")

	// ErrFieldUnknown is returned for an unrecognized field name.
	ErrFieldUnknown = errors.New("eeprom: unknown field")

	// ErrHexInput is returned when a numeric value uses 0x/0o/0b notation.
	// The CLI policy is "no hex in user input"; values are always decimal.
	ErrHexInput = errors.New("eeprom: hex/binary/octal numeric input is not accepted; use decimal")
)

// ApplyUpdate parses `value` according to the type of the named field and
// returns a copy of `base` with that one field updated. The caller is
// expected to Validate the returned View before writing.
//
// `value` is in the same human-readable form as YAML scalars and CLI flags:
// integer dB written in decimal, plain string for string fields,
// `true`/`false` for booleans, named values (`12db`, `headset`, ...) for
// enums. **No hex.**
//
//nolint:gocognit,gocyclo // one switch arm per documented field; splitting hurts readability
func ApplyUpdate(base View, field, value string) (View, error) {
	switch Field(field) {
	case "vid", "pid":
		return base, fmt.Errorf("%w: %q (VID/PID are sourced from compiled-in OpenVLM constants)",
			ErrFieldLocked, field)

	case FieldProductString, FieldManufacturerString:
		return base, fmt.Errorf("%w: %q (product/manufacturer strings are sourced from compiled-in OpenVLM defaults)",
			ErrFieldLocked, field)

	case FieldExtendedFieldsValid:
		b, err := parseBool(field, value)
		if err != nil {
			return base, err
		}

		base.ExtendedFieldsValid = b
	case FieldSerialEnable:
		b, err := parseBool(field, value)
		if err != nil {
			return base, err
		}

		base.SerialEnable = b

	case FieldSerial:
		base.Serial = value

	case FieldDACInitVolume:
		n, err := parseDecimalInt(field, value)
		if err != nil {
			return base, err
		}

		base.DACInitVolume = n
	case FieldADCInitVolume:
		n, err := parseDecimalInt(field, value)
		if err != nil {
			return base, err
		}

		base.ADCInitVolume = n
	case FieldDACMaxMinVolumeValid:
		b, err := parseBool(field, value)
		if err != nil {
			return base, err
		}

		base.DACMaxMinVolumeValid = b
	case FieldADCMaxMinVolumeValid:
		b, err := parseBool(field, value)
		if err != nil {
			return base, err
		}

		base.ADCMaxMinVolumeValid = b
	case FieldAAMaxMinVolumeValid:
		b, err := parseBool(field, value)
		if err != nil {
			return base, err
		}

		base.AAMaxMinVolumeValid = b

	case FieldAAInitVolume:
		n, err := parseDecimalInt(field, value)
		if err != nil {
			return base, err
		}

		base.AAInitVolume = n
	case FieldBoostMode:
		bm := BoostMode(strings.ToLower(value))
		if !IsBoostMode(bm) {
			return base, fmt.Errorf("eeprom: %s: %q is not one of: %s, %s",
				field, value, Boost12dB, Boost22dB)
		}

		base.BoostMode = bm
	case FieldDACShutdown:
		b, err := parseBool(field, value)
		if err != nil {
			return base, err
		}

		base.DACShutdown = b
	case FieldTotalPowerControl:
		b, err := parseBool(field, value)
		if err != nil {
			return base, err
		}

		base.TotalPowerControl = b
	case FieldMicHighPassFilter:
		b, err := parseBool(field, value)
		if err != nil {
			return base, err
		}

		base.MicHighPassFilter = b
	case FieldMicPLLAdjust:
		b, err := parseBool(field, value)
		if err != nil {
			return base, err
		}

		base.MicPLLAdjust = b
	case FieldMicBoost:
		b, err := parseBool(field, value)
		if err != nil {
			return base, err
		}

		base.MicBoost = b
	case FieldDACOutput:
		do := DACOutput(strings.ToLower(value))
		if !IsDACOutput(do) {
			return base, fmt.Errorf("eeprom: %s: %q is not one of: %s, %s",
				field, value, DACOutputSpeaker, DACOutputHeadset)
		}

		base.DACOutput = do
	case FieldHIDEnable:
		b, err := parseBool(field, value)
		if err != nil {
			return base, err
		}

		base.HIDEnable = b
	case FieldRemoteWakeup:
		b, err := parseBool(field, value)
		if err != nil {
			return base, err
		}

		base.RemoteWakeup = b

	case FieldDACMinVolume:
		n, err := parseDecimalInt(field, value)
		if err != nil {
			return base, err
		}

		base.DACMinVolume = n
	case FieldDACMaxVolume:
		n, err := parseDecimalInt(field, value)
		if err != nil {
			return base, err
		}

		base.DACMaxVolume = n
	case FieldADCMinVolume:
		n, err := parseDecimalInt(field, value)
		if err != nil {
			return base, err
		}

		base.ADCMinVolume = n
	case FieldADCMaxVolume:
		n, err := parseDecimalInt(field, value)
		if err != nil {
			return base, err
		}

		base.ADCMaxVolume = n
	case FieldAAMinVolume:
		n, err := parseDecimalInt(field, value)
		if err != nil {
			return base, err
		}

		base.AAMinVolume = n
	case FieldAAMaxVolume:
		n, err := parseDecimalInt(field, value)
		if err != nil {
			return base, err
		}

		base.AAMaxVolume = n

	default:
		return base, fmt.Errorf("%w: %q (known fields: %s)", ErrFieldUnknown, field, FieldList())
	}

	return base, nil
}

// parseBool accepts only the canonical YAML/CLI true/false spellings.
func parseBool(field, value string) (bool, error) {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "true", "yes", "1", "on":
		return true, nil
	case "false", "no", "0", "off":
		return false, nil
	}

	return false, fmt.Errorf("eeprom: %s: %q is not a boolean (use true/false)", field, value)
}

// parseDecimalInt enforces the no-hex-input rule and parses a signed decimal
// integer. Anything containing 0x / 0X / 0o / 0b prefixes or non-digit
// characters (other than a leading minus sign) is rejected.
func parseDecimalInt(field, value string) (int, error) {
	v := strings.TrimSpace(value)
	if v == "" {
		return 0, fmt.Errorf("eeprom: %s: empty value", field)
	}

	body := v
	if body[0] == '-' || body[0] == '+' {
		body = body[1:]
	}

	if len(body) >= 2 && body[0] == '0' && (body[1] == 'x' || body[1] == 'X' ||
		body[1] == 'b' || body[1] == 'B' || body[1] == 'o' || body[1] == 'O') {
		return 0, fmt.Errorf("%w: %s: %q", ErrHexInput, field, value)
	}

	for _, r := range body {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("eeprom: %s: %q contains non-decimal characters",
				field, value)
		}
	}

	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("eeprom: %s: parse %q: %w", field, value, err)
	}

	return n, nil
}
