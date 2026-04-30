package cmd

import (
	"strings"
	"testing"

	"github.com/openmanet/openvlm/internal/eeprom"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests in this file mutate the package-level `partialOverrides` global
// (which is unavoidable: Cobra's flag layer requires a stable destination
// pointer). Per-test t.Parallel() would race on it; the tests are cheap
// (microseconds each) so they all run serially.

// TestRegisterOverrideFlags_HasNoVIDPID guards the documented invariant
// that no `--vid` or `--pid` flag exists. If either ever shows up the user
// gets a write surface for VID/PID — which is exactly what the lock policy
// is meant to prevent.
func TestRegisterOverrideFlags_HasNoVIDPID(t *testing.T) {
	c := &cobra.Command{Use: "x"}
	registerOverrideFlags(c)

	assert.Nil(t, c.Flag("vid"), "--vid must not exist")
	assert.Nil(t, c.Flag("pid"), "--pid must not exist")
}

// TestRegisterOverrideFlags_CoversAllFields asserts a flag exists for every
// PartialView-eligible field. If a future contributor adds a field to
// PartialView and forgets to register a flag, this test fails.
func TestRegisterOverrideFlags_CoversAllFields(t *testing.T) {
	c := &cobra.Command{Use: "x"}
	registerOverrideFlags(c)

	for _, f := range eeprom.AllFields() {
		assert.NotNil(t, c.Flag(string(f)),
			"missing CLI flag for %q", f)
	}
}

// TestNoHexInt_RejectsHexBinaryOctal exercises the no-hex-input rule at the
// flag layer.
func TestNoHexInt_RejectsHexBinaryOctal(t *testing.T) {
	bad := []string{"0x10", "0X10", "0b1010", "0B1010", "0o7", "0O7", "0xff"}

	for _, value := range bad {
		t.Run(value, func(t *testing.T) {
			resetOverrides()

			c := &cobra.Command{Use: "x"}
			registerOverrideFlags(c)
			c.SetArgs([]string{"--dac-init-volume", value})

			err := c.Execute()
			require.Error(t, err)
			assert.True(t,
				strings.Contains(err.Error(), "hex") ||
					strings.Contains(err.Error(), "non-decimal"),
				"want hex/non-decimal error, got %q", err.Error())
		})
	}
}

// TestNoHexInt_AcceptsDecimal confirms decimal values land in the partial.
func TestNoHexInt_AcceptsDecimal(t *testing.T) {
	resetOverrides()

	c := &cobra.Command{Use: "x", Run: func(*cobra.Command, []string) {}}
	registerOverrideFlags(c)
	c.SetArgs([]string{"--dac-init-volume", "-6"})

	require.NoError(t, c.Execute())
	require.NotNil(t, partialOverrides.DACInitVolume)
	assert.Equal(t, -6, *partialOverrides.DACInitVolume)
}

// TestBoolPair_NoFormFlipsDefault confirms `--no-mic-boost` flows into a
// false PartialView entry without consuming the next argument.
func TestBoolPair_NoFormFlipsDefault(t *testing.T) {
	resetOverrides()

	c := &cobra.Command{Use: "x", Run: func(*cobra.Command, []string) {}}
	registerOverrideFlags(c)
	c.SetArgs([]string{"--no-mic-boost", "--dac-init-volume", "-6"})

	require.NoError(t, c.Execute())
	require.NotNil(t, partialOverrides.MicBoost)
	assert.False(t, *partialOverrides.MicBoost)
	require.NotNil(t, partialOverrides.DACInitVolume)
	assert.Equal(t, -6, *partialOverrides.DACInitVolume)
}

// TestBoolPair_PositiveForm: --mic-boost with no value sets to true.
func TestBoolPair_PositiveForm(t *testing.T) {
	resetOverrides()

	c := &cobra.Command{Use: "x", Run: func(*cobra.Command, []string) {}}
	registerOverrideFlags(c)
	c.SetArgs([]string{"--mic-boost"})

	require.NoError(t, c.Execute())
	require.NotNil(t, partialOverrides.MicBoost)
	assert.True(t, *partialOverrides.MicBoost)
}

// TestEnumFlags_InvalidValue: --boost-mode foo is rejected with the allowed
// values listed.
func TestEnumFlags_InvalidValue(t *testing.T) {
	resetOverrides()

	c := &cobra.Command{Use: "x"}
	registerOverrideFlags(c)
	c.SetArgs([]string{"--boost-mode", "33db"})

	err := c.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "12db")
	assert.Contains(t, err.Error(), "22db")
}
