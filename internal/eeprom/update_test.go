package eeprom_test

import (
	"errors"
	"testing"

	"github.com/openmanet/openvlm/internal/eeprom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestApplyUpdate_RejectsHexInput exercises the no-hex-input policy on the
// `update` parser. Same enforcement as on the per-field CLI flags but at a
// different layer; both must match.
func TestApplyUpdate_RejectsHexInput(t *testing.T) {
	t.Parallel()

	bad := []string{"0x10", "0X10", "0b1010", "0o7", "-0x10"}

	for _, value := range bad {
		value := value

		t.Run(value, func(t *testing.T) {
			t.Parallel()

			_, err := eeprom.ApplyUpdate(eeprom.OpenVLMDefaults, "dac-init-volume", value)
			require.Error(t, err)
			assert.True(t, errors.Is(err, eeprom.ErrHexInput),
				"want ErrHexInput, got %v", err)
		})
	}
}

// TestApplyUpdate_LockedFields enforces the documented refusal to update
// VID, PID, product-string, or manufacturer-string via the `update` verb.
// A user typing `openvlm update vid 0x1234` (or `update product-string foo`)
// must get a fixed, recognizable error.
func TestApplyUpdate_LockedFields(t *testing.T) {
	t.Parallel()

	for _, field := range []string{"vid", "pid", "product-string", "manufacturer-string"} {
		field := field

		t.Run(field, func(t *testing.T) {
			t.Parallel()

			_, err := eeprom.ApplyUpdate(eeprom.OpenVLMDefaults, field, "anything")
			require.Error(t, err)
			assert.True(t, errors.Is(err, eeprom.ErrFieldLocked),
				"want ErrFieldLocked, got %v", err)
		})
	}
}

// TestApplyUpdate_UnknownField returns ErrFieldUnknown so the CLI can
// distinguish "typo" from "policy violation".
func TestApplyUpdate_UnknownField(t *testing.T) {
	t.Parallel()

	_, err := eeprom.ApplyUpdate(eeprom.OpenVLMDefaults, "nonexistent", "true")
	require.Error(t, err)
	assert.True(t, errors.Is(err, eeprom.ErrFieldUnknown),
		"want ErrFieldUnknown, got %v", err)
}

// TestApplyUpdate_EveryField walks every documented user-facing field
// through ApplyUpdate. The parser's switch dispatch has one arm per field;
// missing a case (typo, copy-paste bug, dropped field) shows up here as
// an ErrFieldUnknown for what should be a valid field name. Field-list
// drift is caught at the same time: AllFields() and the ApplyUpdate
// switch must agree.
func TestApplyUpdate_EveryField(t *testing.T) {
	t.Parallel()

	// Map every field to a value that ApplyUpdate must accept on top of
	// OpenVLMDefaults. Boolean / enum / int / string types are mixed
	// deliberately so a misclassified arm fails its assertion rather than
	// silently passing.
	values := map[eeprom.Field]string{
		eeprom.FieldExtendedFieldsValid:  "true",
		eeprom.FieldSerialEnable:         "false",
		eeprom.FieldSerial:               "abc-123",
		eeprom.FieldDACInitVolume:        "-12",
		eeprom.FieldADCInitVolume:        "0",
		eeprom.FieldDACMaxMinVolumeValid: "1",
		eeprom.FieldADCMaxMinVolumeValid: "yes",
		eeprom.FieldAAMaxMinVolumeValid:  "off",
		eeprom.FieldAAInitVolume:         "0",
		eeprom.FieldBoostMode:            "12db",
		eeprom.FieldDACShutdown:          "true",
		eeprom.FieldTotalPowerControl:    "false",
		eeprom.FieldMicHighPassFilter:    "true",
		eeprom.FieldMicPLLAdjust:         "false",
		eeprom.FieldMicBoost:             "true",
		eeprom.FieldDACOutput:            "speaker",
		eeprom.FieldHIDEnable:            "true",
		eeprom.FieldRemoteWakeup:         "false",
		eeprom.FieldDACMinVolume:         "-37",
		eeprom.FieldDACMaxVolume:         "0",
		eeprom.FieldADCMinVolume:         "-12",
		eeprom.FieldADCMaxVolume:         "23",
		eeprom.FieldAAMinVolume:          "-16",
		eeprom.FieldAAMaxVolume:          "8",
	}

	for _, f := range eeprom.AllFields() {
		f := f

		v, ok := values[f]
		if !ok {
			t.Fatalf("test fixture missing a value for %q — keep this map in sync with AllFields()", f)
		}

		t.Run(string(f), func(t *testing.T) {
			t.Parallel()

			_, err := eeprom.ApplyUpdate(eeprom.OpenVLMDefaults, string(f), v)
			require.NoErrorf(t, err, "ApplyUpdate(%q, %q) must succeed", f, v)
		})
	}
}

// TestApplyUpdate_BoolParserAcceptsAllForms exercises the synonym table
// in parseBool — yes/on/1/true and no/off/0/false plus rejection of
// anything else.
func TestApplyUpdate_BoolParserAcceptsAllForms(t *testing.T) {
	t.Parallel()

	for _, value := range []string{"true", "yes", "1", "on", "TRUE", "True"} {
		value := value
		t.Run("accept_"+value, func(t *testing.T) {
			t.Parallel()

			v, err := eeprom.ApplyUpdate(eeprom.OpenVLMDefaults, "mic-boost", value)
			require.NoError(t, err)
			assert.True(t, v.MicBoost)
		})
	}

	for _, value := range []string{"false", "no", "0", "off", "FALSE"} {
		value := value
		t.Run("reject_to_false_"+value, func(t *testing.T) {
			t.Parallel()

			v, err := eeprom.ApplyUpdate(eeprom.OpenVLMDefaults, "mic-boost", value)
			require.NoError(t, err)
			assert.False(t, v.MicBoost)
		})
	}

	_, err := eeprom.ApplyUpdate(eeprom.OpenVLMDefaults, "mic-boost", "maybe")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boolean")
}

// TestApplyUpdate_EnumRejectsUnknownValue exercises the BoostMode/DACOutput
// parsers' rejection paths.
func TestApplyUpdate_EnumRejectsUnknownValue(t *testing.T) {
	t.Parallel()

	_, err := eeprom.ApplyUpdate(eeprom.OpenVLMDefaults, "boost-mode", "33db")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "12db")

	_, err = eeprom.ApplyUpdate(eeprom.OpenVLMDefaults, "dac-output", "subwoofer")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "speaker")
}

