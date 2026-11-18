// Package corebin reads version information from installed proxy binaries.
package corebin

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

func Version(ctx context.Context, bin, tool string) (string, error) {
	arg := "version"
	if tool == "mihomo" {
		arg = "-v"
	}
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, arg).Output()
	if err != nil {
		return "", err
	}
	line, _, _ := strings.Cut(strings.TrimSpace(string(out)), "\n")
	return line, nil
}
