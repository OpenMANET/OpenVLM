# Human-friendly CLI output

## Context

The `openvlm` CLI currently exposes ~80 user-visible strings (help text, errors, success messages, warnings) scattered across 14 files with no central messaging layer. Audit results: jargon clusters in five domains (hardware/chip, protocol/datasheet, USB, audio, implementation leakage). The user wants every routine touchpoint to be readable by someone who does not own the CM108B datasheet — without insulting the technical user who does.

This plan covers errors, success messages, warnings, and `--help` text. Out of scope: `dump --format hex`/`yaml` raw output (those are deliberately mechanical), debug-level HID logging, internal sentinel error strings used only for tests/debugging.

## Design decisions

### 1. Audience floor: friendly + accurate, not dumbed down

Lead every routine message with plain English. When a hardware term is genuinely the precise word (EEPROM, dongle), keep it; when it's lab jargon (GPIO1 strap, hex address, magic word, register IR0[7:6]), translate it for default output and surface the technical detail behind `--verbose`.

### 2. Centralize wording

Introduce **`cmd/messages.go`** containing:

- All `Use` / `Short` / `Long` strings as constants (one block per verb, named `useList`, `shortList`, `longList`, etc.).
- Format functions returning the final printable string for every routine message: `msgConfirmed(serial)`, `msgWritten(path, n)`, `msgUpdated(serial, field)`, `msgWiped(serial, words, pattern)`, `msgDryRun(n)`, `msgChipBlank(vid, pid)`, `msgReadComplete(n, src, dest)`, `msgForceWarning(serial)`, …
- One translator: **`friendlyError(err error, verbose bool) string`**. Recognizes known sentinels via `errors.Is` / `errors.As` and substitutes plain-English text. Falls through to `err.Error()` for unknown errors so nothing is silently swallowed.

Why centralize: today every verb calls `fmt.Print*` inline, so changes to tone require touching every file and lock-in tests are scattered. A single file makes future tone tweaks cheap, makes localization possible later, and lets `cmd/messages_test.go` pin every string with table-driven tests.

### 3. Repurpose the existing `--verbose` / `-v` persistent flag

The flag is already declared in `cmd/root.go:68` (described as "log each HID transfer for diagnostics") but nothing wires it through. Repurpose it: `--verbose` = "show technical detail in user-facing messages" (hex addresses, register names, GPIO state, byte indices). HID transfer logging stays a future feature; if we want it later we'll add `--debug-hid`. Update the flag's help text accordingly.

### 4. Don't rewrite internal error strings

Errors raised inside `internal/eeprom/*` and `internal/cm108/*` keep their current wording — they're useful when reading test failures or debugging hardware issues, and they're already prefixed (`eeprom:`, `cm108:`) for grep-ability. The cmd layer is the translation boundary; `friendlyError` runs once, just before printing in `Execute()`.

### 5. Keep field names, YAML keys, and CLI flags identical

`dac-init-volume`, `boost-mode`, `--force`, `--yes`, `--input`, etc. — these are interface, not narrative. Renaming them would break scripts and docs.

## The pattern

### `cmd/messages.go` shape

