package cmd

import (
	"errors"
	"fmt"
	"testing"

	"github.com/openmanet/openvlm/internal/cm108"
	"github.com/openmanet/openvlm/internal/eeprom"
	"github.com/stretchr/testify/assert"
)

// Tests in this file pin the user-facing wording in messages.go. The CLI is
// a hardware-programming tool; we lean on these tests so a tone-shifting
// edit ("friendly" → "stern", or vice versa) shows up as a deliberate diff
// rather than a quiet drift.

func TestDisplayName(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name         string
		serial, path string
		want         string
	}{
		{name: "serial preferred", serial: "ABC123", path: "/dev/hidraw3", want: "ABC123"},
		{name: "friendly fallback when serial empty", serial: "", path: "/dev/hidraw3", want: "OpenVLM device"},
		{name: "friendly fallback when both empty", serial: "", path: "", want: "OpenVLM device"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, displayName(tc.serial, tc.path))
		})
	}
}

func TestSuccessMessages(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		got  string
		want string
	}{
		{
			name: "msgIdentified",
			got:  msgIdentified("ABC123"),
			want: "ABC123: confirmed — this is an OpenVLM device.",
		},
		{
			name: "msgWritten",
			got:  msgWritten("ABC123", 128),
			want: "ABC123: wrote and verified 128 bytes.",
		},
		{
			name: "msgUpdated",
			got:  msgUpdated("ABC123", "serial"),
			want: "ABC123: updated serial.",
		},
		{
			name: "msgProvisioned",
			got:  msgProvisioned("ABC123"),
			want: "ABC123: applied the OpenVLM defaults.",
		},
		{
			name: "msgWiped",
			got:  msgWiped("ABC123"),
			want: "ABC123: erased. Unplug and plug back in for the change to take effect.",
		},
		{
			name: "msgDryRun",
			got:  msgDryRun(128),
			want: "Dry-run: would write 128 bytes. No changes made.",
		},
		{
			name: "msgReadComplete",
			got:  msgReadComplete("ABC123", 128, "backup.bin"),
			want: "ABC123: saved 128 bytes to backup.bin.",
		},
		{
			name: "msgNoDevices",
			got:  msgNoDevices(),
			want: "No OpenVLM devices are plugged in.",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, tc.got)
		})
	}
}

func TestForceWarning(t *testing.T) {
	t.Parallel()
	assert.Equal(t,
		"Warning: device's identity bit isn't set; continuing because --force was passed.",
		msgForceWarning())
}

// TestPowerShellStdoutGuardMessage pins the wording of the
// `openvlm read` PowerShell-stdout refusal so a future tone-tweak shows up
// as a deliberate diff. The body must include the three remedies (-o, cmd
// /c, --force-stdout) so the user is never left without an escape hatch.
func TestPowerShellStdoutGuardMessage(t *testing.T) {
	t.Parallel()

	msg := errPowerShellStdoutGuard().Error()

	assert.Contains(t, msg, "PowerShell")
	assert.Contains(t, msg, "UTF-16")
	assert.Contains(t, msg, "-o backup.bin")
	assert.Contains(t, msg, "cmd /c")
	assert.Contains(t, msg, "--force-stdout")
}

func TestChipBlank(t *testing.T) {
	t.Parallel()

	assert.Equal(t,
		"Warning: this device's configuration looks blank or corrupted.",
		msgChipBlank(false, 0x0D8C, 0x0012))

	assert.Equal(t,
		"Warning: this device's configuration looks blank or corrupted. (read VID:PID 0x0D8C:0x0012)",
		msgChipBlank(true, 0x0D8C, 0x0012))
}

func TestFriendlyError_NilReturnsEmpty(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "", friendlyError(nil, false))
}

