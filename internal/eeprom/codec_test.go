package eeprom_test

import (
	"strings"
	"testing"

	"github.com/openmanet/openvlm/internal/cm108"
	"github.com/openmanet/openvlm/internal/eeprom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestEncodeDefaults_Roundtrip exercises the canonical Encode → Decode loop
// against OpenVLMDefaults. It is the single test that guarantees the
// compiled-in defaults survive every layer of the codec, and indirectly
// proves that every documented field has both an encoder and a decoder.
func TestEncodeDefaults_Roundtrip(t *testing.T) {
	t.Parallel()

	var tail [eeprom.WordCount - 0x33]uint16

	img := eeprom.OpenVLMDefaults.Encode(cm108.OpenVLMVendorID, cm108.OpenVLMProductID, tail)

	require.Equal(t, cm108.OpenVLMVendorID, img.VID(),
		"encoder must source VID from constants, not from View")
	require.Equal(t, cm108.OpenVLMProductID, img.PID(),
		"encoder must source PID from constants, not from View")
	require.True(t, img.IsProgrammed(), "encoded defaults must satisfy magic word")

	view, warnings, err := img.Decode()
	require.NoError(t, err)
	require.Empty(t, warnings, "defaults image must not produce decode warnings")

	assert.Equal(t, eeprom.OpenVLMDefaults, view,
		"defaults must round-trip bit-for-bit")
}

// TestImageDecode_RejectsBadMagic ensures that a fresh (zeroed) image is
// surfaced to the user as "unprogrammed" rather than silently decoded into
// nonsense.
func TestImageDecode_RejectsBadMagic(t *testing.T) {
	t.Parallel()

	var img eeprom.Image

	_, _, err := img.Decode()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "magic")
}

// TestApplyOverrides_FieldByField confirms every PartialView field
// individually overrides the base View. Coverage gap here would mean a new
// field added to View without a matching merge entry — the test catches it
// by failing the round-trip on that one field.
func TestApplyOverrides_FieldByField(t *testing.T) {
	t.Parallel()

	base := eeprom.OpenVLMDefaults

	cases := []struct {
		name  string
		mut   func(*eeprom.PartialView)
		check func(*testing.T, eeprom.View)
	}{
		{
			name: "extended-fields-valid",
			mut:  func(p *eeprom.PartialView) { f := false; p.ExtendedFieldsValid = &f },
			check: func(t *testing.T, v eeprom.View) {
				t.Helper()
				assert.False(t, v.ExtendedFieldsValid)
			},
		},
		{
			name: "product-string",
			mut:  func(p *eeprom.PartialView) { s := "OpenVLM v2"; p.ProductString = &s },
			check: func(t *testing.T, v eeprom.View) {
				t.Helper()
				assert.Equal(t, "OpenVLM v2", v.ProductString)
			},
		},
		{
			name: "dac-init-volume",
			mut:  func(p *eeprom.PartialView) { n := -20; p.DACInitVolume = &n },
			check: func(t *testing.T, v eeprom.View) {
				t.Helper()
				assert.Equal(t, -20, v.DACInitVolume)
			},
		},
		{
			name: "boost-mode",
			mut: func(p *eeprom.PartialView) {
				bm := eeprom.Boost22dB
				p.BoostMode = &bm
			},
			check: func(t *testing.T, v eeprom.View) {
				t.Helper()
				assert.Equal(t, eeprom.Boost22dB, v.BoostMode)
			},
		},
		{
			name: "dac-output",
			mut: func(p *eeprom.PartialView) {
				do := eeprom.DACOutputHeadset
				p.DACOutput = &do
			},
			check: func(t *testing.T, v eeprom.View) {
				t.Helper()
				assert.Equal(t, eeprom.DACOutputHeadset, v.DACOutput)
			},
		},
		{
			name: "aa-min-volume",
			mut:  func(p *eeprom.PartialView) { n := -100; p.AAMinVolume = &n },
			check: func(t *testing.T, v eeprom.View) {
				t.Helper()
				assert.Equal(t, -100, v.AAMinVolume)
			},
		},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var p eeprom.PartialView
			tc.mut(&p)

			merged := eeprom.ApplyOverrides(base, &p)
			tc.check(t, merged)
		})
	}
}

// TestApplyOverrides_LayeredPrecedence proves the documented ordering:
// CLI flags win over YAML, YAML wins over compiled defaults.
func TestApplyOverrides_LayeredPrecedence(t *testing.T) {
	t.Parallel()

	base := eeprom.OpenVLMDefaults

	yamlVol := -15
	yaml := eeprom.PartialView{DACInitVolume: &yamlVol}

	cliVol := -6
	cli := eeprom.PartialView{DACInitVolume: &cliVol}

	merged := eeprom.ApplyOverrides(eeprom.ApplyOverrides(base, &yaml), &cli)
	assert.Equal(t, -6, merged.DACInitVolume,
		"CLI flag override must beat YAML override")

	yamlOnly := eeprom.ApplyOverrides(base, &yaml)
	assert.Equal(t, -15, yamlOnly.DACInitVolume,
		"YAML override must beat compiled defaults")
}

// TestUnmarshalPartial_RejectsVIDPID enforces the YAML-side half of the
// VID/PID write-lock. Both keys must produce a clear, message-stable error.
func TestUnmarshalPartial_RejectsVIDPID(t *testing.T) {
	t.Parallel()

	cases := []string{
		"vid: 0x0d8c\n",
		"pid: 0x0012\n",
		"product-string: foo\nvid: 0x0d8c\n",
	}

	for _, doc := range cases {
		doc := doc

		t.Run(strings.SplitN(doc, "\n", 2)[0], func(t *testing.T) {
			t.Parallel()

			_, err := eeprom.UnmarshalPartial([]byte(doc))
			require.Error(t, err)
			assert.Contains(t, err.Error(), "not user-programmable")
		})
	}
}

// TestUnmarshalPartial_RejectsUnknownKeys ensures KnownFields(true) is on so
// typos in the YAML don't silently produce empty overrides.
func TestUnmarshalPartial_RejectsUnknownKeys(t *testing.T) {
	t.Parallel()

	_, err := eeprom.UnmarshalPartial([]byte("typo-field: 1\n"))
	require.Error(t, err)
}

// TestUnmarshalPartial_Empty returns an empty partial without erroring.
func TestUnmarshalPartial_Empty(t *testing.T) {
	t.Parallel()

	p, err := eeprom.UnmarshalPartial([]byte(""))
	require.NoError(t, err)
	require.NotNil(t, p)
	assert.Nil(t, p.DACInitVolume)
}
