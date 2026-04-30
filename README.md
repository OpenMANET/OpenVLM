# OpenVLM

Cross-platform CLI for reading, writing, and validating the EEPROM on **OpenVLM USB audio dongles**.

OpenVLM dongles are based on the **C-Media CM108B** USB audio chip wired to a 93C46 SPI EEPROM, with a GPIO1 hardware strap that distinguishes OpenVLM-branded hardware from generic CM108-family devices. The `openvlm` CLI talks to the chip exclusively over USB-HID class control transfers — no kernel driver, no vendor blob, no platform-specific cable. The same binary runs on Linux, macOS, and Windows.

## Install

Prebuilt binaries for Linux (amd64 / arm64), macOS (universal), and Windows (amd64) are published on the [Releases page](https://github.com/openmanet/openvlm/releases).

To build from source (requires Go 1.26+):

```bash
make build           # produces bin/openvlm
```

## Quick start

```bash
openvlm list                                  # find attached dongles
openvlm identify                              # confirm GPIO1 strap
openvlm provision --serial "00001234"         # write OpenVLM defaults
openvlm dump --format yaml                    # inspect live EEPROM
openvlm update dac-init-volume -6             # tweak one field
```

See the [CLI reference](docs/cli-reference.md) for every subcommand, flag, field, exit code, and workflow.

## Documentation

- **[docs/cli-reference.md](docs/cli-reference.md)** — complete user guide: installation, platform setup, every subcommand, the EEPROM field schema, YAML format, common workflows, troubleshooting.

## Build / test / lint

```bash
make build           # builds bin/openvlm; CGO auto-detected per OS
make test            # go test ./... + coverage
make test-race       # CGO_ENABLED=1, -race, 120s timeout
make lint            # golangci-lint --fix --timeout 5m
make run ARGS="list"
```

Cross-compile from macOS to Linux:

```bash
GOOS=linux CGO_ENABLED=0 make build
```

## License

[GNU General Public License v3.0](LICENSE).

## Contributing

Project conventions, hardware context, and architectural rationale live in [CLAUDE.md](CLAUDE.md). Read it before submitting changes.
