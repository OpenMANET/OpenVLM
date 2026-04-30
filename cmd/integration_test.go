package cmd

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/openmanet/openvlm/internal/cm108"
	"github.com/openmanet/openvlm/internal/eeprom"
	"github.com/openmanet/openvlm/internal/hidx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// withFakeBackend swaps in a fake hidx backend with one OpenVLM-strapped
// device, returns the live state handle, and registers cleanup to restore
// the real backend.
func withFakeBackend(t *testing.T) *hidx.FakeDeviceState {
	t.Helper()

	fb := hidx.NewFakeBackend()

	var tail [eeprom.WordCount - 0x33]uint16

	img := eeprom.OpenVLMDefaults.Encode(cm108.OpenVLMVendorID, cm108.OpenVLMProductID, tail)

	var initial [64]uint16
	for i := 0; i < 64; i++ {
		initial[i] = img.Word(uint8(i))
	}

	state := fb.RegisterDevice(hidx.DeviceInfo{
		Path:      "/dev/fake0",
		VendorID:  cm108.OpenVLMVendorID,
		ProductID: cm108.OpenVLMProductID,
	}, initial, true)

	prev := SetBackend(fb)

	t.Cleanup(func() { SetBackend(prev) })

	return state
}

// TestProvision_EndToEnd runs `openvlm provision --serial foo` against
// a fake backend and verifies the EEPROM bytes after the command. Serial
// is the only string field still user-overridable; product-string and
// manufacturer-string are write-locked to the compiled defaults.
func TestProvision_EndToEnd(t *testing.T) {
	state := withFakeBackend(t)

	resetOverrides()

	rootCmd.SetArgs([]string{"provision", "--serial", "00001234"})

	var (
		stdout bytes.Buffer
		stderr bytes.Buffer
	)

	rootCmd.SetOut(&stdout)
	rootCmd.SetErr(&stderr)

	require.NoError(t, rootCmd.Execute())

	got := state.EEPROM()

	view, _, err := decodeBytes(got)
	require.NoError(t, err)
	assert.Equal(t, "00001234", view.Serial,
		"the override flag must reach the device's EEPROM")
	assert.Equal(t, eeprom.OpenVLMDefaults.ProductString, view.ProductString,
		"product-string must always equal the compiled default")
	assert.Equal(t, eeprom.OpenVLMDefaults.ManufacturerString, view.ManufacturerString,
		"manufacturer-string must always equal the compiled default")

	// VID/PID must be the constants regardless of any default-tweaking.
	assert.Equal(t, cm108.OpenVLMVendorID,
		uint16(got[2])|uint16(got[3])<<8)
	assert.Equal(t, cm108.OpenVLMProductID,
		uint16(got[4])|uint16(got[5])<<8)
}

// TestUpdate_EndToEnd verifies `openvlm update <field> <value>` reads the
// live EEPROM, applies the change, and writes back. The fake's EEPROM
// state lets us assert the byte-level result.
func TestUpdate_EndToEnd(t *testing.T) {
	state := withFakeBackend(t)

	resetOverrides()

	rootCmd.SetArgs([]string{"update", "serial", "Updated01"})

	require.NoError(t, rootCmd.Execute())

	got := state.EEPROM()
	view, _, err := decodeBytes(got)
	require.NoError(t, err)
	assert.Equal(t, "Updated01", view.Serial)
}

// TestUpdate_AcceptsNegativeValue verifies that signed-int fields like
// dac-init-volume can be set to negative numbers without the `--` separator.
// pflag's default behavior treats `-10` as an unknown shorthand flag; the
// SetInterspersed(false) in updateCmd's init() makes pflag stop scanning for
// flags once the first positional arg appears.
func TestUpdate_AcceptsNegativeValue(t *testing.T) {
	state := withFakeBackend(t)

	resetOverrides()

	rootCmd.SetArgs([]string{"update", "dac-init-volume", "-10"})
	require.NoError(t, rootCmd.Execute())

	got := state.EEPROM()
	view, _, err := decodeBytes(got)
	require.NoError(t, err)
	assert.Equal(t, -10, view.DACInitVolume,
		"negative positional value must reach the field without -- separator")
}

