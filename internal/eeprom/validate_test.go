package eeprom_test

import (
	"strings"
	"testing"

	"github.com/openmanet/openvlm/internal/eeprom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestValidate_DefaultsAreValid is the canary: if anyone tightens the
// validator without updating OpenVLMDefaults, this test catches it. The
// shipped defaults must always pass.
func TestValidate_DefaultsAreValid(t *testing.T) {
	t.Parallel()

	v := eeprom.OpenVLMDefaults
	require.NoError(t, v.Validate())
}

// TestValidate_RangeChecks runs through every numeric field and confirms
// the documented [low, high] bounds are enforced.
func TestValidate_RangeChecks(t *testing.T) {
	t.Parallel()

	type tweak func(*eeprom.View)

	cases := []struct {
		name      string
		mut       tweak
		field     eeprom.Field
		wantError bool
	}{
		// dac-init-volume: -37..0
		{"dac-init lower bound ok", func(v *eeprom.View) { v.DACInitVolume = -37 }, eeprom.FieldDACInitVolume, false},
		{"dac-init upper bound ok", func(v *eeprom.View) { v.DACInitVolume = 0 }, eeprom.FieldDACInitVolume, false},
		{"dac-init below range", func(v *eeprom.View) { v.DACInitVolume = -38 }, eeprom.FieldDACInitVolume, true},
		{"dac-init above range", func(v *eeprom.View) { v.DACInitVolume = 1 }, eeprom.FieldDACInitVolume, true},

		// adc-init-volume: -12..23
		{"adc-init lower ok", func(v *eeprom.View) { v.ADCInitVolume = -12 }, eeprom.FieldADCInitVolume, false},
		{"adc-init upper ok", func(v *eeprom.View) { v.ADCInitVolume = 23 }, eeprom.FieldADCInitVolume, false},
		{"adc-init below", func(v *eeprom.View) { v.ADCInitVolume = -13 }, eeprom.FieldADCInitVolume, true},
		{"adc-init above", func(v *eeprom.View) { v.ADCInitVolume = 24 }, eeprom.FieldADCInitVolume, true},

		// aa-init-volume: full datasheet range -23..+8, encoded offset-binary.
		{"aa-init lower ok", func(v *eeprom.View) { v.AAInitVolume = -23 }, eeprom.FieldAAInitVolume, false},
		{"aa-init upper ok", func(v *eeprom.View) { v.AAInitVolume = 8 }, eeprom.FieldAAInitVolume, false},
		{"aa-init below", func(v *eeprom.View) { v.AAInitVolume = -24 }, eeprom.FieldAAInitVolume, true},
		{"aa-init above", func(v *eeprom.View) { v.AAInitVolume = 9 }, eeprom.FieldAAInitVolume, true},

		// min/max overrides: -128..127
		{"dac-min ok", func(v *eeprom.View) { v.DACMinVolume = -128 }, eeprom.FieldDACMinVolume, false},
		{"dac-min below", func(v *eeprom.View) { v.DACMinVolume = -129 }, eeprom.FieldDACMinVolume, true},
		{"dac-max above", func(v *eeprom.View) { v.DACMaxVolume = 128 }, eeprom.FieldDACMaxVolume, true},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			v := eeprom.OpenVLMDefaults
			tc.mut(&v)
			err := v.Validate()

			if tc.wantError {
				require.Error(t, err)
				assert.Contains(t, err.Error(), string(tc.field),
					"error must name the offending field")
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestValidate_CrossFieldChecks covers the constraints that aren't a
// simple per-field range: min ≤ max and init within [min, max].
func TestValidate_CrossFieldChecks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		mut  func(*eeprom.View)
		want string
	}{
		{
			name: "dac-min > dac-max",
			mut:  func(v *eeprom.View) { v.DACMinVolume = 5; v.DACMaxVolume = 0 },
			want: "dac-min-volume",
		},
		{
			name: "init outside range",
			mut: func(v *eeprom.View) {
				v.DACMinVolume = -20
				v.DACMaxVolume = -15
				v.DACInitVolume = -10
			},
			want: "dac-init-volume",
		},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			v := eeprom.OpenVLMDefaults
			tc.mut(&v)

			err := v.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.want)
		})
	}
}

// TestValidate_StringChecks covers the printable-ASCII + length-cap rules.
func TestValidate_StringChecks(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		mut       func(*eeprom.View)
		errSubstr string
	}{
		{
			name:      "product-string too long",
			mut:       func(v *eeprom.View) { v.ProductString = strings.Repeat("a", 31) },
			errSubstr: "product-string",
		},
		{
			name:      "manufacturer-string too long",
			mut:       func(v *eeprom.View) { v.ManufacturerString = strings.Repeat("a", 31) },
			errSubstr: "manufacturer-string",
		},
		{
			name:      "serial too long",
			mut:       func(v *eeprom.View) { v.Serial = strings.Repeat("a", 13) },
			errSubstr: "serial",
		},
		{
			name:      "product-string non-ascii",
			mut:       func(v *eeprom.View) { v.ProductString = "café" },
			errSubstr: "not printable ASCII",
		},
		{
			name:      "product-string control char",
			mut:       func(v *eeprom.View) { v.ProductString = "x\x00y" },
			errSubstr: "not printable ASCII",
		},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			v := eeprom.OpenVLMDefaults
			tc.mut(&v)

			err := v.Validate()
			require.Error(t, err)
			assert.Contains(t, err.Error(), tc.errSubstr)
		})
	}
}

// TestValidate_EnumChecks ensures both enum fields reject unknown values
// and that the error message lists the allowed values.
func TestValidate_EnumChecks(t *testing.T) {
	t.Parallel()

	v := eeprom.OpenVLMDefaults
	v.BoostMode = "33db"

	err := v.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "boost-mode")
	assert.Contains(t, err.Error(), "12db")
	assert.Contains(t, err.Error(), "22db")

	v = eeprom.OpenVLMDefaults
	v.DACOutput = "subwoofer"

	err = v.Validate()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dac-output")
	assert.Contains(t, err.Error(), "speaker")
	assert.Contains(t, err.Error(), "headset")
}
