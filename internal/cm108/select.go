package cm108

import (
	"errors"
	"fmt"
)

// PickOptions controls the behavior of Pick. Both fields are optional:
//
//   - Serial selects the device whose USB serial-number string equals
//     this value. When set, exactly that device must be present or Pick
//     returns ErrSerialNotFound.
//   - When Serial is empty, Pick auto-selects the unique
//     OpenVLM-strapped device. If multiple devices have the strap, or if
//     none of them do but multiple non-strapped CM108s are attached, Pick
//     returns an error asking the user to disambiguate with --serial.
type PickOptions struct {
	Serial string
}

// Sentinel errors so callers can distinguish "no devices at all" (likely
// "device not plugged in / permissions") from "many devices, pick one"
// (user error).
var (
	ErrNoDevice          = errors.New("cm108: no OpenVLM device found")
	ErrSerialNotFound    = errors.New("cm108: no device matched the requested --serial")
	ErrAmbiguousDevice   = errors.New("cm108: multiple devices found; use --serial to disambiguate")
	ErrNoOpenVLMStrapped = errors.New("cm108: no GPIO1-strapped OpenVLM device found")
)

// Pick selects exactly one descriptor from descs according to opts and
// returns it. The caller is responsible for opening it with the Backend.
//
// Selection rules (in priority order):
//
//  1. If opts.Serial is non-empty, return the device whose SerialNumber
//     equals it. Zero matches → ErrSerialNotFound. Multiple matches → an
//     ambiguity error (this should not happen in practice but we surface it
//     rather than picking arbitrarily).
//  2. Otherwise, if exactly one descriptor has IsOpenVLM == true, return it.
//  3. Otherwise, if exactly one descriptor exists overall, return it.
//  4. Otherwise return ErrAmbiguousDevice / ErrNoOpenVLMStrapped /
//     ErrNoDevice as appropriate.
func Pick(descs []Descriptor, opts PickOptions) (Descriptor, error) {
	if len(descs) == 0 {
		return Descriptor{}, ErrNoDevice
	}

	if opts.Serial != "" {
		var matches []Descriptor

		for _, d := range descs {
			if d.SerialNumber == opts.Serial {
				matches = append(matches, d)
			}
		}

		switch len(matches) {
		case 0:
			return Descriptor{}, fmt.Errorf("%w: %q", ErrSerialNotFound, opts.Serial)
		case 1:
			return matches[0], nil
		default:
			return Descriptor{}, fmt.Errorf("%w: serial %q matched %d devices",
				ErrAmbiguousDevice, opts.Serial, len(matches))
		}
	}

	var strapped []Descriptor

	for _, d := range descs {
		if d.IsOpenVLM {
			strapped = append(strapped, d)
		}
	}

	if len(strapped) == 1 {
		return strapped[0], nil
	}

	if len(strapped) > 1 {
		return Descriptor{}, fmt.Errorf("%w: %d OpenVLM-strapped devices",
			ErrAmbiguousDevice, len(strapped))
	}

	if len(descs) == 1 {
		return descs[0], nil
	}

	return Descriptor{}, fmt.Errorf("%w: %d CM108 devices, none strapped",
		ErrNoOpenVLMStrapped, len(descs))
}
