package eeprom

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"gopkg.in/yaml.v3"
)

// MarshalYAML returns the View as YAML bytes. Used by `openvlm dump
// --format yaml`. Round-trips bit-for-bit through UnmarshalPartial +
// ApplyOverrides on a base View matching the dumped one.
func (v *View) MarshalYAML() ([]byte, error) {
	out, err := yaml.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("eeprom: marshal yaml: %w", err)
	}

	return out, nil
}

// UnmarshalPartial parses YAML bytes into a PartialView. It rejects any
// write-locked key (`vid`, `pid`, `product-string`, `manufacturer-string`)
// and any key not present on PartialView (so a typo doesn't silently
// disappear).
func UnmarshalPartial(data []byte) (*PartialView, error) {
	if err := rejectLockedKeys(data); err != nil {
		return nil, err
	}

	var p PartialView

	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)

	if err := dec.Decode(&p); err != nil {
		if errors.Is(err, io.EOF) {
			// Empty document — return an empty partial, no overrides.
			return &p, nil
		}

		return nil, fmt.Errorf("eeprom: parse yaml overrides: %w", err)
	}

	return &p, nil
}

// rejectLockedKeys scans the top-level YAML keys and returns an error if
// any write-locked field (`vid`, `pid`, `product-string`,
// `manufacturer-string`) appears. Doing this at the structural level
// (rather than via `KnownFields(true)`) gives the user a fixed,
// recognizable message about the write-lock policy instead of a generic
// "field not found" surprise.
func rejectLockedKeys(data []byte) error {
	var n yaml.Node

	if err := yaml.Unmarshal(data, &n); err != nil {
		return fmt.Errorf("eeprom: parse yaml: %w", err)
	}

	root := &n
	if n.Kind == yaml.DocumentNode && len(n.Content) > 0 {
		root = n.Content[0]
	}

	if root.Kind != yaml.MappingNode {
		return nil
	}

	for i := 0; i+1 < len(root.Content); i += 2 {
		key := root.Content[i].Value

		switch key {
		case "vid", "pid":
			return fmt.Errorf("eeprom: %q is not user-programmable in this tool "+
				"(VID/PID are sourced from compiled-in OpenVLM constants)", key)
		case "product-string", "manufacturer-string":
			return fmt.Errorf("eeprom: %q is not user-programmable in this tool "+
				"(product/manufacturer strings are sourced from compiled-in OpenVLM defaults)", key)
		}
	}

	return nil
}
