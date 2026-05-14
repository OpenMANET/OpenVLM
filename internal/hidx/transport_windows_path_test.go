//go:build windows

package hidx

import (
	"testing"
	"unicode/utf16"

	"github.com/stretchr/testify/assert"
)

// TestDecodeDetailPath pins the buffer-offset assumption inside
// getDeviceInterfacePath. Regression test for a bug where the DevicePath
// byte offset was conflated with the struct's sizeof, eating the leading
// `\\` of every HID device path on Windows and causing every subsequent
// CreateFile to fail with ERROR_INVALID_NAME.
func TestDecodeDetailPath(t *testing.T) {
	// Synthesize what Windows hands back in the post-cbSize portion of
	// the SP_DEVICE_INTERFACE_DETAIL_DATA_W buffer: a UTF-16LE,
	// null-terminated copy of the device path, optionally followed by
	// trailing alignment / slack bytes.
	want := `\\?\hid#vid_0d8c&pid_0012&mi_03#7&104c02ef&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`

	u16 := utf16.Encode([]rune(want))
	u16 = append(u16, 0) // null terminator

	buf := make([]byte, len(u16)*2+4) // +4 to mimic trailing alignment slack
	for i, w := range u16 {
		buf[i*2] = byte(w)
		buf[i*2+1] = byte(w >> 8)
	}

	got := decodeDetailPath(buf)
	assert.Equal(t, want, got, "leading characters must be preserved; missing \\\\ means the wrong byte offset")
}

func TestParseVIDPIDFromPath(t *testing.T) {
	tests := []struct {
		name    string
		path    string
		wantVID uint16
		wantPID uint16
		wantOK  bool
	}{
		{
			name:    "openvlm uppercase",
			path:    `\\?\HID#VID_0D8C&PID_0012&MI_03#7&abc&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`,
			wantVID: 0x0D8C,
			wantPID: 0x0012,
			wantOK:  true,
		},
		{
			name:    "openvlm lowercase",
			path:    `\\?\hid#vid_0d8c&pid_0012&mi_03#7&abc&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`,
			wantVID: 0x0D8C,
			wantPID: 0x0012,
			wantOK:  true,
		},
		{
			name:    "mixed case",
			path:    `\\?\HID#Vid_0D8C&Pid_0012&MI_03#7&abc&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`,
			wantVID: 0x0D8C,
			wantPID: 0x0012,
			wantOK:  true,
		},
		{
			name:    "different vendor",
			path:    `\\?\HID#VID_046D&PID_C52B&MI_01#7&xyz&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`,
			wantVID: 0x046D,
			wantPID: 0xC52B,
			wantOK:  true,
		},
		{
			name:   "no vid pid markers",
			path:   `\\?\HID#{00001812-0000-1000-8000-00805f9b34fb}#7&abc&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`,
			wantOK: false,
		},
		{
			name:   "vid only no pid",
			path:   `\\?\HID#VID_0D8C&MI_03#7&abc&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`,
			wantOK: false,
		},
		{
			name:   "pid only no vid",
			path:   `\\?\HID#PID_0012&MI_03#7&abc&0&0000#{4d1e55b2-f16f-11cf-88cb-001111000030}`,
			wantOK: false,
		},
		{
			name:   "empty",
			path:   "",
			wantOK: false,
		},
		{
			name:   "short hex",
			path:   `\\?\HID#VID_0D8&PID_0012`,
			wantOK: false,
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			vid, pid, ok := parseVIDPIDFromPath(tc.path)
			assert.Equal(t, tc.wantOK, ok)

			if tc.wantOK {
				assert.Equal(t, tc.wantVID, vid)
				assert.Equal(t, tc.wantPID, pid)
			}
		})
	}
}