```go
package cmd

import (
    "errors"
    "fmt"

    "github.com/openmanet/openvlm/internal/cm108"
    "github.com/openmanet/openvlm/internal/eeprom"
)

// ---- help text ----------------------------------------------------------

const (
    useList   = "list"
    shortList = "Show every OpenVLM dongle plugged into this computer"
    longList  = `Lists every USB audio dongle that looks like an OpenVLM …`
    // … one block per verb …
)

// ---- success / progress messages ---------------------------------------

func msgConfirmed(serial string) string {
    return fmt.Sprintf("%s: this is an OpenVLM dongle.", serial)
}

func msgWritten(serial string, n int) string {
    return fmt.Sprintf("%s: wrote and verified %d bytes.", serial, n)
}

// … etc …

// ---- error translation -------------------------------------------------

func friendlyError(err error, verbose bool) string {
    switch {
    case errors.Is(err, cm108.ErrNoDevice):
        return "No OpenVLM dongles are plugged in. Connect one and try again."
    case errors.Is(err, cm108.ErrSerialNotFound):
        return fmt.Sprintf(
            "No OpenVLM dongle has the serial number you asked for.\n" +
            "Run 'openvlm list' to see what's connected.")
    case errors.Is(err, cm108.ErrAmbiguousDevice):
        return "More than one OpenVLM dongle is plugged in.\n" +
            "Use --serial <serial> to pick which one."
    case errors.Is(err, eeprom.ErrFieldLocked):
        return "That field is built into the dongle's identity and can't be changed by this tool."
    case errors.Is(err, eeprom.ErrHexInput):
        return "Numbers must be in regular decimal form. (No 0x or 0b prefixes.)"
    case errors.Is(err, eeprom.ErrVerifyMismatch):
        return "The dongle accepted the write but read back something different.\n" +
            "Unplug it, plug it back in, and try once more."
    // … etc …
    default:
        if verbose {
            return err.Error()
        }
        // strip noisy "openvlm:" / "eeprom:" / "cm108:" prefixes for default output
        return stripPrefixes(err.Error())
    }
}
```

Verbs change from `fmt.Println("Wrote 128 bytes (verified)")` to `cmd.Println(msgWritten(serial, n))`. Errors continue to bubble; `Execute()` calls `friendlyError(err, flagVerbose)` before printing.

### Tone before/after, illustrative

| Today | After |
|---|---|
| `openvlm: device at /dev/hidraw3: GPIO1 strap reads low; pass --force to override` | `This doesn't look like an OpenVLM dongle (its identity bit isn't set).`<br>`Pass --force to program it anyway.` |
| `openvlm: warning: GPIO1 strap not confirmed; --force in effect, proceeding anyway` | `Warning: identity bit not set; continuing because --force was passed.` |
| `openvlm: 0123ABCD: NOT confirmed` (identify) | `0123ABCD: this doesn't look like an OpenVLM dongle.` |
| `openvlm: 0123ABCD: confirmed OpenVLM` | `0123ABCD: confirmed — this is an OpenVLM dongle.` |
| `openvlm: 0123ABCD: wiped all 64 words to 0xFFFF (re-enumerate to apply)` | `0123ABCD: erased. Unplug and plug back in for the change to take effect.` |
| `openvlm: 0123ABCD: wrote 128 bytes (verified)` | `0123ABCD: wrote and verified 128 bytes.` |
| `openvlm: warning: image magic word missing — chip looks unprogrammed (read VID:PID 0x0D8C:0x0012)` | `Warning: this dongle's memory looks blank or corrupted.` (verbose adds the hex VID:PID) |
| `openvlm: eeprom: dac-init-volume: value 50 out of range [-127, 31]` | `Can't set dac-init-volume to 50 — it has to be between -127 and 31.` |
| `openvlm: eeprom: hex/binary/octal numeric input is not accepted; use decimal` | `Numbers must be in regular decimal form. (No 0x or 0b prefixes.)` |

### Help-text shape (every verb)

```
Use:   <verb> [args]
Short: <one-sentence: what does this do?>
Long:  <one paragraph: what + when to use>
       <one example>
       <gotchas, if any>
```

No more datasheet section references in default `Long` text. The technical detail moves into either `--verbose` runtime output or a standalone `docs/` page (not in scope here).

## File-by-file changes