func TestFriendlyError_KnownSentinels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want string
	}{
		{
			name: "no devices",
			err:  cm108.ErrNoDevice,
			want: "No OpenVLM devices are plugged in.\nPlug one in and try again.",
		},
		{
			name: "serial not found, wrapped",
			err:  fmt.Errorf("%w: %q", cm108.ErrSerialNotFound, "ABC"),
			want: "No device has the serial number you asked for.\n" +
				"Run 'openvlm list' to see what's connected.",
		},
		{
			name: "ambiguous device",
			err:  fmt.Errorf("%w: 2 OpenVLM-strapped devices", cm108.ErrAmbiguousDevice),
			want: "More than one device is plugged in.\n" +
				"Use --serial <serial> to pick which one. Run 'openvlm list' to see them.",
		},
		{
			name: "no strapped device",
			err:  fmt.Errorf("%w: 2 CM108 devices, none strapped", cm108.ErrNoOpenVLMStrapped),
			want: "Devices are plugged in, but none of them are confirmed as OpenVLM.\n" +
				"Use --force to program one anyway, or --serial <serial> to pick a specific device.",
		},
		{
			name: "field locked",
			err:  fmt.Errorf("%w: %q", eeprom.ErrFieldLocked, "vid"),
			want: "That field is part of the device's identity and can't be changed by this tool.",
		},
		{
			name: "hex input",
			err:  fmt.Errorf("%w: %s: %q", eeprom.ErrHexInput, "dac-init-volume", "0x10"),
			want: "Numbers must be in regular decimal form. (No 0x, 0b, or 0o prefixes.)",
		},
		{
			name: "verify mismatch sentinel only, default",
			err:  eeprom.ErrVerifyMismatch,
			want: "The device accepted the write but read back a different value.\n" +
				"Unplug it, plug it back in, and try the command once more.",
		},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, friendlyError(tc.err, false))
		})
	}
}

func TestFriendlyError_VerifyMismatchVerbose(t *testing.T) {
	t.Parallel()

	ve := &eeprom.VerifyError{Addr: 0x07, Wanted: 0xCAFE, GotValue: 0xBABE}

	got := friendlyError(ve, true)
	assert.Contains(t, got,
		"The device accepted the write but read back a different value.")
	assert.Contains(t, got, "word 0x07")
	assert.Contains(t, got, "wrote 0xCAFE")
	assert.Contains(t, got, "read 0xBABE")

	// Default mode hides the address detail.
	gotDefault := friendlyError(ve, false)
	assert.NotContains(t, gotDefault, "0x07")
	assert.NotContains(t, gotDefault, "0xCAFE")
}

func TestFriendlyError_UnknownFieldListsKnownFields(t *testing.T) {
	t.Parallel()

	err := fmt.Errorf("%w: %q (known fields: %s)", eeprom.ErrFieldUnknown, "nope", eeprom.FieldList())

	got := friendlyError(err, false)
	assert.Contains(t, got, "Unknown field")
	assert.Contains(t, got, `"nope"`)
	assert.Contains(t, got, "serial")
	assert.Contains(t, got, "dac-init-volume")
}

func TestFriendlyError_UnknownErrorStripsPrefixes(t *testing.T) {
	t.Parallel()

	// Single-line, single prefix.
	got := friendlyError(errors.New("eeprom: dac-init-volume: value 99 out of range [-37, 0]"), false)
	assert.Equal(t, "dac-init-volume: value 99 out of range [-37, 0]", got)

	// Multi-line errors.Join — every line independently prefixed.
	multi := errors.Join(
		errors.New("eeprom: dac-init-volume: out of range"),
		errors.New("eeprom: serial: too long"),
	)
	gotMulti := friendlyError(multi, false)
	assert.Equal(t,
		"dac-init-volume: out of range\nserial: too long",
		gotMulti)

	// Stacked prefixes.
	stacked := errors.New("openvlm: eeprom: foo: bar")
	assert.Equal(t, "foo: bar", friendlyError(stacked, false))
}

func TestFriendlyError_VerboseFallsThrough(t *testing.T) {
	t.Parallel()

	got := friendlyError(errors.New("eeprom: low-level detail"), true)
	assert.Equal(t, "eeprom: low-level detail", got,
		"verbose mode must preserve internal prefixes for debugging")
}

func TestStripInternalPrefixes_NoMatchIsIdentity(t *testing.T) {
	t.Parallel()
	assert.Equal(t, "plain message", stripInternalPrefixes("plain message"))
}

func TestCapitalize(t *testing.T) {
	t.Parallel()

	cases := []struct {
		in, want string
	}{
		{"", ""},
		{"already Capital", "Already Capital"},
		{"lower", "Lower"},
		{"123 numeric", "123 numeric"},
	}

	for _, tc := range cases {
		tc := tc
		t.Run(tc.in, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tc.want, capitalize(tc.in))
		})
	}
}
