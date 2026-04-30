package cmd

import (
	"fmt"

	"github.com/openmanet/openvlm/internal/cm108"
	"github.com/openmanet/openvlm/internal/hidx"
)

// backend returns the HID backend the rest of the CLI talks through. Tests
// override it via SetBackend so they can inject hidx.FakeBackend without
// touching real hardware.
//
//nolint:gochecknoglobals // package-level seam is the cleanest way to inject the fake in tests
var backend hidx.Backend = hidx.NewBackend()

// SetBackend swaps the HID backend used by every subcommand. Tests call
// this in setup; production code never touches it.
func SetBackend(b hidx.Backend) hidx.Backend {
	prev := backend
	backend = b

	return prev
}

// pickDevice runs cm108.List + cm108.Pick using the persistent --serial
// flag. Returns the chosen Descriptor (with IsOpenVLM populated) or a
// usage-style error.
func pickDevice() (cm108.Descriptor, error) {
	descs, err := cm108.List(backend)
	if err != nil {
		return cm108.Descriptor{}, err //nolint:wrapcheck // already prefixed by cm108.List
	}

	d, err := cm108.Pick(descs, cm108.PickOptions{Serial: flagSerial})
	if err != nil {
		return cm108.Descriptor{}, &usageError{err: err}
	}

	return d, nil
}

// openDevice picks a device and opens its HID transport. Caller must Close
// the returned transport.
func openDevice() (cm108.Descriptor, hidx.Transport, error) {
	d, err := pickDevice()
	if err != nil {
		return cm108.Descriptor{}, nil, err
	}

	t, err := backend.Open(d.Path)
	if err != nil {
		return d, nil, fmt.Errorf("couldn't open %s: %w", d.Path, err)
	}

	return d, t, nil
}

// requireOpenVLM enforces the identity-bit safety gate used by every write
// path. force=true (set by --force) skips the gate but still surfaces the
// non-confirmation in stderr so the user is aware.
func requireOpenVLM(d cm108.Descriptor, force bool) error {
	if d.IsOpenVLM {
		return nil
	}

	if force {
		_, _ = fmt.Fprintln(rootCmd.ErrOrStderr(), msgForceWarning())

		return nil
	}

	name := displayName(d.SerialNumber, d.Path)

	reason := "this doesn't look like an OpenVLM dongle (its identity bit isn't set)"
	if d.ProbeError != nil {
		reason = "couldn't check the dongle's identity bit: " + d.ProbeError.Error()
	}

	return &notIdentifiedError{
		err: fmt.Errorf("%s: %s. Pass --force to program it anyway", name, reason),
	}
}
