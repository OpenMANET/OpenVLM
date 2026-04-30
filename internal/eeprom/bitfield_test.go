package eeprom_test

import (
	"strings"
	"testing"

	"github.com/openmanet/openvlm/internal/cm108"
	"github.com/openmanet/openvlm/internal/eeprom"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRoundTrip_EveryNumericFieldAtBoundaries (Phase A3) is the validator+
// encoder agreement test. For every documented numeric field it visits the
// range boundaries that the validator promises to accept, encodes the View,
// decodes the resulting Image, and asserts the decoded value matches the
// input. The init-volume encoder is now attenuation-style (bits 0 =
// loudest, bench-confirmed 2026-04-30; two's complement silenced the DAC,
// plain offset-binary inverted the direction), so the full datasheet
// ranges round-trip including aa-init-volume = -23.
func TestRoundTrip_EveryNumericFieldAtBoundaries(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		set    func(*eeprom.View, int)
		get    func(eeprom.View) int
		values []int
	}{
		{
			name:   "dac-init-volume",
			set:    func(v *eeprom.View, n int) { v.DACInitVolume = n },
			get:    func(v eeprom.View) int { return v.DACInitVolume },
			values: []int{-37, -36, -10, -1, 0},
		},
		{
			name:   "adc-init-volume",
			set:    func(v *eeprom.View, n int) { v.ADCInitVolume = n },
			get:    func(v eeprom.View) int { return v.ADCInitVolume },
			values: []int{-12, -11, 0, 22, 23},
		},
		{
			name:   "aa-init-volume",
			set:    func(v *eeprom.View, n int) { v.AAInitVolume = n },
			get:    func(v eeprom.View) int { return v.AAInitVolume },
			values: []int{-23, -22, -16, -7, 0, 7, 8},
		},
		{
			name:   "dac-min-volume",
			set:    func(v *eeprom.View, n int) { v.DACMinVolume = n; v.DACMaxVolume = 0; v.DACInitVolume = 0 },
			get:    func(v eeprom.View) int { return v.DACMinVolume },
			values: []int{-128, -127, -50, -1, 0},
		},
		{
			name:   "adc-max-volume",
			set:    func(v *eeprom.View, n int) { v.ADCMaxVolume = n; v.ADCMinVolume = -1; v.ADCInitVolume = -1 },
			get:    func(v eeprom.View) int { return v.ADCMaxVolume },
			values: []int{0, 1, 50, 126, 127},
		},
	}

	for _, tc := range cases {
		tc := tc

		for _, value := range tc.values {
			value := value
			t.Run(tc.name+"="+itoa(value), func(t *testing.T) {
				t.Parallel()

				v := eeprom.OpenVLMDefaults
				tc.set(&v, value)

				require.NoErrorf(t, v.Validate(),
					"validator must accept %s=%d", tc.name, value)

				var tail [eeprom.WordCount - 0x33]uint16

				img := v.Encode(cm108.OpenVLMVendorID, cm108.OpenVLMProductID, tail)

				decoded, _, err := img.Decode()
				require.NoError(t, err)
				assert.Equal(t, value, tc.get(decoded),
					"%s round-trip: encoder + decoder must agree on every accepted value",
					tc.name)
			})
		}
	}
}

