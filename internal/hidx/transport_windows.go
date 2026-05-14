//go:build windows

package hidx

import (
	"errors"
	"fmt"
	"log"
	"os"
	"regexp"
	"strconv"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowsBackend talks to the Win32 HID stack directly via LazyDLL bindings.
// No CGO and no third-party DLLs — only hid.dll and setupapi.dll, both
// shipped by Windows itself.
//
// Enumeration walks the HID interface class GUID via the SetupDi* APIs.
// VID/PID come from parsing the device interface path string the HID class
// driver constructs from the USB device descriptor — opening the device is
// not required and is the path that previously broke enumeration in user
// contexts where CreateFile on the consumer-control collection was denied.
// String fields (serial / product / manufacturer) are still read by opening
// the device, but their failure is non-fatal: a DeviceInfo with empty strings
// is still returned so the caller can see the device exists.
type windowsBackend struct{}

func newBackend() Backend { return &windowsBackend{} }

//nolint:gochecknoglobals // LazyDLL handles are the canonical x/sys/windows pattern
var (
	hidDLL      = windows.NewLazySystemDLL("hid.dll")
	setupAPIDLL = windows.NewLazySystemDLL("setupapi.dll")

	procHidDGetHIDGuid            = hidDLL.NewProc("HidD_GetHidGuid")
	procHidDGetAttributes         = hidDLL.NewProc("HidD_GetAttributes")
	procHidDGetSerialNumberString = hidDLL.NewProc("HidD_GetSerialNumberString")
	procHidDGetProductString      = hidDLL.NewProc("HidD_GetProductString")
	procHidDGetManufacturerString = hidDLL.NewProc("HidD_GetManufacturerString")
	procHidDGetInputReport        = hidDLL.NewProc("HidD_GetInputReport")
	procHidDSetOutputReport       = hidDLL.NewProc("HidD_SetOutputReport")

	procSetupDiGetClassDevsW             = setupAPIDLL.NewProc("SetupDiGetClassDevsW")
	procSetupDiEnumDeviceInterfaces      = setupAPIDLL.NewProc("SetupDiEnumDeviceInterfaces")
	procSetupDiGetDeviceInterfaceDetailW = setupAPIDLL.NewProc("SetupDiGetDeviceInterfaceDetailW")
	procSetupDiDestroyDeviceInfoList     = setupAPIDLL.NewProc("SetupDiDestroyDeviceInfoList")
)

// hidPathVIDPID matches the vid_NNNN / pid_NNNN segments in a Windows HID
// device interface path like
// `\\?\HID#VID_0D8C&PID_0012&MI_03#7&abc&0&0000#{4d1e55b2-...}`.
// Case-insensitive because Windows SDK versions are inconsistent across
// `vid_` / `VID_` / `Vid_`.
//
//nolint:gochecknoglobals // package-level compiled regex per performance.md
var hidPathVIDPID = regexp.MustCompile(`(?i)vid_([0-9a-f]{4}).*pid_([0-9a-f]{4})`)

// SetupDi flags — values from SetupAPI.h.
const (
	digcfPresent         = 0x00000002
	digcfDeviceInterface = 0x00000010
)

// hiddAttributes mirrors HIDD_ATTRIBUTES from hidsdi.h.
type hiddAttributes struct {
	Size          uint32
	VendorID      uint16
	ProductID     uint16
	VersionNumber uint16
}

// spDeviceInterfaceData mirrors SP_DEVICE_INTERFACE_DATA from setupapi.h.
type spDeviceInterfaceData struct {
	CbSize             uint32
	InterfaceClassGuid windows.GUID
	Flags              uint32
	Reserved           uintptr
}

// parseVIDPIDFromPath extracts the USB VID and PID from a Windows HID device
// interface path. The path is constructed by the HID class driver from the
// USB device descriptor and is reliable without opening the device.
//
// Returns ok=false for paths that don't carry vid_/pid_ markers (e.g.
// Bluetooth HID paths, which use a service-UUID format).
func parseVIDPIDFromPath(path string) (vid, pid uint16, ok bool) {
	m := hidPathVIDPID.FindStringSubmatch(path)
	if len(m) != 3 {
		return 0, 0, false
	}

	v, err := strconv.ParseUint(m[1], 16, 16)
	if err != nil {
		return 0, 0, false
	}

	p, err := strconv.ParseUint(m[2], 16, 16)
	if err != nil {
		return 0, 0, false
	}

	return uint16(v), uint16(p), true
}

func (b *windowsBackend) Enumerate(vendorID, productID uint16) ([]DeviceInfo, error) {
	var hidGUID windows.GUID

	if _, _, err := procHidDGetHIDGuid.Call(uintptr(unsafe.Pointer(&hidGUID))); err != nil && !errors.Is(err, syscall.Errno(0)) {
		return nil, fmt.Errorf("hidx: HidD_GetHidGuid: %w", err)
	}

	devInfoSet, _, err := procSetupDiGetClassDevsW.Call(
		uintptr(unsafe.Pointer(&hidGUID)),
		0,
		0,
		uintptr(digcfPresent|digcfDeviceInterface),
	)
	if devInfoSet == ^uintptr(0) {
		return nil, fmt.Errorf("hidx: SetupDiGetClassDevs: %w", err)
	}

	defer procSetupDiDestroyDeviceInfoList.Call(devInfoSet) //nolint:errcheck // best-effort cleanup

	var dbg *log.Logger
	if os.Getenv("OPENVLM_DEBUG") != "" {
		dbg = log.New(os.Stderr, "openvlm: ", 0)
	}

	results := make([]DeviceInfo, 0, 4)

	var (
		index                            uint32
		ifd                              spDeviceInterfaceData
		enumErrors                       []error
		enumerated, matched, stringsRead int
	)

	ifd.CbSize = uint32(unsafe.Sizeof(ifd))

	for {
		ret, _, _ := procSetupDiEnumDeviceInterfaces.Call(
			devInfoSet,
			0,
			uintptr(unsafe.Pointer(&hidGUID)),
			uintptr(index),
			uintptr(unsafe.Pointer(&ifd)),
		)
		if ret == 0 {
			break
		}

		index++
		enumerated++

		path, perr := getDeviceInterfacePath(devInfoSet, &ifd)
		if perr != nil {
			enumErrors = append(enumErrors, fmt.Errorf("path at index %d: %w", index-1, perr))

			continue
		}

		// Primary identification: parse VID/PID from the device path. The
		// HID class driver constructs the path from the USB device
		// descriptor, so this works without ever opening the device — the
		// path that previously failed silently when CreateFile was denied.
		if pathVID, pathPID, ok := parseVIDPIDFromPath(path); ok {
			if vendorID != 0 && pathVID != vendorID {
				continue
			}

			if productID != 0 && pathPID != productID {
				continue
			}

			matched++

			info := DeviceInfo{
				Path:      path,
				VendorID:  pathVID,
				ProductID: pathPID,
			}

			// Best-effort: read string fields. The Backend contract
			// promises devices the caller can't fully query are still
			// returned, so a failure here only annotates enumErrors —
			// the DeviceInfo still goes into results.
			if got, serr := queryDeviceStrings(path); serr == nil {
				info.SerialNumber = got.SerialNumber
				info.ProductName = got.ProductName
				info.ManufacturerName = got.ManufacturerName
				stringsRead++
			} else {
				enumErrors = append(enumErrors, fmt.Errorf("read strings %s: %w", path, serr))
			}

			results = append(results, info)

			continue
		}

		// Fallback for non-USB HID paths (Bluetooth, virtual HID, etc.):
		// open the device and use HidD_GetAttributes for VID/PID.
		info, qerr := queryDevice(path)
		if qerr != nil {
			enumErrors = append(enumErrors, fmt.Errorf("query %s: %w", path, qerr))

			continue
		}

		if vendorID != 0 && info.VendorID != vendorID {
			continue
		}

		if productID != 0 && info.ProductID != productID {
			continue
		}

		info.Path = path
		matched++
		stringsRead++
		results = append(results, info)
	}

	if dbg != nil {
		dbg.Printf("enumerated %d HID interface paths, matched %d by VID/PID, read strings for %d",
			enumerated, matched, stringsRead)
	}

	if len(results) == 0 && len(enumErrors) > 0 {
		return nil, fmt.Errorf("hidx: no devices found; %d enumeration error(s): %w",
			len(enumErrors), errors.Join(enumErrors...))
	}

	return results, nil
}

func (b *windowsBackend) Open(path string) (Transport, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, fmt.Errorf("hidx: encode path %q: %w", path, err)
	}

	handle, err := windows.CreateFile(
		pathPtr,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return nil, fmt.Errorf("hidx: CreateFile %s: %w", path, err)
	}

	return &windowsDevice{handle: handle, path: path}, nil
}

