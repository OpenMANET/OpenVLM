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

// TestProvision_EndToEnd runs `openvlm provision --product-string foo` against
// a fake backend and verifies the EEPROM bytes after the command.
func TestProvision_EndToEnd(t *testing.T) {
	state := withFakeBackend(t)

	resetOverrides()

	rootCmd.SetArgs([]string{"provision", "--product-string", "OpenVLM v9"})

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
	assert.Equal(t, "OpenVLM v9", view.ProductString,
		"the override flag must reach the device's EEPROM")

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

	rootCmd.SetArgs([]string{"update", "product-string", "Updated"})

	require.NoError(t, rootCmd.Execute())

	got := state.EEPROM()
	view, _, err := decodeBytes(got)
	require.NoError(t, err)
	assert.Equal(t, "Updated", view.ProductString)
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
	require.NoError(t, os.WriteFile(yamlPath, []byte("dac-init-volume: -20\nproduct-string: from-yaml\n"), 0o600))

	// CLI dac-init-volume should win over YAML's -20; YAML's product-string
	// should win over OpenVLMDefaults.
	rootCmd.SetArgs([]string{"provision", "--overrides", yamlPath, "--dac-init-volume", "-12"})

	require.NoError(t, rootCmd.Execute())

	got := state.EEPROM()
	view, _, err := decodeBytes(got)
	require.NoError(t, err)
	assert.Equal(t, -12, view.DACInitVolume, "CLI flag must beat YAML")
	assert.Equal(t, "from-yaml", view.ProductString, "YAML must beat compiled default")
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
