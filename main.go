// Command openvlm is the cross-platform CLI for reading, writing, and
// validating the EEPROM on OpenVLM USB audio dongles (C-Media CM108B with a
// GPIO1 hardware strap).
//
// See `openvlm --help` for the full subcommand reference.
package main

import "github.com/openmanet/openvlm/cmd"

func main() {
	cmd.Execute()
}