type windowsDevice struct {
	handle windows.Handle
	path   string
}

func (d *windowsDevice) GetInputReport(reportID byte, buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, errors.New("hidx: GetInputReport called with empty buffer")
	}

	buf[0] = reportID

	ret, _, err := procHidDGetInputReport.Call(
		uintptr(d.handle),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if ret == 0 {
		return 0, fmt.Errorf("hidx: HidD_GetInputReport %s: %w", d.path, err)
	}

	return len(buf), nil
}

func (d *windowsDevice) SetOutputReport(reportID byte, buf []byte) (int, error) {
	if len(buf) == 0 {
		return 0, errors.New("hidx: SetOutputReport called with empty buffer")
	}

	buf[0] = reportID

	ret, _, err := procHidDSetOutputReport.Call(
		uintptr(d.handle),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(len(buf)),
	)
	if ret == 0 {
		return 0, fmt.Errorf("hidx: HidD_SetOutputReport %s: %w", d.path, err)
	}

	return len(buf), nil
}

func (d *windowsDevice) Close() error {
	if d.handle == windows.InvalidHandle || d.handle == 0 {
		return nil
	}

	err := windows.CloseHandle(d.handle)
	d.handle = windows.InvalidHandle

	return err //nolint:wrapcheck // CloseHandle errors are passed through
}