// TestUpdate_RejectsProductString mirrors the protocol-level write-lock at
// the CLI surface for the product-string field. `openvlm update
// product-string ...` must never write anything.
func TestUpdate_RejectsProductString(t *testing.T) {
	state := withFakeBackend(t)

	resetOverrides()

	before := state.EEPROM()

	rootCmd.SetArgs([]string{"update", "product-string", "Hijacked"})
	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Equal(t, before, state.EEPROM(),
		"device must not change when update product-string is rejected")
}

// TestUpdate_RejectsVID mirrors the protocol-level VID lock at the CLI
// surface. `openvlm update vid` must never write anything.
func TestUpdate_RejectsVID(t *testing.T) {
	state := withFakeBackend(t)

	resetOverrides()

	before := state.EEPROM()

	rootCmd.SetArgs([]string{"update", "vid", "1234"})
	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Equal(t, before, state.EEPROM(),
		"device must not change when update vid is rejected")
}

// TestProvision_OverridesYAML runs the `--overrides path` flow and verifies
// the YAML field beats the compiled default while the explicit CLI flag
// beats the YAML.
func TestProvision_OverridesYAML(t *testing.T) {
	state := withFakeBackend(t)

	resetOverrides()

	dir := t.TempDir()
	yamlPath := filepath.Join(dir, "overrides.yaml")
	require.NoError(t, os.WriteFile(yamlPath, []byte("dac-init-volume: -20\nserial: from-yaml\n"), 0o600))

	// CLI dac-init-volume should win over YAML's -20; YAML's serial should
	// win over OpenVLMDefaults.
	rootCmd.SetArgs([]string{"provision", "--overrides", yamlPath, "--dac-init-volume", "-12"})

	require.NoError(t, rootCmd.Execute())

	got := state.EEPROM()
	view, _, err := decodeBytes(got)
	require.NoError(t, err)
	assert.Equal(t, -12, view.DACInitVolume, "CLI flag must beat YAML")
	assert.Equal(t, "from-yaml", view.Serial, "YAML must beat compiled default")
}

// TestWipe_HappyPath runs `openvlm wipe --yes` against a populated fake
// and confirms every byte landed at 0xFF.
func TestWipe_HappyPath(t *testing.T) {
	state := withFakeBackend(t)

	resetOverrides()
	resetWipeFlags()

	rootCmd.SetArgs([]string{"wipe", "--yes"})

	require.NoError(t, rootCmd.Execute())

	got := state.EEPROM()
	for i, b := range got {
		require.Equalf(t, byte(0xFF), b, "byte %d", i)
	}

	var img eeprom.Image
	copy(img[:], got[:])
	assert.False(t, img.IsProgrammed(),
		"post-wipe chip must look unprogrammed to subsequent commands")
}

// TestWipe_PatternZero exercises the alternate fill value.
func TestWipe_PatternZero(t *testing.T) {
	state := withFakeBackend(t)

	resetOverrides()
	resetWipeFlags()

	rootCmd.SetArgs([]string{"wipe", "--yes", "--pattern", "00"})

	require.NoError(t, rootCmd.Execute())

	got := state.EEPROM()
	for i, b := range got {
		require.Equalf(t, byte(0x00), b, "byte %d", i)
	}
}

// TestWipe_RequiresYesFlag ensures running without --yes is rejected and
// leaves the device untouched. This is the documented confirmation gate.
func TestWipe_RequiresYesFlag(t *testing.T) {
	state := withFakeBackend(t)

	resetOverrides()
	resetWipeFlags()

	before := state.EEPROM()

	rootCmd.SetArgs([]string{"wipe"})
	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "--yes")
	assert.Equal(t, before, state.EEPROM(),
		"device must not change when --yes is omitted")
}

