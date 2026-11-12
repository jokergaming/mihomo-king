package config

import (
	"errors"
	"runtime"
	"strconv"
	"strings"
)

var ErrInvalidTunDevice = errors.New("macOS TUN device must be utun followed by a number (for example utun1024); change tun device in settings")

func defaultTunDevice() string {
	if runtime.GOOS == "darwin" {
		return "utun1024"
	}
	return "mihomo-king"
}

// ValidateTunDevice rejects names the macOS kernel cannot create.
func (s *Settings) ValidateTunDevice() error {
	if runtime.GOOS != "darwin" {
		return nil
	}
	number, ok := strings.CutPrefix(s.TunDevice, "utun")
	n, err := strconv.ParseUint(number, 10, 32)
	if !ok || err != nil || strconv.FormatUint(n, 10) != number {
		return ErrInvalidTunDevice
	}
	return nil
}
