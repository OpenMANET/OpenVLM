package eeprom

// BoostMode is the mic-boost gain stage selector (CM108B word 0x2B bit 9).
//
// The datasheet documents the encoding as 1=12dB, 0=22dB; we expose the
// human-readable string in `View` and convert at encode time.
type BoostMode string

const (
	Boost12dB BoostMode = "12db"
	Boost22dB BoostMode = "22db"
)

// DACOutput is the speaker-vs-headset terminal-property selector
// (CM108B word 0x2B bit 2).
type DACOutput string

const (
	DACOutputSpeaker DACOutput = "speaker"
	DACOutputHeadset DACOutput = "headset"
)

// View is the user-facing decoded representation of the EEPROM image. It
// carries every documented field from CM108B datasheet §7.1.3 in human
// units (integer dB, plain strings, bools, named enums); register-level
// encodings are produced on demand by Encode().
//
// Reserved bits and the 12-bit magic nibble are intentionally absent — the
// codec sets them to the documented "must be" values on every encode and
// flags drift on decode, so the user can't violate datasheet invariants by
// accident.
//
// VID and PID are intentionally absent — they are write-locked and sourced
// from cm108.OpenVLMVendorID/ProductID at encode time. Read-side display
// of the device's current VID/PID lives on Image, not on View.
//
// ProductString and ManufacturerString are kept on the struct so the decoder
// can populate them for `openvlm dump --format text`, but they are tagged
// `yaml:"-"` because they are write-locked: a YAML dump must round-trip
// through `provision --overrides` without re-introducing fields that
// `UnmarshalPartial` will reject.
type View struct {
	DACOutput            DACOutput `yaml:"dac-output"`
	BoostMode            BoostMode `yaml:"boost-mode"`
	Serial               string    `yaml:"serial"`
	ProductString        string    `yaml:"-"`
	ManufacturerString   string    `yaml:"-"`
	ADCInitVolume        int       `yaml:"adc-init-volume"`
	DACInitVolume        int       `yaml:"dac-init-volume"`
	AAMinVolume          int       `yaml:"aa-min-volume"`
	ADCMaxVolume         int       `yaml:"adc-max-volume"`
	ADCMinVolume         int       `yaml:"adc-min-volume"`
	AAInitVolume         int       `yaml:"aa-init-volume"`
	AAMaxVolume          int       `yaml:"aa-max-volume"`
	DACMaxVolume         int       `yaml:"dac-max-volume"`
	DACMinVolume         int       `yaml:"dac-min-volume"`
	DACShutdown          bool      `yaml:"dac-shutdown"`
	MicPLLAdjust         bool      `yaml:"mic-pll-adjust"`
	MicBoost             bool      `yaml:"mic-boost"`
	MicHighPassFilter    bool      `yaml:"mic-high-pass-filter"`
	HIDEnable            bool      `yaml:"hid-enable"`
	RemoteWakeup         bool      `yaml:"remote-wakeup"`
	TotalPowerControl    bool      `yaml:"total-power-control"`
	ExtendedFieldsValid  bool      `yaml:"extended-fields-valid"`
	AAMaxMinVolumeValid  bool      `yaml:"aa-max-min-volume-valid"`
	ADCMaxMinVolumeValid bool      `yaml:"adc-max-min-volume-valid"`
	DACMaxMinVolumeValid bool      `yaml:"dac-max-min-volume-valid"`
	SerialEnable         bool      `yaml:"serial-enable"`
}

// PartialView mirrors View field-for-field with pointer types so callers can
// express "leave this at its compiled-in default" by simply not setting the
// field. Both YAML decoding (omitted keys → nil) and CLI flag handling
// (unset flag → nil) use this representation.
//
// PartialView intentionally omits the write-locked fields (VID, PID,
// product-string, manufacturer-string). Attempts to set them via YAML or
// flag are rejected at decode / flag-registration time so the chip always
// receives the compiled-in defaults for those identity fields.
type PartialView struct {
	ExtendedFieldsValid *bool `yaml:"extended-fields-valid,omitempty"`
	SerialEnable        *bool `yaml:"serial-enable,omitempty"`

	Serial *string `yaml:"serial,omitempty"`

	DACInitVolume        *int  `yaml:"dac-init-volume,omitempty"`
	ADCInitVolume        *int  `yaml:"adc-init-volume,omitempty"`
	DACMaxMinVolumeValid *bool `yaml:"dac-max-min-volume-valid,omitempty"`
	ADCMaxMinVolumeValid *bool `yaml:"adc-max-min-volume-valid,omitempty"`
	AAMaxMinVolumeValid  *bool `yaml:"aa-max-min-volume-valid,omitempty"`

	AAInitVolume      *int       `yaml:"aa-init-volume,omitempty"`
	BoostMode         *BoostMode `yaml:"boost-mode,omitempty"`
	DACShutdown       *bool      `yaml:"dac-shutdown,omitempty"`
	TotalPowerControl *bool      `yaml:"total-power-control,omitempty"`
	MicHighPassFilter *bool      `yaml:"mic-high-pass-filter,omitempty"`
	MicPLLAdjust      *bool      `yaml:"mic-pll-adjust,omitempty"`
	MicBoost          *bool      `yaml:"mic-boost,omitempty"`
	DACOutput         *DACOutput `yaml:"dac-output,omitempty"`
	HIDEnable         *bool      `yaml:"hid-enable,omitempty"`
	RemoteWakeup      *bool      `yaml:"remote-wakeup,omitempty"`

	DACMinVolume *int `yaml:"dac-min-volume,omitempty"`
	DACMaxVolume *int `yaml:"dac-max-volume,omitempty"`
	ADCMinVolume *int `yaml:"adc-min-volume,omitempty"`
	ADCMaxVolume *int `yaml:"adc-max-volume,omitempty"`
	AAMinVolume  *int `yaml:"aa-min-volume,omitempty"`
	AAMaxVolume  *int `yaml:"aa-max-volume,omitempty"`
}

// IsBoostMode returns true if v names a known boost mode. Used by the
// validator and by the flag/`update` parsers so the same allow-list is the
// single source of truth.
func IsBoostMode(v BoostMode) bool {
	return v == Boost12dB || v == Boost22dB
}

// IsDACOutput returns true if v names a known DAC-output terminal.
func IsDACOutput(v DACOutput) bool {
	return v == DACOutputSpeaker || v == DACOutputHeadset
}
