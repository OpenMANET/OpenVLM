package cmd

import (
	"fmt"
	"strings"

	"github.com/openmanet/openvlm/internal/eeprom"
	"github.com/spf13/cobra"
	"github.com/spf13/pflag"
)

// partialOverrides is the live PartialView built up from per-field CLI
// flags on the `provision` command. Flags read into pointer fields so
// "the user did not pass --foo" stays observable as nil.
//
//nolint:gochecknoglobals // shared across flag registration and runProvision
var partialOverrides eeprom.PartialView

// truthyValue is the string pflag uses for "this Var-flag was passed without
// an explicit value" — see flag.NoOptDefVal. We define it once here so the
// lint config doesn't complain about repeated string literals.
const truthyValue = "true"

// boolPair represents one bool field with its --no-<name> partner. The
// custom Var implementation lets `--mic-boost` / `--no-mic-boost` flow
// into a single *bool; pflag's BoolVar/BoolVarP would need two separate
// destination variables.
type boolPair struct {
	target **bool
	value  bool
}

func newBoolPair(target **bool, value bool) *boolPair { return &boolPair{target: target, value: value} }
func (b *boolPair) String() string                    { return "" }
func (b *boolPair) Type() string                      { return "bool" }
func (b *boolPair) IsBoolFlag() bool                  { return true }

func (b *boolPair) Set(s string) error {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case truthyValue, "1", "":
		v := b.value
		*b.target = &v

		return nil
	case "false", "0":
		v := !b.value
		*b.target = &v

		return nil
	}

	return fmt.Errorf("invalid bool: %q", s)
}

// noHexInt is a pflag.Value that parses signed decimal integers but rejects
// any hex/octal/binary prefix. Mirrors eeprom.parseDecimalInt at the flag
// boundary so the user gets a clear "no hex" message from the flag parser.
type noHexInt struct {
	target **int
	field  eeprom.Field
}

func newNoHexInt(target **int, field eeprom.Field) *noHexInt {
	return &noHexInt{target: target, field: field}
}

func (n *noHexInt) String() string { return "" }
func (n *noHexInt) Type() string   { return "int" }

func (n *noHexInt) Set(s string) error {
	v, err := eeprom.ApplyUpdate(eeprom.View{}, string(n.field), s)
	_ = v
	// We don't actually want the View update here, just the parse + range
	// guarantees. ApplyUpdate returns the appropriate "no hex" / "non-decimal"
	// errors.
	if err != nil {
		return err //nolint:wrapcheck // ApplyUpdate already produces the user-facing message
	}
	// Re-parse the value into a fresh int because ApplyUpdate threw away the
	// scalar result; cheaper than a separate parser path.
	parsed, perr := parseFlagDecimal(s)
	if perr != nil {
		return fmt.Errorf("%s: %w", n.field, perr)
	}

	*n.target = &parsed

	return nil
}

// stringPtrFlag captures a *string flag without bool-pair semantics.
type stringPtrFlag struct{ target **string }

func newStringPtr(target **string) *stringPtrFlag { return &stringPtrFlag{target: target} }
func (f *stringPtrFlag) String() string           { return "" }
func (f *stringPtrFlag) Type() string             { return "string" }

func (f *stringPtrFlag) Set(s string) error {
	*f.target = &s

	return nil
}

// boostModeFlag / dacOutputFlag are enum-style flags that delegate validation
// to the canonical IsBoostMode / IsDACOutput helpers.
type boostModeFlag struct{ target **eeprom.BoostMode }

func newBoostModeFlag(target **eeprom.BoostMode) *boostModeFlag {
	return &boostModeFlag{target: target}
}
func (f *boostModeFlag) String() string { return "" }
func (f *boostModeFlag) Type() string   { return "12db|22db" }

func (f *boostModeFlag) Set(s string) error {
	v := eeprom.BoostMode(strings.ToLower(s))
	if !eeprom.IsBoostMode(v) {
		return fmt.Errorf("boost-mode: %q is not one of %s, %s",
			s, eeprom.Boost12dB, eeprom.Boost22dB)
	}

	*f.target = &v

	return nil
}

type dacOutputFlag struct{ target **eeprom.DACOutput }

func newDACOutputFlag(target **eeprom.DACOutput) *dacOutputFlag {
	return &dacOutputFlag{target: target}
}
func (f *dacOutputFlag) String() string { return "" }
func (f *dacOutputFlag) Type() string   { return "speaker|headset" }

func (f *dacOutputFlag) Set(s string) error {
	v := eeprom.DACOutput(strings.ToLower(s))
	if !eeprom.IsDACOutput(v) {
		return fmt.Errorf("dac-output: %q is not one of %s, %s",
			s, eeprom.DACOutputSpeaker, eeprom.DACOutputHeadset)
	}

	*f.target = &v

	return nil
}