// TestWipe_RejectsUnknownPattern enforces the FF/00-only allow-list.
func TestWipe_RejectsUnknownPattern(t *testing.T) {
	state := withFakeBackend(t)

	resetOverrides()
	resetWipeFlags()

	before := state.EEPROM()

	rootCmd.SetArgs([]string{"wipe", "--yes", "--pattern", "AA"})
	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "FF")
	assert.Contains(t, err.Error(), "00")
	assert.Equal(t, before, state.EEPROM())
}

// TestWipe_StrapLowRequiresForce confirms the GPIO1 safety gate applies
// to wipe just like every other write verb. An unstrapped device + no
// --force = exit-3 notIdentifiedError, no bytes touched.
func TestWipe_StrapLowRequiresForce(t *testing.T) {
	state := withFakeBackend(t)
	state.SetGPIO1(false)

	resetOverrides()
	resetWipeFlags()

	before := state.EEPROM()

	rootCmd.SetArgs([]string{"wipe", "--yes"})
	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Equal(t, before, state.EEPROM(),
		"device must not change when strap is low and --force is absent")
}

// TestWipe_StrapLowWithForceSucceeds is the bootstrap path: brand-new or
// post-wipe device with strap reading low, --force in effect.
func TestWipe_StrapLowWithForceSucceeds(t *testing.T) {
	state := withFakeBackend(t)
	state.SetGPIO1(false)

	resetOverrides()
	resetWipeFlags()

	rootCmd.SetArgs([]string{"wipe", "--yes", "--force"})
	require.NoError(t, rootCmd.Execute())

	got := state.EEPROM()
	for i, b := range got {
		require.Equalf(t, byte(0xFF), b, "byte %d", i)
	}
}

// TestWrite_RejectsNon128ByteRawWithoutYAMLLook ensures the input-size
// guardrail surfaces a usage-style error, not a silent corrupt write.
func TestWrite_RejectsNon128ByteRawWithoutYAMLLook(t *testing.T) {
	withFakeBackend(t)
	resetOverrides()

	dir := t.TempDir()
	bad := filepath.Join(dir, "bad.bin")
	require.NoError(t, os.WriteFile(bad, []byte{0xDE, 0xAD, 0xBE, 0xEF}, 0o600))

	rootCmd.SetArgs([]string{"write", "-i", bad})
	err := rootCmd.Execute()
	require.Error(t, err)
	assert.Contains(t, err.Error(), "expected exactly 128")
}

// decodeBytes is a small convenience that turns a 128-byte slice into a
// fully-decoded View, dropping the warnings list since these tests only
// care about the typed field values.
func decodeBytes(b [eeprom.ByteCount]byte) (eeprom.View, []string, error) {
	var img eeprom.Image

	copy(img[:], b[:])

	return img.Decode() //nolint:wrapcheck // test helper passes through verbatim
}

// runRoot executes rootCmd with capture-buffers wired in. Tests that need
// to assert on the printed text (as opposed to the returned error) use this
// instead of poking rootCmd directly.
func runRoot(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	var (
		out    bytes.Buffer
		errBuf bytes.Buffer
	)

	rootCmd.SetArgs(args)
	rootCmd.SetOut(&out)
	rootCmd.SetErr(&errBuf)

	err = rootCmd.Execute()

	return out.String(), errBuf.String(), err
}

// TestProvision_FriendlyOutput pins the success-message wording on the
// happy path. If a future tone change drifts this string, the test calls
// it out instead of letting it slip silently into a release.
func TestProvision_FriendlyOutput(t *testing.T) {
	withFakeBackend(t)
	resetOverrides()
	resetProvisionFlags()

	stdout, _, err := runRoot(t, "provision")
	require.NoError(t, err)
	assert.Contains(t, stdout, "applied the OpenVLM defaults")
}

