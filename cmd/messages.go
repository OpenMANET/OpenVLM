package cmd

import (
	"errors"
	"fmt"
	"strings"

	"github.com/openmanet/openvlm/internal/cm108"
	"github.com/openmanet/openvlm/internal/eeprom"
)

// All user-facing wording lives in this file. The plan in
// .claude/plans/human-friendly-cli-output.md explains why; the short version
// is "one place to tweak tone, one place to pin strings in tests."
//
// The default voice is plain English aimed at someone who doesn't own the
// CM108B datasheet. When --verbose is set, friendlyError falls through to
// the underlying error text so the technical detail (hex addresses, register
// names, internal prefixes) is one keystroke away.

// =====================================================================
// Help text — Use / Short / Long
// =====================================================================

const (
	shortRoot = "Read, write, and validate the configuration on OpenVLM USB devices"
	longRoot  = `openvlm reads, writes, and validates the configuration on OpenVLM
USB audio devices.

What you can do:
  openvlm list        show every device plugged in
  openvlm identify    check if a device is OpenVLM
  openvlm read        save the configuration to a file
  openvlm dump        show the configuration
  openvlm write       load a configuration from a file
  openvlm update      change one setting
  openvlm provision   apply the OpenVLM defaults
  openvlm wipe        erase the configuration

Run 'openvlm <verb> --help' for details on each verb.

Add --verbose (-v) to any command for more technical detail in
errors, warnings, and diagnostics.`

	flagSerialHelp  = "pick the device whose USB serial-number string matches this value"
	flagVerboseHelp = "show technical detail in errors, warnings, and diagnostics"

	useList   = "list"
	shortList = "Show every OpenVLM device plugged in"
	longList  = `Lists every USB audio device plugged in that looks like an OpenVLM.

For each device it shows the serial number, the device path, whether
the device is confirmed as OpenVLM hardware, and any error from probing.

Example:
  openvlm list

Exits 0 if at least one matching device was found, 1 if none were.`

	useIdentify   = "identify"
	shortIdentify = "Check if a device is OpenVLM (exit 0 if yes, 3 if no)"
	longIdentify  = `Checks whether the selected device is an OpenVLM (its identity
bit is set).

Useful in shell scripts:

  if openvlm identify; then
    openvlm provision
  fi

Exit codes:
  0  the device is OpenVLM
  1  no device plugged in, permission denied, or other error
  3  a device is plugged in but it isn't OpenVLM`

	useRead   = "read"
	shortRead = "Save the device's configuration to a file (or stdout)"
	longRead  = `Saves the device's full configuration to a file or pipes it to stdout.

The output is 128 bytes of raw binary. You can pipe it back through
'openvlm write -i <file>' to restore an exact copy.

Examples:
  openvlm read -o backup.bin
  openvlm read > backup.bin

On Windows PowerShell, '>' and '|' silently corrupt binary streams
(UTF-16 BOM / ASCII re-encoding). Prefer -o, or wrap the call with
'cmd /c "openvlm read > backup.bin"'. The CLI refuses to write a raw
binary EEPROM image to stdout when PowerShell is the parent shell;
pass --force-stdout to override if you know your pipeline preserves
raw bytes.`

	flagReadOutputHelp = "file to write the 128-byte image to (default: stdout)"

	flagReadForceStdoutHelp = "bypass the Windows PowerShell binary-stdout guard " +
		"(use only if you know your shell preserves raw bytes)"

	useDump   = "dump"
	shortDump = "Show the device's configuration as YAML, text, or hex"
	longDump  = `Reads the device's configuration and shows it in a readable form.

Formats:
  yaml   editable config you can pipe back into 'openvlm write -i -' (default)
  text   human-readable summary
  hex    raw byte view for debugging

Examples:
  openvlm dump
  openvlm dump --format text
  openvlm dump --format hex`

	flagDumpFormatHelp = "output format: yaml | text | hex"

	useWrite   = "write"
	shortWrite = "Load a configuration onto the device (binary or YAML)"
	longWrite  = `Writes a full configuration to the device.

Accepts two input forms:
  - a 128-byte binary backup from 'openvlm read'
  - a YAML config (full or partial) — missing fields are filled from
    the OpenVLM defaults

Every value is checked before any byte hits the device, and each word
is read back after writing to make sure it stuck.

Examples:
  openvlm write -i backup.bin
  openvlm write -i config.yaml
  openvlm write -i config.yaml --force
  openvlm dump --format yaml | openvlm write -i -

A few fields can't be changed — VID, PID, product-string, and
manufacturer-string are part of the device's identity. The --force
flag is only needed for fresh devices whose identity bit hasn't been
set yet.`

	flagWriteInputHelp = "input file: 128-byte raw .bin (matches 'read' output) or YAML " +
		"(matches 'dump --format yaml'); use - for stdin"
	flagWriteForceHelp = "program a device even if its identity bit isn't set yet"

	useUpdate   = "update <field> <value>"
	shortUpdate = "Change one setting on the device"

	flagUpdateForceHelp = "program a device even if its identity bit isn't set yet"

	useProvision   = "provision"
	shortProvision = "Apply the OpenVLM factory defaults, with optional overrides"
	longProvision  = `Writes the OpenVLM factory defaults to the device, with optional
overrides.

Overrides come in two layers (later wins over earlier):
  1. --overrides <file.yaml>            (any subset of fields)
  2. per-field flags like --serial or --dac-init-volume

Examples:
  openvlm provision
  openvlm provision --serial "00001234"
  openvlm provision --overrides factory.yaml
  openvlm provision --overrides factory.yaml --dac-init-volume -6
  openvlm provision --dry-run                  # preview without writing
  openvlm provision --force                    # fresh device, skip safety check

A few values can't be overridden — VID, PID, product-string, and
manufacturer-string are part of the device's identity. Bad values are
caught before any byte hits the device.`

	flagProvisionOverridesHelp = "YAML file whose keys override the factory defaults"
	flagProvisionDryRunHelp    = "preview the configuration without writing it to the device"
	flagProvisionForceHelp     = "program a device even if its identity bit isn't set yet"

	useWipe   = "wipe"
	shortWipe = "Erase the device's configuration"
	longWipe  = `Erases everything in the device's configuration memory.

After a wipe, the device behaves like a brand-new chip:
  - it identifies as a generic CM108 (not OpenVLM)
  - re-provisioning it will require --force

Safety:
  --yes is required. There's no interactive prompt.
  --force is required if the device isn't already confirmed as OpenVLM.

Examples:
  openvlm wipe --yes
  openvlm wipe --yes --pattern 00
  openvlm wipe --yes --force        # for fresh or post-wipe devices`

	flagWipeYesHelp     = "required confirmation — wipe refuses to run without this"
	flagWipeForceHelp   = "wipe a device even if its identity bit isn't set yet"
	flagWipePatternHelp = "byte pattern to write to every word: FF (default, factory-blank) or 00"
)