// registerOverrideFlags adds one CLI flag per PartialView field on cmd.
// Bool fields are registered as `--<name>` plus `--no-<name>`; numeric
// fields use the no-hex parser; enums use their typed flags. There is no
// `--vid` or `--pid` flag — those fields are absent from PartialView.
func registerOverrideFlags(cmd *cobra.Command) {
	fs := cmd.Flags()

	addBool := func(name string, defaultValue bool, target **bool, help string) {
		fs.Var(newBoolPair(target, true), name, help)
		// NoOptDefVal lets pflag accept the flag standalone (without "=true")
		// — required because Var-registered flags don't auto-detect
		// IsBoolFlag for argument-consumption logic.
		fs.Lookup(name).NoOptDefVal = truthyValue

		fs.Var(newBoolPair(target, false), "no-"+name, "set "+name+" to "+oppositeWord(defaultValue))
		fs.Lookup("no-" + name).NoOptDefVal = truthyValue
		fs.Lookup("no-" + name).Hidden = true
	}

	addInt := func(name string, field eeprom.Field, target **int, help string) {
		fs.Var(newNoHexInt(target, field), name, help+" (decimal only)")
	}

	addString := func(name string, target **string, help string) {
		fs.Var(newStringPtr(target), name, help)
	}

	addString("serial", &partialOverrides.Serial,
		"USB serial number string (≤12 printable ASCII chars)")

	addInt("dac-init-volume", eeprom.FieldDACInitVolume, &partialOverrides.DACInitVolume,
		"playback initial volume in dB (-37..0)")
	addInt("adc-init-volume", eeprom.FieldADCInitVolume, &partialOverrides.ADCInitVolume,
		"recording initial volume in dB (-12..23)")
	addInt("aa-init-volume", eeprom.FieldAAInitVolume, &partialOverrides.AAInitVolume,
		"analog mixer (sidetone) initial volume in dB (-23..8)")

	addInt("dac-min-volume", eeprom.FieldDACMinVolume, &partialOverrides.DACMinVolume,
		"playback minimum volume in dB")
	addInt("dac-max-volume", eeprom.FieldDACMaxVolume, &partialOverrides.DACMaxVolume,
		"playback maximum volume in dB")
	addInt("adc-min-volume", eeprom.FieldADCMinVolume, &partialOverrides.ADCMinVolume,
		"recording minimum volume in dB")
	addInt("adc-max-volume", eeprom.FieldADCMaxVolume, &partialOverrides.ADCMaxVolume,
		"recording maximum volume in dB")
	addInt("aa-min-volume", eeprom.FieldAAMinVolume, &partialOverrides.AAMinVolume,
		"analog mixer minimum volume in dB")
	addInt("aa-max-volume", eeprom.FieldAAMaxVolume, &partialOverrides.AAMaxVolume,
		"analog mixer maximum volume in dB")

	fs.Var(newBoostModeFlag(&partialOverrides.BoostMode), "boost-mode",
		"mic preamp gain stage")
	fs.Var(newDACOutputFlag(&partialOverrides.DACOutput), "dac-output",
		"USB audio terminal type")

	addBool("mic-boost", true, &partialOverrides.MicBoost,
		"enable mic preamp")
	addBool("mic-high-pass-filter", true, &partialOverrides.MicHighPassFilter,
		"enable mic input high-pass filter")
	addBool("mic-pll-adjust", false, &partialOverrides.MicPLLAdjust,
		"enable mic PLL frequency adjust")
	addBool("hid-enable", true, &partialOverrides.HIDEnable,
		"enable USB HID interface (volume buttons, GPIO)")
	addBool("remote-wakeup", false, &partialOverrides.RemoteWakeup,
		"enable USB remote-wakeup capability")
	addBool("dac-shutdown", false, &partialOverrides.DACShutdown,
		"shut down DAC analog circuits")
	addBool("total-power-control", false, &partialOverrides.TotalPowerControl,
		"enable total power control")
	addBool("serial-enable", false, &partialOverrides.SerialEnable,
		"present USB serial-number string descriptor")
	addBool("extended-fields-valid", true, &partialOverrides.ExtendedFieldsValid,
		"mark word 0x2A/0x2B/0x32 fields as valid")
	addBool("dac-max-min-volume-valid", false, &partialOverrides.DACMaxMinVolumeValid,
		"honor dac-min-volume / dac-max-volume from EEPROM")
	addBool("adc-max-min-volume-valid", false, &partialOverrides.ADCMaxMinVolumeValid,
		"honor adc-min-volume / adc-max-volume from EEPROM")
	addBool("aa-max-min-volume-valid", false, &partialOverrides.AAMaxMinVolumeValid,
		"honor aa-min-volume / aa-max-volume from EEPROM")
}

// resetOverrides clears the package-level partialOverrides between command
// invocations. Called by Cobra's PreRun hook on `provision`; tests use it
// directly to reset state between table-driven cases.
func resetOverrides() { partialOverrides = eeprom.PartialView{} }

func oppositeWord(b bool) string {
	if b {
		return "false"
	}

	return "true"
}

// parseFlagDecimal is the shared signed-decimal parser used by the flag
// layer. It rejects the hex/octal/binary prefixes that strconv.Atoi would
// accept via prefix shenanigans, mirroring eeprom.parseDecimalInt's
// behavior.
func parseFlagDecimal(s string) (int, error) {
	v := strings.TrimSpace(s)
	if v == "" {
		return 0, fmt.Errorf("empty value")
	}

	body := v
	if body[0] == '-' || body[0] == '+' {
		body = body[1:]
	}

	if len(body) >= 2 && body[0] == '0' && (body[1] == 'x' || body[1] == 'X' ||
		body[1] == 'b' || body[1] == 'B' || body[1] == 'o' || body[1] == 'O') {
		return 0, fmt.Errorf("hex/binary/octal numeric input is not accepted; use decimal: %q", s)
	}

	for _, r := range body {
		if r < '0' || r > '9' {
			return 0, fmt.Errorf("non-decimal characters in %q", s)
		}
	}

	var (
		n    int
		sign = 1
	)

	switch v[0] {
	case '-':
		sign = -1
		v = v[1:]
	case '+':
		v = v[1:]
	}

	for _, r := range v {
		n = n*10 + int(r-'0')
	}

	return sign * n, nil
}

// referenced to silence unused-import linters when only Var is used.
var _ = pflag.NewFlagSet
