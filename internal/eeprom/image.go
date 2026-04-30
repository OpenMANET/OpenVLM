package eeprom

import (
	"encoding/binary"
	"errors"
	"fmt"
)

// Image is the raw 128-byte EEPROM contents — exactly what `openvlm read`
// dumps to disk and exactly what `openvlm write -i image.bin` accepts.
//
// The CM108B stores 64 little-endian 16-bit words; helpers below treat the
// image as both bytes and words depending on the caller's needs.
type Image [ByteCount]byte

// Word returns the little-endian 16-bit word at the given EEPROM word
// address. Out-of-range addresses return 0.
func (i *Image) Word(addr uint8) uint16 {
	if int(addr)*2+1 >= len(i) {
		return 0
	}

	return binary.LittleEndian.Uint16(i[int(addr)*2:])
}

// SetWord writes the little-endian 16-bit word at the given EEPROM word
// address. Out-of-range addresses are silently ignored.
func (i *Image) SetWord(addr uint8, value uint16) {
	if int(addr)*2+1 >= len(i) {
		return
	}

	binary.LittleEndian.PutUint16(i[int(addr)*2:], value)
}

// VID returns the device's currently programmed USB vendor ID. Provided for
// `dump` / `read` display only; not user-programmable.
func (i *Image) VID() uint16 { return i.Word(addrVID) }

// PID returns the device's currently programmed USB product ID. Provided
// for `dump` / `read` display only; not user-programmable.
func (i *Image) PID() uint16 { return i.Word(addrPID) }

// IsProgrammed reports whether the magic-word nibble at word 0x00 matches
// 0x670. False either means the chip was never programmed or the EEPROM is
// corrupted.
func (i *Image) IsProgrammed() bool {
	return i.Word(addrFlags)&magicMask == magicValue
}

// Decode parses the raw image into a typed View. It returns a non-nil error
// for any decoding problem (bad magic, malformed string, reserved bits set
// in word 0x32, ...). Reserved-bit drift in word 0x00 / 0x2B is reported as
// a non-fatal warning via the second return value; callers can decide
// whether to ignore.
func (i *Image) Decode() (View, []string, error) {
	var (
		v        View
		warnings []string
	)

	flags := i.Word(addrFlags)
	if flags&magicMask != magicValue {
		return View{}, nil, errors.New("eeprom: image magic word missing — chip is unprogrammed or corrupted")
	}

	v.ExtendedFieldsValid = flags&flagBitExtFields != 0
	v.SerialEnable = flags&flagBitSerialEna != 0

	if flags&magicReservedBit2 == 0 {
		warnings = append(warnings, "word 0x00 bit 2 is 0 (datasheet says must be 1)")
	}

	if flags&magicReservedBit0 == 0 {
		warnings = append(warnings, "word 0x00 bit 0 is 0 (datasheet says must be 1)")
	}

	v.Serial = decodeString(i, addrSerialHeader, addrSerialBody, maxSerialBytes)
	v.ProductString = decodeString(i, addrProductHeader, addrProductBody, maxProductBytes)
	v.ManufacturerString = decodeString(i, addrMfrHeader, addrMfrBody, maxMfrBytes)

	w2A := i.Word(addrVolumeInit)
	v.DACInitVolume = signExtend(int(w2A>>dacInitShift)&((1<<dacInitWidth)-1), dacInitWidth)
	v.ADCInitVolume = signExtend(int(w2A>>adcInitShift)&((1<<adcInitWidth)-1), adcInitWidth)
	v.DACMaxMinVolumeValid = w2A&dacMaxMinValid != 0
	v.ADCMaxMinVolumeValid = w2A&adcMaxMinValid != 0
	v.AAMaxMinVolumeValid = w2A&aaMaxMinValid != 0

	w2B := i.Word(addrAnalogConfig)
	v.AAInitVolume = signExtend(int(w2B>>aaInitShift)&((1<<aaInitWidth)-1), aaInitWidth)

	if w2B&bitBoostMode12dB != 0 {
		v.BoostMode = Boost12dB
	} else {
		v.BoostMode = Boost22dB
	}

	v.DACShutdown = w2B&bitDACShutdown != 0
	v.TotalPowerControl = w2B&bitTotalPowerControl != 0
	v.MicHighPassFilter = w2B&bitMicHighPassFilter != 0
	v.MicPLLAdjust = w2B&bitMicPLLAdjust != 0
	v.MicBoost = w2B&bitMicBoost != 0

	if w2B&bitDACOutputHeadset != 0 {
		v.DACOutput = DACOutputHeadset
	} else {
		v.DACOutput = DACOutputSpeaker
	}

	v.HIDEnable = w2B&bitHIDEnable != 0
	v.RemoteWakeup = w2B&bitRemoteWakeup != 0

	if w2B&mask2BReservedZero != 0 {
		warnings = append(warnings, fmt.Sprintf("word 0x2B reserved bits set (mask 0x%04X)", w2B&mask2BReservedZero))
	}

	v.DACMinVolume = decodeDBHighByte(i.Word(addrDACMin))
	v.DACMaxVolume = decodeDBHighByte(i.Word(addrDACMax))
	v.ADCMinVolume = decodeDBHighByte(i.Word(addrADCMin))
	v.ADCMaxVolume = decodeDBHighByte(i.Word(addrADCMax))
	v.AAMinVolume = decodeDBHighByte(i.Word(addrAAMin))
	v.AAMaxVolume = decodeDBHighByte(i.Word(addrAAMax))

	if w := i.Word(addrEEOption2); w != 0 {
		warnings = append(warnings, fmt.Sprintf("word 0x32 (EE_OPTION2) reserved bits set: 0x%04X", w))
	}

	return v, warnings, nil
}