// longUpdate is a var rather than a const so it can interpolate the live
// list of field names from the eeprom package — a function call isn't a
// constant expression.
//
//nolint:gochecknoglobals // package-level help text built at init
var longUpdate = `Changes one setting on the device and writes the result back.

Values are entered as plain decimal numbers, true/false, or named
values:

  openvlm update serial "00001234"
  openvlm update dac-init-volume -6
  openvlm update mic-boost true
  openvlm update boost-mode 22db
  openvlm update dac-output headset

Available fields:
  ` + eeprom.FieldList() + `

A few fields can't be changed — VID, PID, product-string, and
manufacturer-string are part of the device's identity.`

// =====================================================================
// Success and progress messages
// =====================================================================

// displayName is the user-facing name for a device. Prefer the USB serial
// number (stable across plug-in/plug-out); fall back to a generic "OpenVLM
// device" label when the device has no serial, since the OS device path is
// noisy on Windows (`\\?\hid#vid_...#{guid}`) and not useful to most users.
// Pass --verbose to surface the path on error.
func displayName(serial, _ string) string {
	if serial != "" {
		return serial
	}

	return "OpenVLM device"
}

func msgIdentified(name string) string {
	return fmt.Sprintf("%s: confirmed — this is an OpenVLM device.", name)
}

func msgWritten(name string, n int) string {
	return fmt.Sprintf("%s: wrote and verified %d bytes.", name, n)
}

func msgUpdated(name, field string) string {
	return fmt.Sprintf("%s: updated %s.", name, field)
}

func msgProvisioned(name string) string {
	return fmt.Sprintf("%s: applied the OpenVLM defaults.", name)
}

func msgWiped(name string) string {
	return fmt.Sprintf("%s: erased. Unplug and plug back in for the change to take effect.", name)
}

func msgDryRun(n int) string {
	return fmt.Sprintf("Dry-run: would write %d bytes. No changes made.", n)
}

func msgReadComplete(name string, n int, dest string) string {
	return fmt.Sprintf("%s: saved %d bytes to %s.", name, n, dest)
}

func msgNoDevices() string {
	return "No OpenVLM devices are plugged in."
}

// =====================================================================
// Warnings
// =====================================================================

func msgForceWarning() string {
	return "Warning: device's identity bit isn't set; continuing because --force was passed."
}

// errPowerShellStdoutGuard is returned when `openvlm read` would write a
// raw 128-byte EEPROM image to stdout from a PowerShell session, where
// PowerShell's `>` (UTF-16LE BOM) and `|` (ASCII via $OutputEncoding)
// silently corrupt binary streams. The message offers three remedies in
// order of preference.
func errPowerShellStdoutGuard() error {
	return errors.New(
		"PowerShell's '>' redirect and '|' pipe corrupt binary streams " +
			"(stdout would be re-encoded as UTF-16 or ASCII, not raw bytes).\n" +
			"Use one of:\n" +
			"  openvlm read -o backup.bin                    write directly to a file (recommended)\n" +
			"  cmd /c \"openvlm read > backup.bin\"            cmd.exe redirect is binary-safe\n" +
			"  openvlm read --force-stdout > backup.bin      override this check")
}