// getDeviceInterfacePath calls SetupDiGetDeviceInterfaceDetail twice — first
// to learn the required buffer size, then to read the actual UTF-16 device
// path string.
func getDeviceInterfacePath(devInfoSet uintptr, ifd *spDeviceInterfaceData) (string, error) {
	var requiredSize uint32

	procSetupDiGetDeviceInterfaceDetailW.Call(
		devInfoSet,
		uintptr(unsafe.Pointer(ifd)),
		0,
		0,
		uintptr(unsafe.Pointer(&requiredSize)),
		0,
	)

	if requiredSize == 0 {
		return "", errors.New("hidx: SetupDiGetDeviceInterfaceDetail returned zero size")
	}

	// SP_DEVICE_INTERFACE_DETAIL_DATA_W layout:
	//   DWORD cbSize;                       // offset 0, 4 bytes
	//   WCHAR DevicePath[ANYSIZE_ARRAY];    // offset 4, variable length
	//
	// sizeof(struct) on amd64 is 8 (cbSize 4 + first WCHAR 2 + 2 trailing
	// alignment bytes). The API requires cbSize == sizeof(struct), but the
	// trailing alignment sits AFTER the first WCHAR — DevicePath itself
	// still starts at byte offset 4. goreleaser only builds windows/amd64.
	const (
		cbSize     = 8 // sizeof(SP_DEVICE_INTERFACE_DETAIL_DATA_W) on amd64
		pathOffset = 4 // byte offset of DevicePath member
	)

	buf := make([]byte, requiredSize)
	*(*uint32)(unsafe.Pointer(&buf[0])) = cbSize

	ret, _, err := procSetupDiGetDeviceInterfaceDetailW.Call(
		devInfoSet,
		uintptr(unsafe.Pointer(ifd)),
		uintptr(unsafe.Pointer(&buf[0])),
		uintptr(requiredSize),
		0,
		0,
	)
	if ret == 0 {
		return "", fmt.Errorf("hidx: SetupDiGetDeviceInterfaceDetail: %w", err)
	}

	return decodeDetailPath(buf[pathOffset:]), nil
}

