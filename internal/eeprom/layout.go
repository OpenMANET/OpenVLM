// Package eeprom owns everything about the CM108B's 93C46 EEPROM image:
// the on-chip word layout (per CM108B datasheet §7.1.3), the typed View
// struct that every user-facing surface (YAML, CLI flags, `update <field>`)
// shares, the validator that catches bad values before any HID transfer,
// and the protocol helpers that drive ReadWord / WriteWord against a
// hidx.Transport.
//
// The package is platform-neutral; per-OS HID code lives in internal/hidx.
package eeprom

// 93C46 EEPROM geometry. The chip is 64 × 16-bit words, addressed 0..63 and
// stored on the wire as 128 bytes.
const (
	WordCount = 64
	ByteCount = WordCount * 2
)

// Word addresses, mirroring the layout table in CM108B datasheet §7.1.3.
// Used by the codec; never exposed to the user.
const (
	addrFlags         uint8 = 0x00
	addrVID           uint8 = 0x01
	addrPID           uint8 = 0x02
	addrSerialHeader  uint8 = 0x03 // header word: high byte = first char, low byte = total length
	addrSerialBody    uint8 = 0x04 // 6 words = 12 bytes string body
	addrProductHeader uint8 = 0x0A
	addrProductBody   uint8 = 0x0B // 15 words = 30 bytes
	addrMfrHeader     uint8 = 0x1A
	addrMfrBody       uint8 = 0x1B // 15 words = 30 bytes
	addrVolumeInit    uint8 = 0x2A
	addrAnalogConfig  uint8 = 0x2B
	addrDACMin        uint8 = 0x2C
	addrDACMax        uint8 = 0x2D
	addrADCMin        uint8 = 0x2E
	addrADCMax        uint8 = 0x2F
	addrAAMin         uint8 = 0x30
	addrAAMax         uint8 = 0x31
	addrEEOption2     uint8 = 0x32
)

// String body sizes in bytes. Datasheet:
//
//   - serial: 12 bytes (after the 1-byte length in the header word)
//   - product / manufacturer: 30 bytes (after the 1-byte length)
const (
	maxSerialBytes  = 12
	maxProductBytes = 30
	maxMfrBytes     = 30
)

// Magic word: word 0x00 bits[15:4] must equal 0x670 for the chip to honor
// the EEPROM contents. Bits[2] and bits[0] are reserved-must-be-1.
const (
	magicNibble        uint16 = 0x670
	magicReservedBit2  uint16 = 1 << 2
	magicReservedBit0  uint16 = 1 << 0
	magicMask          uint16 = 0xFFF0
	magicValue         uint16 = magicNibble << 4
	flagBitExtFields   uint16 = 1 << 3
	flagBitSerialEna   uint16 = 1 << 1
	flagReservedAlways        = magicReservedBit2 | magicReservedBit0
)

// Word 0x2A bit ranges.
const (
	dacInitShift   = 9
	dacInitWidth   = 7
	adcInitShift   = 3
	adcInitWidth   = 6
	dacMaxMinValid = 1 << 2
	adcMaxMinValid = 1 << 1
	aaMaxMinValid  = 1 << 0
)

// Word 0x2B bit positions.
const (
	aaInitShift          = 11
	aaInitWidth          = 5
	bitBoostMode12dB     = 1 << 9 // 1 = 12dB, 0 = 22dB (datasheet table)
	bitDACShutdown       = 1 << 8
	bitTotalPowerControl = 1 << 7
	bitMicHighPassFilter = 1 << 5
	bitMicPLLAdjust      = 1 << 4
	bitMicBoost          = 1 << 3
	bitDACOutputHeadset  = 1 << 2
	bitHIDEnable         = 1 << 1
	bitRemoteWakeup      = 1 << 0

	// Bits that must be zero in word 0x2B per datasheet "reserved, should be 0"
	// rows. Used for round-trip integrity assertions.
	mask2BReservedZero uint16 = (1 << 10) | (1 << 6)
)
