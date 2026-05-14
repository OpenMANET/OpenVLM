// Package cm108 owns the OpenVLM-specific knowledge that sits on top of the
// generic hidx transport: the exact USB VID/PID we accept, the GPIO1 strap
// probe that confirms a device is OpenVLM hardware, the Descriptor type
// that pairs hidx.DeviceInfo with that confirmation, and the small selector
// that picks one device out of the enumerated set.
//
// Anything that talks to the wire lives in hidx; anything that knows what
// "OpenVLM" means lives here.
package cm108

// USB identifiers the CLI is willing to operate on. These are the only IDs
// `openvlm list` will surface and the only IDs `openvlm provision` will
// program. They mirror the constants used in openmanetd's hazel branch so a
// future shared package can be a drop-in replacement.
const (
	// OpenVLMVendorID is the C-Media Electronics USB vendor identifier.
	OpenVLMVendorID uint16 = 0x0D8C

	// OpenVLMProductID identifies the OpenVLM (Open Voice Link Module) USB
	// audio device. It is the factory-default CM108B PID; programming a
	// different PID would prevent this CLI from finding the device, which
	// is why VID/PID are write-locked (see the plan, "VID/PID are
	// write-locked").
	OpenVLMProductID uint16 = 0x0012
)
