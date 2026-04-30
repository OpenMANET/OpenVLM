package cm108

import (
	"fmt"

	"github.com/openmanet/openvlm/internal/hidx"
)

// List enumerates every device matching the OpenVLM VID/PID and probes each
// for the GPIO1 strap. The probe outcome is recorded on each Descriptor
// (IsOpenVLM + ProbeError); a per-device probe failure does not abort the
// listing.
//
// On success returns the descriptors in the order the OS reports them.
func List(b hidx.Backend) ([]Descriptor, error) {
	infos, err := b.Enumerate(OpenVLMVendorID, OpenVLMProductID)
	if err != nil {
		return nil, fmt.Errorf("cm108: enumerate: %w", err)
	}

	results := make([]Descriptor, 0, len(infos))

	for _, info := range infos {
		d := Descriptor{
			Path:             info.Path,
			SerialNumber:     info.SerialNumber,
			ProductName:      info.ProductName,
			ManufacturerName: info.ManufacturerName,
			VendorID:         info.VendorID,
			ProductID:        info.ProductID,
		}

		t, openErr := b.Open(info.Path)
		if openErr != nil {
			d.ProbeError = fmt.Errorf("open: %w", openErr)
			results = append(results, d)

			continue
		}

		ok, probeErr := CheckOpenVLMIdentity(t)
		_ = t.Close()

		d.IsOpenVLM = ok
		d.ProbeError = probeErr
		results = append(results, d)
	}

	return results, nil
}