// TestEncode_ReservedBitsAreZero (Phase A4) pins the datasheet's
// reserved-must-be-zero invariants on every encoded image. Today these are
// only enforced as decode-time *warnings*; this test makes them an
// encode-time guarantee, so a future contributor can't introduce a stray
// bit-set without the build catching it.
func TestEncode_ReservedBitsAreZero(t *testing.T) {
	t.Parallel()

	var tail [eeprom.WordCount - 0x33]uint16

	img := eeprom.OpenVLMDefaults.Encode(cm108.OpenVLMVendorID, cm108.OpenVLMProductID, tail)

	// Word 0x00: reserved-must-be-1 bits at positions 0 and 2.
	w0 := img.Word(0x00)
	assert.Equal(t, uint16(1), w0&0x01,
		"word 0x00 bit[0] must be 1 (datasheet 'reserved, should be 1')")
	assert.Equal(t, uint16(0x4), w0&0x04,
		"word 0x00 bit[2] must be 1 (datasheet 'reserved, should be 1')")

	// Word 0x2B: reserved-must-be-0 bits at positions 6 and 10.
	w2B := img.Word(0x2B)
	assert.Equal(t, uint16(0), w2B&(1<<10),
		"word 0x2B bit[10] must be 0 (datasheet 'reserved, should be 0')")
	assert.Equal(t, uint16(0), w2B&(1<<6),
		"word 0x2B bit[6] must be 0 (datasheet 'reserved, should be 0')")

	// Word 0x32 (EE_OPTION2): all bits reserved-must-be-0 in §7.1.3.
	assert.Equal(t, uint16(0), img.Word(0x32),
		"word 0x32 (EE_OPTION2) must be all-zero")

	// Min/max volume words 0x2C..0x31: low byte must be 0 per datasheet
	// examples ('0xEA00', '0x1700', etc.).
	for addr := uint8(0x2C); addr <= 0x31; addr++ {
		w := img.Word(addr)
		assert.Equal(t, uint16(0), w&0xFF,
			"word 0x%02X low byte must be 0 (datasheet pattern '0xXX00')", addr)
	}
}

// TestEncode_InitVolumeBitPatterns pins the exact bit placement for DAC,
// ADC, and AA init-volume fields. The chip uses attenuation encoding
// (bits = maxDB - value, bits 0 = loudest), bench-confirmed 2026-04-30:
// two's-complement silenced the DAC, plain offset-binary produced very
// soft audio because direction was inverted. Round-trip tests alone are
// tautological (encoder + decoder agree on the same wrong convention);
// these assertions are the load-bearing gate that pins the convention.
func TestEncode_InitVolumeBitPatterns(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name      string
		set       func(*eeprom.View)
		wordAddr  uint8
		shift     uint
		widthMask uint16
		wantBits  uint16
	}{
		{"dac-init 0 dB → 0 (loudest)", func(v *eeprom.View) { v.DACInitVolume = 0 }, 0x2A, 9, 0x7F, 0},
		{"dac-init -10 dB → 10", func(v *eeprom.View) { v.DACInitVolume = -10 }, 0x2A, 9, 0x7F, 10},
		{"dac-init -37 dB → 37 (quietest)", func(v *eeprom.View) { v.DACInitVolume = -37 }, 0x2A, 9, 0x7F, 37},
		{"adc-init +23 dB → 0 (loudest)", func(v *eeprom.View) { v.ADCInitVolume = 23 }, 0x2A, 3, 0x3F, 0},
		{"adc-init +8 dB → 15", func(v *eeprom.View) { v.ADCInitVolume = 8 }, 0x2A, 3, 0x3F, 15},
		{"adc-init -12 dB → 35 (quietest)", func(v *eeprom.View) { v.ADCInitVolume = -12 }, 0x2A, 3, 0x3F, 35},
		{"aa-init +8 dB → 0 (loudest)", func(v *eeprom.View) { v.AAInitVolume = 8 }, 0x2B, 11, 0x1F, 0},
		{"aa-init -7 dB → 15", func(v *eeprom.View) { v.AAInitVolume = -7 }, 0x2B, 11, 0x1F, 15},
		{"aa-init -23 dB → 31 (quietest)", func(v *eeprom.View) { v.AAInitVolume = -23 }, 0x2B, 11, 0x1F, 31},
	}

	for _, tc := range cases {
		tc := tc

		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			v := eeprom.OpenVLMDefaults
			tc.set(&v)
			require.NoError(t, v.Validate())

			var tail [eeprom.WordCount - 0x33]uint16

			img := v.Encode(cm108.OpenVLMVendorID, cm108.OpenVLMProductID, tail)
			got := (img.Word(tc.wordAddr) >> tc.shift) & tc.widthMask
			assert.Equal(t, tc.wantBits, got)
		})
	}
}