// msgChipBlank is the warning printed by 'dump --format text' when the chip
// isn't programmed yet. Verbose mode appends the raw VID/PID for diagnostics.
func msgChipBlank(verbose bool, vid, pid uint16) string {
	if verbose {
		return fmt.Sprintf(
			"Warning: this device's configuration looks blank or corrupted. (read VID:PID 0x%04X:0x%04X)",
			vid, pid)
	}

	return "Warning: this device's configuration looks blank or corrupted."
}

// =====================================================================
// Error translation
// =====================================================================

// friendlyError turns an internal error into something a non-technical user
// can act on. Recognized sentinels get a hand-written translation; everything
// else falls through to err.Error() with internal prefixes ("eeprom: ",
// "cm108: ") stripped, unless verbose is set.
//
// The translator runs once, in Execute(), just before printing — so internal
// error wording stays useful for tests and bug reports.
func friendlyError(err error, verbose bool) string {
	if err == nil {
		return ""
	}

	// Sentinel translations. Order matters when one error wraps another:
	// check the more specific sentinel first.
	switch {
	case errors.Is(err, cm108.ErrSerialNotFound):
		return "No device has the serial number you asked for.\n" +
			"Run 'openvlm list' to see what's connected."

	case errors.Is(err, cm108.ErrAmbiguousDevice):
		return "More than one device is plugged in.\n" +
			"Use --serial <serial> to pick which one. Run 'openvlm list' to see them."

	case errors.Is(err, cm108.ErrNoOpenVLMStrapped):
		return "Devices are plugged in, but none of them are confirmed as OpenVLM.\n" +
			"Use --force to program one anyway, or --serial <serial> to pick a specific device."

	case errors.Is(err, cm108.ErrNoDevice):
		return msgNoDevices() + "\nPlug one in and try again."

	case errors.Is(err, eeprom.ErrFieldLocked):
		return "That field is part of the device's identity and can't be changed by this tool."

	case errors.Is(err, eeprom.ErrFieldUnknown):
		return friendlyUnknownField(err)

	case errors.Is(err, eeprom.ErrHexInput):
		return "Numbers must be in regular decimal form. (No 0x, 0b, or 0o prefixes.)"

	case errors.Is(err, eeprom.ErrVerifyMismatch):
		return friendlyVerifyMismatch(err, verbose)
	}

	// Unknown error — strip noisy internal prefixes for default output. In
	// verbose mode, leave them in so the source layer is obvious.
	if verbose {
		return err.Error()
	}

	return stripInternalPrefixes(err.Error())
}

// friendlyUnknownField extracts the field name (and the "known fields" list,
// if any) from eeprom's wrapped ErrFieldUnknown so the user gets a useful
// hint instead of the raw "eeprom: unknown field: \"foo\" (known fields: ...)".
func friendlyUnknownField(err error) string {
	msg := err.Error()
	// eeprom.ApplyUpdate produces:
	//   eeprom: unknown field: "foo" (known fields: a, b, c)
	// Drop the "eeprom: " prefix and lift the rest verbatim — the wording
	// underneath is already user-friendly.
	return capitalize(stripInternalPrefixes(msg))
}

// friendlyVerifyMismatch handles the post-write read-back failure. Verbose
// mode includes the offending word address; default mode says what to do.
func friendlyVerifyMismatch(err error, verbose bool) string {
	base := "The device accepted the write but read back a different value.\n" +
		"Unplug it, plug it back in, and try the command once more."

	var ve *eeprom.VerifyError
	if verbose && errors.As(err, &ve) {
		return base + fmt.Sprintf(
			"\n  detail: word 0x%02X — wrote 0x%04X, read 0x%04X",
			ve.Addr, ve.Wanted, ve.GotValue)
	}

	return base
}

// stripInternalPrefixes removes leading "eeprom: " / "cm108: " / "openvlm: "
// markers from each line of an error message. These are useful for grep and
// debugging but read as noise to a non-technical user.
//
// The function operates per-line because errors.Join (used by View.Validate)
// produces newline-separated multi-errors where every line is independently
// prefixed.
func stripInternalPrefixes(s string) string {
	prefixes := []string{"eeprom: ", "cm108: ", "openvlm: "}

	lines := strings.Split(s, "\n")
	for i, line := range lines {
		// Strip in a fixed-point loop so stacked prefixes
		// (e.g. wrapping has produced "eeprom: cm108: ...") all come off.
		for {
			stripped := line
			for _, p := range prefixes {
				stripped = strings.TrimPrefix(stripped, p)
			}

			if stripped == line {
				break
			}

			line = stripped
		}

		lines[i] = line
	}

	return strings.Join(lines, "\n")
}

// capitalize uppercases the first letter of a string. Used so messages that
// previously read as "no x: foo" read as "No x: foo" after the prefix strip.
func capitalize(s string) string {
	if s == "" {
		return s
	}

	if s[0] >= 'a' && s[0] <= 'z' {
		return string(s[0]-'a'+'A') + s[1:]
	}

	return s
}