// TestIdentify_FriendlySuccess pins the confirmed-OpenVLM line.
func TestIdentify_FriendlySuccess(t *testing.T) {
	withFakeBackend(t)
	resetOverrides()

	stdout, _, err := runRoot(t, "identify")
	require.NoError(t, err)
	assert.Contains(t, stdout, "this is an OpenVLM dongle")
}

// TestUpdate_FriendlySuccess pins the updated-field line.
func TestUpdate_FriendlySuccess(t *testing.T) {
	withFakeBackend(t)
	resetOverrides()

	stdout, _, err := runRoot(t, "update", "serial", "Friendly2")
	require.NoError(t, err)
	assert.Contains(t, stdout, "updated serial")
}

// TestWipe_FriendlySuccess pins the post-wipe line, including the
// "unplug and plug back in" instruction (which non-technical users
// need; without it they assume the wipe failed).
func TestWipe_FriendlySuccess(t *testing.T) {
	withFakeBackend(t)
	resetOverrides()
	resetWipeFlags()

	stdout, _, err := runRoot(t, "wipe", "--yes")
	require.NoError(t, err)
	assert.Contains(t, stdout, "erased")
	assert.Contains(t, stdout, "Unplug and plug back in")
}

// TestProvision_DryRunFriendlyMessage pins the dry-run preface.
func TestProvision_DryRunFriendlyMessage(t *testing.T) {
	withFakeBackend(t)
	resetOverrides()
	resetProvisionFlags()

	_, stderr, err := runRoot(t, "provision", "--dry-run")
	require.NoError(t, err)
	assert.Contains(t, stderr, "Dry-run: would write")
	assert.Contains(t, stderr, "No changes made.")
}

// TestNoDongles_FriendlyError verifies the cm108.ErrNoDevice translation
// fires end-to-end, with the action hint appended.
func TestNoDongles_FriendlyError(t *testing.T) {
	// Empty fake backend — no devices registered.
	prev := SetBackend(hidx.NewFakeBackend())

	t.Cleanup(func() { SetBackend(prev) })

	resetOverrides()
	resetWipeFlags()

	_, _, err := runRoot(t, "list")
	require.Error(t, err)

	got := friendlyError(err, false)
	assert.Contains(t, got, "No OpenVLM dongles are plugged in")
	assert.Contains(t, got, "Plug one in")
}

// TestStrapLow_FriendlyError verifies the not-OpenVLM device error reads
// as a sentence with an actionable hint.
func TestStrapLow_FriendlyError(t *testing.T) {
	state := withFakeBackend(t)
	state.SetGPIO1(false)

	resetOverrides()

	_, _, err := runRoot(t, "identify")
	require.Error(t, err)

	got := friendlyError(err, false)
	assert.Contains(t, got, "this doesn't look like an OpenVLM dongle")
}

// TestVerbose_PreservesInternalDetail confirms --verbose surfaces the
// kind of low-level wording (in this case the field name + range) that
// the default mode keeps but the friendly translator can omit prefixes
// from. This is the contract that makes verbose useful for bug reports.
func TestVerbose_PreservesInternalDetail(t *testing.T) {
	withFakeBackend(t)
	resetOverrides()

	// Out-of-range value should fail validation. The error from eeprom
	// has the "eeprom:" prefix; verbose preserves it, default strips it.
	_, _, errDefault := runRoot(t, "update", "dac-init-volume", "999")
	require.Error(t, errDefault)

	defaultMsg := friendlyError(errDefault, false)
	verboseMsg := friendlyError(errDefault, true)

	assert.Contains(t, defaultMsg, "out of range",
		"default mode should still surface the substantive error")
	assert.NotContains(t, defaultMsg, "eeprom:",
		"default mode should strip the internal package prefix")

	assert.Contains(t, verboseMsg, "eeprom:",
		"verbose mode should preserve the internal package prefix")
	assert.Contains(t, verboseMsg, "out of range")
}