// TestApplyUpdate_KnownFields_Accept covers one case per type-class so a
// regression in the parser dispatch surfaces immediately.
func TestApplyUpdate_KnownFields_Accept(t *testing.T) {
	t.Parallel()

	cases := []struct {
		field, value string
		check        func(*testing.T, eeprom.View)
	}{
		{"serial", "00001234", func(t *testing.T, v eeprom.View) {
			t.Helper()
			assert.Equal(t, "00001234", v.Serial)
		}},
		{"dac-init-volume", "-6", func(t *testing.T, v eeprom.View) {
			t.Helper()
			assert.Equal(t, -6, v.DACInitVolume)
		}},
		{"mic-boost", "false", func(t *testing.T, v eeprom.View) {
			t.Helper()
			assert.False(t, v.MicBoost)
		}},
		{"boost-mode", "22db", func(t *testing.T, v eeprom.View) {
			t.Helper()
			assert.Equal(t, eeprom.Boost22dB, v.BoostMode)
		}},
		{"dac-output", "headset", func(t *testing.T, v eeprom.View) {
			t.Helper()
			assert.Equal(t, eeprom.DACOutputHeadset, v.DACOutput)
		}},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.field, func(t *testing.T) {
			t.Parallel()

			v, err := eeprom.ApplyUpdate(eeprom.OpenVLMDefaults, tc.field, tc.value)
			require.NoError(t, err)
			tc.check(t, v)
		})
	}
}
