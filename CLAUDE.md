# CLAUDE.md

Project-specific guidance for Claude Code working in this repository. Read this before making changes.

## What this project is

`openvlm` is a cross-platform CLI for reading, writing, and validating the EEPROM on **OpenVLM USB audio devices**. The hardware is a **C-Media CM108B** USB audio chip wired to a 93C46 SPI EEPROM, with a GPIO1 hardware strap that distinguishes OpenVLM-branded devices from generic CM108-family devices.

The CLI talks to the chip exclusively over USB-HID class control transfers (`Get_Input_Report` / `Set_Output_Report`) — there is no kernel driver, no vendor blob, and no platform-specific cable. The CM108B datasheet §7.4 documents the HID protocol; §7.1.3 documents the EEPROM word layout.

## Hardware context that shapes the code

These facts are not in the datasheet but are required to read the code correctly:

- **Stock chips ship with a blank EEPROM.** The strings `C-Media Electronics Inc.` / `USB Audio Device` come from the chip's internal ROM, not from EEPROM bytes. There is no "factory image" to read off a virgin device.
- **VID/PID are write-locked in the CLI.** Both are sourced from compiled-in constants (`cm108.OpenVLMVendorID = 0x0D8C`, `cm108.OpenVLMProductID = 0x0012`). Programming a different VID/PID would prevent this CLI from finding the device, so YAML, `update`, and per-field flags all refuse to set them.
- **Product/manufacturer strings are also write-locked** to the compiled-in `OpenVLMDefaults` (`OpenVLM` / `BuildsByShane`). Same reason — these are identity, not configuration.
- **GPIO1 strap is the OpenVLM identity probe.** A generic CM108-family device has GPIO1 floating low; an OpenVLM device pulls it high. `openvlm identify` and the `requireOpenVLM` gate on every write verb check this. `--force` bypasses for bench / bootstrap work.
- **macOS IOKit returns transient `kIOReturnError` (0xE00002BC)** under host-controller pressure during back-to-back HID reports. The protocol layer absorbs this with bounded retries; do not remove the `transferRetries` / `verifyRetries` machinery without a replacement.

## Datasheet pinning issues (open)

Two load-bearing assumptions in the codec are unverified against real hardware and still need a one-time bench gate:

1. **`EEPROM_CTRL` bit layout.** Datasheet §7.4 names the register but never documents `op<<6 | addr` — that's the convention we copied from Linux `hid-cm108`.
2. **String body byte order in body words.** Datasheet specifies the header word's byte order, not the body words.

A third assumption — init-volume encoding — was bench-confirmed 2026-04-30: the fields are attenuation-encoded (`bits = maxDB - value`, bits 0 = loudest). Two's complement and offset-binary were both tried on hardware and rejected; see the comment above the word-0x2A constants in [internal/eeprom/layout.go](internal/eeprom/layout.go).

If you touch string parsing, flag any byte-order ambiguity as a bench question rather than papering over it with assumptions.

## Architecture

```
cmd/                 — Cobra subcommands (one file per verb)
internal/cm108/      — OpenVLM-specific knowledge: VID/PID, GPIO1 probe, device selection
internal/eeprom/     — Datasheet §7.1.3 codec, validator, HID protocol helpers
internal/hidx/       — Cross-platform HID transport seam (linux/darwin/windows + fake)
```

Layering rule: `cmd` → `eeprom` + `cm108` → `hidx`. `hidx` knows nothing about CM108B or EEPROMs; `eeprom` knows nothing about USB enumeration or the GPIO1 strap; `cm108` is the bridge between the two.

### Subcommands ([cmd/](cmd/))

| Verb | Purpose |
|------|---------|
| `list` | enumerate every CM108-VID/PID device, report GPIO1 strap state |
| `identify` | exit 0 if selected device's GPIO1 reads high, exit 3 otherwise |
| `read` | dump 128-byte raw EEPROM image to file or stdout |
| `dump` | decode EEPROM, render as YAML / text / hex |
| `write` | program a 128-byte raw image or a YAML overrides file |
| `update` | read-modify-write a single field |
| `provision` | write `OpenVLMDefaults` with optional YAML or per-field flag overrides |
| `wipe` | erase EEPROM to all-`0xFF` (or `0x00`) — destructive, requires `--yes` |

Every write verb honors the GPIO1 safety gate; `--force` is the documented bypass.

### HID transport ([internal/hidx/](internal/hidx/))

`Transport` is single-threaded per device handle. `Backend` enumerates and opens.

- `transport_linux.go` — pure Go, hidraw ioctls (`HIDIOCGINPUT` / `HIDIOCSOUTPUT`) via `golang.org/x/sys/unix`. Walks `/sys/bus/usb/devices` over an `fs.FS` (testable with `fstest.MapFS`).
- `transport_darwin.go` — CGO via `github.com/sstallion/go-hid` (hidapi → IOKit). The **only** file in the repo that requires CGO. `make build` on macOS sets `CGO_ENABLED=1` automatically; Linux/Windows builds are pure Go and produce static binaries.
- `transport_windows.go` — pure Go via `setupapi.dll` / `hid.dll`.
- `fake.go` — in-memory backend used by every test in the repo. Implements the CM108B HID protocol closely enough that production code paths are exercised identically in tests as on hardware.

### EEPROM codec ([internal/eeprom/](internal/eeprom/))