// TestEncode_StringBoundaries (Phase A5) covers the string descriptor
// edge cases. The bLength byte stored in the header word's low byte must
// equal 2 + 2*char_count for any non-empty string; an empty string sets
// the entire header word to zero so the decoder treats it as 'unset'.
func TestEncode_StringBoundaries(t *testing.T) {
	t.Parallel()

	t.Run("empty product string → zero header", func(t *testing.T) {
		t.Parallel()

		v := eeprom.OpenVLMDefaults
		v.ProductString = ""
		require.NoError(t, v.Validate())

		var tail [eeprom.WordCount - 0x33]uint16

		img := v.Encode(cm108.OpenVLMVendorID, cm108.OpenVLMProductID, tail)
		assert.Equal(t, uint16(0), img.Word(0x0A),
			"empty product string must zero word 0x0A")
	})

	t.Run("30-char product → bLength 0x3E (datasheet hint)", func(t *testing.T) {
		t.Parallel()

		v := eeprom.OpenVLMDefaults
		v.ProductString = strings.Repeat("a", 30)
		require.NoError(t, v.Validate())

		var tail [eeprom.WordCount - 0x33]uint16

		img := v.Encode(cm108.OpenVLMVendorID, cm108.OpenVLMProductID, tail)
		assert.Equal(t, byte(0x3E), img[0x14],
			"30-char string header low byte must equal 0x3E per datasheet §7.1.3 hint")
	})

	t.Run("31-char product rejected by validator", func(t *testing.T) {
		t.Parallel()

		v := eeprom.OpenVLMDefaults
		v.ProductString = strings.Repeat("a", 31)
		err := v.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "product-string")
	})

	t.Run("non-printable serial rejected", func(t *testing.T) {
		t.Parallel()

		v := eeprom.OpenVLMDefaults
		v.Serial = "abc\x00def"
		err := v.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "not printable ASCII")
	})

	t.Run("12-byte serial accepted, 13-byte rejected", func(t *testing.T) {
		t.Parallel()

		v := eeprom.OpenVLMDefaults
		v.Serial = strings.Repeat("S", 12)
		require.NoError(t, v.Validate())

		v.Serial = strings.Repeat("S", 13)
		err := v.Validate()
		require.Error(t, err)
		assert.Contains(t, err.Error(), "serial")
	})
}

// TestEncode_TailPreservation (Phase A6) confirms the user-supplied tail
// bytes (words 0x33..0x3F) flow through Encode unchanged. update.go relies
// on this so a read-modify-write through the CLI never clobbers undocumented
// factory data.
func TestEncode_TailPreservation(t *testing.T) {
	t.Parallel()

	var tail [eeprom.WordCount - 0x33]uint16
	for i := range tail {
		tail[i] = uint16(0xCAFE) ^ uint16(i)
	}

	img := eeprom.OpenVLMDefaults.Encode(cm108.OpenVLMVendorID, cm108.OpenVLMProductID, tail)

	for i, want := range tail {
		assert.Equalf(t, want, img.Word(uint8(0x33+i)),
			"tail word 0x%02X must be preserved verbatim", 0x33+i)
	}
}

// TestImage_WordSetWordBoundary covers the deliberate out-of-range silent
// fail in Image.Word / Image.SetWord — accessing addr 64 (one past the end)
// must return 0 and not panic.
func TestImage_WordSetWordBoundary(t *testing.T) {
	t.Parallel()

	var img eeprom.Image
	img.SetWord(eeprom.WordCount, 0xFFFF)
	assert.Equal(t, uint16(0), img.Word(eeprom.WordCount),
		"out-of-range Word/SetWord must be a silent no-op (current contract)")
}

// itoa is a tiny stdlib-free int-to-string used by the t.Run names so
// negative values render as the test reader expects.
func itoa(n int) string {
	if n == 0 {
		return "0"
	}

	neg := n < 0
	if neg {
		n = -n
	}

	var buf [12]byte

	i := len(buf)

	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}

	if neg {
		i--
		buf[i] = '-'
	}

	return string(buf[i:])
}
