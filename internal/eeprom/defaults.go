package eeprom

// OpenVLMDefaults is the canonical EEPROM image programmed onto OpenVLM
// devices at the factory.
//
// Edit this struct literal to retune defaults for new hardware revisions.
// Runtime overrides (YAML files and per-field CLI flags on
// `openvlm provision`) are applied on top of these values via
// (*View).ApplyOverrides; they never modify this struct.
//
// VID and PID are intentionally absent: the codec sources them straight
// from cm108.OpenVLMVendorID / OpenVLMProductID at encode time (see
// "VID/PID are write-locked" in the plan), so they can never drift away
// from the constants the discover code matches against.
//
//nolint:gochecknoglobals // package-level defaults are the whole point of this file
var OpenVLMDefaults = View{
	// Word 0x00. ExtendedFieldsValid must be true for the chip to honor
	// the volume-init / analog-config words below.
	ExtendedFieldsValid: true,
	SerialEnable:        false,

	// Strings. ProductString and ManufacturerString are write-locked at the
	// input layers (YAML, --flag, `update`); the chip always receives these
	// compiled-in values. Only Serial is overridable per device via
	// `--serial`.
	Serial:             "",
	ProductString:      "OpenVLM",
	ManufacturerString: "BuildsByShane",

	// Word 0x2A — datasheet defaults.
	DACInitVolume:        0,
	ADCInitVolume:        8,
	DACMaxMinVolumeValid: false,
	ADCMaxMinVolumeValid: false,
	AAMaxMinVolumeValid:  false,

	// Word 0x2B — datasheet defaults.
	AAInitVolume:      -7,
	BoostMode:         Boost12dB,
	DACShutdown:       false,
	TotalPowerControl: false,
	MicHighPassFilter: true,
	MicPLLAdjust:      false,
	MicBoost:          true,
	DACOutput:         DACOutputSpeaker,
	HIDEnable:         true,
	RemoteWakeup:      false,

	// Words 0x2C..0x31 — datasheet-default min/max ranges.
	DACMinVolume: -37,
	DACMaxVolume: 0,
	ADCMinVolume: -22,
	ADCMaxVolume: 23,
	AAMinVolume:  -23,
	AAMaxVolume:  8,
}
