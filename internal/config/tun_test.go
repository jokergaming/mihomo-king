package config

import (
	"runtime"
	"testing"
)

func TestTunDeviceDefaultsAndValidation(t *testing.T) {
	s := defaults()
	if err := s.ValidateTunDevice(); err != nil {
		t.Fatalf("invalid default TUN device: %v", err)
	}
	if runtime.GOOS != "darwin" {
		if s.TunDevice != "mihomo-king" {
			t.Fatalf("Linux default TUN device = %s", s.TunDevice)
		}
		return
	}
	for _, name := range []string{"utun0", "utun1024", "utun65534"} {
		s.TunDevice = name
		if err := s.ValidateTunDevice(); err != nil {
			t.Errorf("valid device %s: %v", name, err)
		}
	}
	for _, name := range []string{"mihomo-king", "utun", "utun-1", "utun+1", "utun01", "utun1x"} {
		s.TunDevice = name
		if s.ValidateTunDevice() == nil {
			t.Errorf("invalid device %s was accepted", name)
		}
	}
}