// Encode renders the View into a raw 128-byte image. The caller-provided
// vendorID/productID are written verbatim into words 0x01/0x02 — this is
// the single seam where VID/PID enter the image; View itself does not
// carry them.
//
// Encode does NOT call Validate; callers should always Validate first.
// This split exists so tests can produce and inspect an "intentionally bad"
// image without fighting the validator.
//
// `tail` lets the caller preserve undocumented words 0x33..0x3F across a
// read-modify-write. Pass nil for a fresh provisioning (zeros).
func (v *View) Encode(vendorID, productID uint16, tail [WordCount - 0x33]uint16) Image {
	var img Image

	flags := magicValue | flagReservedAlways

	if v.ExtendedFieldsValid {
		flags |= flagBitExtFields
	}

	if v.SerialEnable {
		flags |= flagBitSerialEna
	}

	img.SetWord(addrFlags, flags)
	img.SetWord(addrVID, vendorID)
	img.SetWord(addrPID, productID)

	encodeString(&img, addrSerialHeader, addrSerialBody, v.Serial, maxSerialBytes)
	encodeString(&img, addrProductHeader, addrProductBody, v.ProductString, maxProductBytes)
	encodeString(&img, addrMfrHeader, addrMfrBody, v.ManufacturerString, maxMfrBytes)

	w2A := uint16(0)
	w2A |= encodeSigned(v.DACInitVolume, dacInitWidth) << dacInitShift
	w2A |= encodeSigned(v.ADCInitVolume, adcInitWidth) << adcInitShift

	if v.DACMaxMinVolumeValid {
		w2A |= dacMaxMinValid
	}

	if v.ADCMaxMinVolumeValid {
		w2A |= adcMaxMinValid
	}

	if v.AAMaxMinVolumeValid {
		w2A |= aaMaxMinValid
	}

	img.SetWord(addrVolumeInit, w2A)

	w2B := uint16(0)
	w2B |= encodeSigned(v.AAInitVolume, aaInitWidth) << aaInitShift

	if v.BoostMode == Boost12dB {
		w2B |= bitBoostMode12dB
	}

	if v.DACShutdown {
		w2B |= bitDACShutdown
	}

	if v.TotalPowerControl {
		w2B |= bitTotalPowerControl
	}

	if v.MicHighPassFilter {
		w2B |= bitMicHighPassFilter
	}

	if v.MicPLLAdjust {
		w2B |= bitMicPLLAdjust
	}

	if v.MicBoost {
		w2B |= bitMicBoost
	}

	if v.DACOutput == DACOutputHeadset {
		w2B |= bitDACOutputHeadset
	}

	if v.HIDEnable {
		w2B |= bitHIDEnable
	}

	if v.RemoteWakeup {
		w2B |= bitRemoteWakeup
	}

	img.SetWord(addrAnalogConfig, w2B)

	img.SetWord(addrDACMin, encodeDBHighByte(v.DACMinVolume))
	img.SetWord(addrDACMax, encodeDBHighByte(v.DACMaxVolume))
	img.SetWord(addrADCMin, encodeDBHighByte(v.ADCMinVolume))
	img.SetWord(addrADCMax, encodeDBHighByte(v.ADCMaxVolume))
	img.SetWord(addrAAMin, encodeDBHighByte(v.AAMinVolume))
	img.SetWord(addrAAMax, encodeDBHighByte(v.AAMaxVolume))

	img.SetWord(addrEEOption2, 0) // reserved-must-be-zero

	// Preserve the undocumented tail words 0x33..0x3F.
	for i, w := range tail {
		img.SetWord(uint8(0x33+i), w)
	}

	return img
}

