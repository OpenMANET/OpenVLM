package eeprom

// ApplyOverrides returns a new View constructed by overlaying every non-nil
// field of `o` onto a copy of `base`. The base is not modified.
//
// This is the merge step that powers the layered override channels:
//
//	OpenVLMDefaults → YAML overrides → CLI flag overrides
//
// The caller stacks PartialViews in the desired precedence order
// (rightmost wins) and calls ApplyOverrides once per layer.
func ApplyOverrides(base View, o *PartialView) View {
	if o == nil {
		return base
	}

	if o.ExtendedFieldsValid != nil {
		base.ExtendedFieldsValid = *o.ExtendedFieldsValid
	}

	if o.SerialEnable != nil {
		base.SerialEnable = *o.SerialEnable
	}

	if o.Serial != nil {
		base.Serial = *o.Serial
	}

	if o.DACInitVolume != nil {
		base.DACInitVolume = *o.DACInitVolume
	}

	if o.ADCInitVolume != nil {
		base.ADCInitVolume = *o.ADCInitVolume
	}

	if o.DACMaxMinVolumeValid != nil {
		base.DACMaxMinVolumeValid = *o.DACMaxMinVolumeValid
	}

	if o.ADCMaxMinVolumeValid != nil {
		base.ADCMaxMinVolumeValid = *o.ADCMaxMinVolumeValid
	}

	if o.AAMaxMinVolumeValid != nil {
		base.AAMaxMinVolumeValid = *o.AAMaxMinVolumeValid
	}

	if o.AAInitVolume != nil {
		base.AAInitVolume = *o.AAInitVolume
	}

	if o.BoostMode != nil {
		base.BoostMode = *o.BoostMode
	}

	if o.DACShutdown != nil {
		base.DACShutdown = *o.DACShutdown
	}

	if o.TotalPowerControl != nil {
		base.TotalPowerControl = *o.TotalPowerControl
	}

	if o.MicHighPassFilter != nil {
		base.MicHighPassFilter = *o.MicHighPassFilter
	}

	if o.MicPLLAdjust != nil {
		base.MicPLLAdjust = *o.MicPLLAdjust
	}

	if o.MicBoost != nil {
		base.MicBoost = *o.MicBoost
	}

	if o.DACOutput != nil {
		base.DACOutput = *o.DACOutput
	}

	if o.HIDEnable != nil {
		base.HIDEnable = *o.HIDEnable
	}

	if o.RemoteWakeup != nil {
		base.RemoteWakeup = *o.RemoteWakeup
	}

	if o.DACMinVolume != nil {
		base.DACMinVolume = *o.DACMinVolume
	}

	if o.DACMaxVolume != nil {
		base.DACMaxVolume = *o.DACMaxVolume
	}

	if o.ADCMinVolume != nil {
		base.ADCMinVolume = *o.ADCMinVolume
	}

	if o.ADCMaxVolume != nil {
		base.ADCMaxVolume = *o.ADCMaxVolume
	}

	if o.AAMinVolume != nil {
		base.AAMinVolume = *o.AAMinVolume
	}

	if o.AAMaxVolume != nil {
		base.AAMaxVolume = *o.AAMaxVolume
	}

	return base
}