| File | Change |
|---|---|
| `cmd/messages.go` (new) | All wording. ~250–300 lines. Structured: help-text constants, success/progress formatters, `friendlyError`. |
| `cmd/messages_test.go` (new) | Pin every formatter's output against a literal expected string (table-driven). Test `friendlyError` against every sentinel error from `cm108` and `eeprom`. |
| `cmd/root.go` | `Long` → `longRoot` const. Update `--verbose` help text. Route stderr error through `friendlyError(err, flagVerbose)`. Drop "openvlm:" prefix when `friendlyError` already produces a complete sentence (still keep it when falling through to raw `err.Error()`). |
| `cmd/list.go` | `Short`/`Long` constants. Replace "no OpenVLM devices found" stdout/stderr split with single friendly message via formatter. Adjust table headers to title case (`Serial`, `Device`, `OpenVLM?`, `Notes`). |
| `cmd/identify.go` | `Short`/`Long` constants. Success/failure lines via `msgConfirmed`/`msgNotConfirmed`. |
| `cmd/read.go` | `Short`/`Long` constants. Use `msgReadComplete`. |
| `cmd/dump.go` | `Short`/`Long` constants. Replace "magic word missing" warning with `msgChipBlank`. Pretty-up table column labels. Verbose mode keeps the hex VID:PID + datasheet word references. |
| `cmd/write.go` | `Short`/`Long` constants. Replace "input is %d bytes, expected exactly %d for a raw image, and does not look like YAML" with `msgWriteInputUnknown(n)`. Replace `msgWritten`. |
| `cmd/update.go` | `Short`/`Long` constants. `msgUpdated`. Field-name listing already friendly; leave it. |
| `cmd/provision.go` | `Short`/`Long` constants. `msgProvisioned`, `msgDryRun`. |
| `cmd/wipe.go` | `Short`/`Long` constants. `msgWiped` (drops "0xFFFF" / "words" jargon). Confirmation-flag error message via formatter. |
| `cmd/device.go` | Move "GPIO1 strap not confirmed" warning + "device at %s: ..." to formatters (`msgForceWarning`, error wrapped through `friendlyError`). |
| `cmd/exit.go` | No structural changes; constants and types stay. |
| `cmd/integration_test.go` | Update expected output strings to match new wording. Add a test that exercises `--verbose` and confirms it surfaces the technical detail. |

Internal packages (`internal/eeprom/*`, `internal/cm108/*`) — **no changes**. Their error strings remain test-friendly.

## Testing

Per `.claude/rules/testing.md`:

- `cmd/messages_test.go`: table-driven, one row per formatter and one row per sentinel error → friendly translation. Pin exact strings.
- Update `cmd/integration_test.go` to assert the new wording on stdout/stderr from each verb against a `hidx.FakeBackend`. Add cases for:
  - `identify` succeeded vs. failed
  - `wipe --yes` success line
  - `provision --dry-run` line
  - `write` happy path
  - error paths: no devices, ambiguous devices, locked field, hex input, verify mismatch, force warning
- Add a `--verbose` integration test: same error path with and without `-v`, assert the verbose form contains hex/register detail and the default form does not.
- Run `make test-race` to confirm no concurrency regressions (none expected; this is wording-only).
- Run `make lint` to keep golangci-lint clean (the pinned strings will pass `goconst` because each is unique).

## Verification

1. `make test` — all existing + new tests pass.
2. Build and run each verb against a fake-backed device locally; eyeball the output for any awkward phrasing missed in tests.
3. Run `openvlm --help` and `openvlm <verb> --help` for every verb; confirm Long text reads cleanly without datasheet references.
4. Run a deliberate failure path (e.g. `openvlm update --field dac-init-volume=99`) and confirm the error reads as a sentence, not a stack of `eeprom: validate: out of range`-style prefixes.
5. Re-run with `-v` and confirm the technical detail returns.

## Out of scope (mention so they aren't forgotten)

- Localization / i18n — single-file string organization makes this straightforward later, but no `golang.org/x/text/message` integration in this PR.
- A standalone glossary subcommand (`openvlm help glossary`) — overkill for current scope.
- Rewriting `dump --format yaml`/`hex` output — that's data, not narrative.
- Wiring `--debug-hid` for HID transfer logging — separate change.
- Any change to YAML keys, CLI flags, or field names — interface stability.

## Critical files

- [cmd/messages.go](cmd/messages.go) (new) — all user-facing wording lives here.
- [cmd/messages_test.go](cmd/messages_test.go) (new) — pins every string.
- [cmd/root.go](cmd/root.go) — error translator wiring + `--verbose` help update.
- [cmd/integration_test.go](cmd/integration_test.go) — output assertions updated.
- [cmd/device.go](cmd/device.go) — `requireOpenVLM` warning/error rewording.
- [cmd/exit.go](cmd/exit.go) — read-only context; structure unchanged.
