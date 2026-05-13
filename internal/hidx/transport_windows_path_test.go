//go:build windows

package hidx

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

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
