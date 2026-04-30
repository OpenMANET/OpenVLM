//go:build windows

package hidx

import (
	"errors"
	"fmt"
	"strings"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

// windowsBackend talks to the Win32 HID stack directly via LazyDLL bindings.
// No CGO and no third-party DLLs — only hid.dll and setupapi.dll, both
// shipped by Windows itself.
//
// Enumeration uses the SetupDi* APIs to walk the HID interface class GUID,
// then HidD_GetAttributes / HidD_GetSerialNumberString / HidD_GetProductString
// to populate DeviceInfo. Open issues CreateFile against the device path,
// and the two transfer methods call HidD_GetInputReport / HidD_SetOutputReport.
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

	results := make([]DeviceInfo, 0, 4)

	var (
		index uint32
		ifd   spDeviceInterfaceData
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

		path, perr := getDeviceInterfacePath(devInfoSet, &ifd)
		if perr != nil {
			continue
		}

		// Best-effort: try to open the device read-only just to query its
		// attributes; if that fails (permissions, in-use), skip it.
		info, ierr := queryDevice(path)
		if ierr != nil {
			continue
		}

		if vendorID != 0 && info.VendorID != vendorID {
			continue
		}

		if productID != 0 && info.ProductID != productID {
			continue
		}

		info.Path = path
		results = append(results, info)
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

	// SP_DEVICE_INTERFACE_DETAIL_DATA_W = { DWORD cbSize; WCHAR DevicePath[ANYSIZE_ARRAY]; }
	// On 64-bit Windows the struct is 8-byte aligned, so cbSize must be 8.
	// On 32-bit it's 6 (cbSize=DWORD + first WCHAR). We assume 64-bit since
	// goreleaser only builds windows/amd64.
	const headerSize = 8

	buf := make([]byte, requiredSize)
	*(*uint32)(unsafe.Pointer(&buf[0])) = headerSize

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

	pathBytes := buf[headerSize:]
	pathU16 := unsafe.Slice((*uint16)(unsafe.Pointer(&pathBytes[0])), (len(pathBytes))/2)

	return windows.UTF16ToString(pathU16), nil
}

// queryDevice opens the HID device just long enough to read its attributes
// and identifying strings. Closes its own handle before returning.
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

func readHidString(handle windows.Handle, proc *windows.LazyProc) string {
	const max = 256

	buf := make([]uint16, max)

	ret, _, _ := proc.Call(uintptr(handle), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)*2))
	if ret == 0 {
		return ""
	}

	return strings.TrimSpace(windows.UTF16ToString(buf))
}
