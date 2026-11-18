package core

import (
	"context"
	"fmt"
	"regexp"
	"runtime"
	"strings"

	"golang.org/x/mod/semver"

	"mihomo-king/internal/config"
	"mihomo-king/internal/corebin"
)

type Update struct {
	Tool, Installed, Latest string
}

var rollingAlpha = regexp.MustCompile(`^alpha-g?([0-9a-f]+)$`)

// CheckUpdate compares the configured binary with its published release.
// Mihomo's rolling alpha has a commit ID in its asset name instead of a version.
func CheckUpdate(ctx context.Context, tool, bin, channel, source string) (Update, error) {
	spec, err := ReleaseSpec(tool, source)
	if err != nil {
		return Update{}, err
	}
	line, err := corebin.Version(ctx, bin, tool)
	if err != nil {
		return Update{}, fmt.Errorf("%s: %w", bin, err)
	}
	var installed string
	for field := range strings.FieldsSeq(line) {
		if tool == config.ToolMihomo && rollingAlpha.MatchString(field) {
			installed = field
			break
		}
		v := "v" + strings.TrimPrefix(field, "v")
		if semver.IsValid(v) {
			installed = v
			break
		}
	}
	if installed == "" {
		return Update{}, fmt.Errorf("unrecognized %s version: %q", tool, line)
	}
	rel, err := Latest(ctx, spec, channel, runtime.GOOS, runtime.GOARCH)
	if err != nil {
		return Update{}, err
	}
	latest := rel.Tag
	if hash := rollingAlpha.FindStringSubmatch(installed); hash != nil {
		if channel != ChannelAlpha {
			return Update{}, fmt.Errorf("installed mihomo is alpha; select its alpha channel")
		}
		_, commit, _ := strings.Cut(rel.Asset, "-alpha-")
		commit = strings.TrimSuffix(commit, ".gz")
		if strings.HasPrefix(hash[1], commit) || strings.HasPrefix(commit, hash[1]) {
			return Update{}, nil
		}
		latest = "alpha-" + commit
	} else {
		latest = "v" + strings.TrimPrefix(latest, "v")
		if !semver.IsValid(latest) {
			return Update{}, fmt.Errorf("unrecognized %s release: %q", tool, rel.Tag)
		}
		if semver.Compare(latest, installed) <= 0 {
			return Update{}, nil
		}
	}
	return Update{Tool: tool, Installed: installed, Latest: latest}, nil
}
