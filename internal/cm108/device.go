package cm108

// Descriptor describes one CM108-family device discovered on the host plus
// whether the GPIO1 hardware strap confirms it is OpenVLM.
type Descriptor struct {
	ProbeError       error
	Path             string
	SerialNumber     string
	ProductName      string
	ManufacturerName string
	VendorID         uint16
	ProductID        uint16
	IsOpenVLM        bool
}