// signExtend interprets value as a `width`-bit two's complement number and
// returns its sign-extended int.
func signExtend(value, width int) int {
	signBit := 1 << (width - 1)
	if value&signBit == 0 {
		return value
	}

	return value - (1 << width)
}

// encodeSigned packs a signed int into a `width`-bit two's complement field.
//
// CALLER CONTRACT: value must already be in the field's representable range
// (-(1<<(width-1)) .. (1<<(width-1))-1). Out-of-range values are silently
// masked, producing whatever bit pattern the low `width` bits happen to be —
// validate first. The init-volume validator in validate.go is the single
// upstream gate; widening any of dac/adc/aaInitMin/Max past the bit-field
// range will reintroduce silent corruption. See plan Phase G.
func encodeSigned(value, width int) uint16 {
	mask := uint16((1 << width) - 1)

	return uint16(value) & mask
}

// decodeDBHighByte reads the datasheet's "high byte = signed dB, low byte =
// 0" min/max-volume encoding. The CM108B documents defaults like 0xEA00 =
// -22dB (0xEA = -22 as signed int8); we mirror that interpretation.
func decodeDBHighByte(word uint16) int {
	hi := int8(word >> 8)

	return int(hi)
}

func encodeDBHighByte(db int) uint16 {
	return uint16(int8(db)) << 8
}

// decodeString reads a length-prefixed ASCII string from the EEPROM and
// returns it as a Go string.
//
// CM108B string layout per datasheet §7.1.3:
//
//	header word: high byte = first ASCII character
//	             low  byte = USB string descriptor bLength
//	                       = 2 + 2 * char_count
//	                       (datasheet hint: 0x3E -> 30 char, 0x40 -> 31 char)
//	body words : remaining ASCII characters, one per byte
//
// The chip stores plain ASCII in EEPROM but expands each ASCII byte to a
// UTF-16LE pair when serving the USB string descriptor; that is why the
// length field uses USB descriptor math (2 header bytes + 2 bytes per
// emitted UTF-16 char) rather than a raw character count.
func decodeString(img *Image, headerAddr, bodyAddr, maxBytes uint8) string {
	header := img.Word(headerAddr)

	first := byte(header >> 8) // first character
	bLength := int(header & 0xFF)

	// Empty / unprogrammed string.
	if bLength == 0 {
		return ""
	}
	// A valid USB string descriptor bLength is at least 2 (header bytes
	// only, zero-character string) and even (because each char contributes
	// two UTF-16 bytes). Anything else is malformed — surface as empty.
	if bLength < 2 || bLength%2 != 0 {
		return ""
	}

	charCount := (bLength - 2) / 2
	if charCount == 0 {
		return ""
	}
	// Clamp to the declared maximum: 1 header char + maxBytes body chars.
	if charCount > int(maxBytes)+1 {
		charCount = int(maxBytes) + 1
	}

	out := make([]byte, 0, charCount)
	out = append(out, first)

	// Body bytes follow, one ASCII char per byte. The image is little-endian
	// on word boundaries; reading bytes directly produces the natural ASCII
	// order.
	bodyOff := int(bodyAddr) * 2

	for i := 0; i+1 < charCount; i++ {
		if bodyOff+i >= len(img) {
			break
		}

		out = append(out, img[bodyOff+i])
	}

	// Strip trailing NULs and non-printable bytes that some chips leave in
	// the unused tail of the string region.
	for len(out) > 0 && (out[len(out)-1] == 0 || out[len(out)-1] < 0x20) {
		out = out[:len(out)-1]
	}

	return string(out)
}

// encodeString writes a Go string into the EEPROM using the layout
// documented on decodeString. The length byte is the USB string descriptor
// bLength (2 + 2 * char_count) so that the chip serves the full string
// to the USB host instead of truncating it.
func encodeString(img *Image, headerAddr, bodyAddr uint8, s string, maxBytes uint8) {
	if len(s) > int(maxBytes) {
		s = s[:maxBytes]
	}

	var header uint16

	if len(s) > 0 {
		header = uint16(s[0]) << 8
		header |= uint16((2 + 2*len(s)) & 0xFF)
	}

	img.SetWord(headerAddr, header)

	bodyOff := int(bodyAddr) * 2

	for i := 0; i < int(maxBytes); i++ {
		var b byte

		if i+1 < len(s) {
			b = s[i+1]
		}

		if bodyOff+i < len(img) {
			img[bodyOff+i] = b
		}
	}
}
