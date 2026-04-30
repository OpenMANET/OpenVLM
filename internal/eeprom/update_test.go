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