// decodeDetailPath converts the WCHAR DevicePath bytes returned by
// SetupDiGetDeviceInterfaceDetailW into a Go string. Split out from
// getDeviceInterfacePath so the buffer-layout assumption is unit-testable
// without going through the SetupAPI.
func decodeDetailPath(pathBytes []byte) string {
	if len(pathBytes) == 0 {
		return ""
	}

	pathU16 := unsafe.Slice((*uint16)(unsafe.Pointer(&pathBytes[0])), len(pathBytes)/2)

	return windows.UTF16ToString(pathU16)
}

// queryDevice opens the HID device just long enough to read its attributes
// and identifying strings. Closes its own handle before returning. Used as
// the fallback for non-USB HID paths (Bluetooth, virtual HID, etc.) where
// the path itself doesn't encode VID/PID.
func queryDevice(path string) (DeviceInfo, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return DeviceInfo{}, err //nolint:wrapcheck // direct pass-through of UTF16 error
	}

	handle, err := windows.CreateFile(
		pathPtr,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return DeviceInfo{}, err //nolint:wrapcheck // surfaced verbatim to caller
	}

	defer windows.CloseHandle(handle) //nolint:errcheck // best-effort cleanup

	var attrs hiddAttributes

	attrs.Size = uint32(unsafe.Sizeof(attrs))

	if ret, _, _ := procHidDGetAttributes.Call(uintptr(handle), uintptr(unsafe.Pointer(&attrs))); ret == 0 {
		return DeviceInfo{}, errors.New("hidx: HidD_GetAttributes failed")
	}

	return DeviceInfo{
		VendorID:         attrs.VendorID,
		ProductID:        attrs.ProductID,
		SerialNumber:     readHidString(handle, procHidDGetSerialNumberString),
		ProductName:      readHidString(handle, procHidDGetProductString),
		ManufacturerName: readHidString(handle, procHidDGetManufacturerString),
	}, nil
}

// queryDeviceStrings opens the HID device just long enough to read its
// serial / product / manufacturer strings. Called when VID/PID have already
// been parsed from the device path, so HidD_GetAttributes is not needed.
// Failure here is non-fatal at the caller — strings are best-effort.
func queryDeviceStrings(path string) (DeviceInfo, error) {
	pathPtr, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return DeviceInfo{}, err //nolint:wrapcheck // direct pass-through of UTF16 error
	}

	handle, err := windows.CreateFile(
		pathPtr,
		0,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE,
		nil,
		windows.OPEN_EXISTING,
		0,
		0,
	)
	if err != nil {
		return DeviceInfo{}, fmt.Errorf("CreateFile: %w", err)
	}

	defer windows.CloseHandle(handle) //nolint:errcheck // best-effort cleanup

	return DeviceInfo{
		SerialNumber:     readHidString(handle, procHidDGetSerialNumberString),
		ProductName:      readHidString(handle, procHidDGetProductString),
		ManufacturerName: readHidString(handle, procHidDGetManufacturerString),
	}, nil
}

func readHidString(handle windows.Handle, proc *windows.LazyProc) string {
	const max = 256

	buf := make([]uint16, max)

	ret, _, _ := proc.Call(uintptr(handle), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2))
	if ret == 0 {
		return ""
	}

	return strings.TrimSpace(windows.UTF16ToString(buf))
}