- `layout.go` — datasheet §7.1.3 word addresses + bit positions.
- `image.go` — raw 128-byte image, `Encode` / `Decode` between `Image` and `View`.
- `view.go` — typed user-facing struct (`View`) plus `PartialView` for layered overrides.
- `defaults.go` — `OpenVLMDefaults`, the canonical factory-programmed image.
- `validate.go` — every per-field range and cross-field constraint. Single gate before any HID transfer.
- `merge.go` — `ApplyOverrides(base, partial)` for the layered override stack.
- `update.go` — string-typed field parser used by `openvlm update`.
- `yaml.go` — YAML round-trip + write-locked-key rejection at the structural level.
- `protocol.go` — `ReadWord` / `WriteWord` / `ReadAll` / `WriteAll` / `WipeAll` over `hidx.Transport`. Owns the macOS retry machinery.

## Build / test / lint

```bash
make build          # builds bin/openvlm; CGO auto-detected per OS
make test           # go test ./... + coverage
make test-race      # CGO_ENABLED=1, -race, 120s timeout
make lint           # golangci-lint --fix --timeout 5m
make run ARGS="list"
```

Cross-compile linux from macOS:

```bash
GOOS=linux CGO_ENABLED=0 make build
```

## Conventions Claude must follow

Project-level rules live in [.claude/rules/](.claude/rules/) and are authoritative:

- [.claude/rules/idiomatic-go.md](.claude/rules/idiomatic-go.md) — naming, error handling, control flow, struct/interface design, package design.
- [.claude/rules/testing.md](.claude/rules/testing.md) — testify split (`require`/`assert`), hand-rolled fakes (no mock frameworks), table-driven tests, no `time.Sleep` in tests.
- [.claude/rules/concurrency.md](.claude/rules/concurrency.md) — mutex naming (`mu`), goroutine shutdown paths, no plain map sharing.
- [.claude/rules/performance.md](.claude/rules/performance.md) — preallocate slices/maps, compile regex once, bounded caches.

Project-specific additions:

- **Terminology: never use "dongle" in user-facing text or doc comments.** Refer to the hardware as an "OpenVLM device" (or just "device"). Applies to CLI strings, help text, error messages, docs, and Go-doc comments. Internal symbol names (`OpenVLM` etc.) are unaffected.
- **VID/PID and product/manufacturer strings are write-locked.** Do not add CLI surface to set them — every input layer (YAML, per-field flags, `update`) must reject them with a fixed error message.
- **`WriteImage` enforces the VID/PID guard.** `WipeAll` is the only documented bypass and exists exclusively for the wipe verb.
- **Decimal only for numeric input.** `update`, per-field flags, and YAML reject `0x` / `0b` / `0o` prefixes with `ErrHexInput`.
- **Tail words 0x33..0x3F are preserved across read-modify-write.** `update.go` extracts them from the live image before re-encoding so undocumented factory data isn't clobbered.
- **No interactive prompts.** Destructive verbs (`wipe`) gate on flags (`--yes`), not TTY prompts. Keeps the CLI scriptable.
- **`SetBackend` is the test seam.** Tests swap in `hidx.NewFakeBackend()` via `cmd.SetBackend`. Production code never calls it.
- **Cobra flag globals persist across `rootCmd.Execute()` calls within one process.** Tests must call `resetOverrides()` and `resetWipeFlags()` between invocations.

## Test patterns specific to this repo

- **`hidx.FakeBackend` is the spine.** Every package's tests use it. `RegisterDevice(info, eeprom, gpio1High)` returns a `FakeDeviceState` handle so tests can inspect / mutate the simulated chip's state.
- **Test doubles wrap a real `hidx.Transport`** when they need to inject failures on specific addresses (`readBackLiar`, `flakyVerifyTransport`, `orderTracker` in `protocol_extra_test.go`). Pass-through methods need `//nolint:wrapcheck` annotations.
- **Encoder→decoder round-trips are tautological.** Real correctness tests pin against documented bit patterns (see `bitfield_test.go` Phase A2/A4) or against captured real-device bytes (Phase A1, blocked on bench gate).

## Memory bug class to watch for

Init-volume validator + encoder pairing. The encoder is attenuation-encoded (`bits = maxDB - value`, bench-confirmed 2026-04-30) on bit fields of widths 7/6/5, and the validator accepts exactly the datasheet §8.3 ranges, which that encoding represents faithfully. If either side changes, both must move together — decoupling them silently corrupts on hardware. The `maxDB` anchors live in [internal/eeprom/layout.go](internal/eeprom/layout.go) (`dac/adc/aaInitMaxDB`); the accepted ranges live in [internal/eeprom/validate.go](internal/eeprom/validate.go) (`dac/adc/aaInitMin/Max`).

## Memory and persistence

- Plans live in [.claude/plans/](.claude/plans/). Read the relevant plan before implementing — they capture context and tradeoffs that aren't in the code.
- The user's auto-memory system records preferences and project state across sessions. Notable durable rule: **never make git commits**; stage and propose the message, the user runs `git commit` themselves.

## When in doubt

- Datasheet ambiguity → flag it as a Phase G bench question in the relevant plan, do not paper over with assumptions.
- Cross-platform HID quirk → the macOS path almost always needs a retry / pacing knob; Linux and Windows are usually fine without.
- New EEPROM field → update `View`, `PartialView`, `Validate`, `ApplyOverrides`, `ApplyUpdate`, `Encode`, `Decode`, `defaults.go`, the per-field flag in `cmd/flags.go`, and add a row to `TestApplyUpdate_EveryField`. Missing any one of those will be caught by an existing test.
