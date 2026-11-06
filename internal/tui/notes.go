package tui

import (
	"mihomo-king/internal/core"
	"mihomo-king/internal/geo"
	"mihomo-king/internal/subscription"
)

func fetchedNote(done text, res *subscription.Result) text {
	return text{key: "subscription result", format: func(tr func(string, ...any) string) string {
		summary := res.FormatSummary(tr)
		if summary == "" {
			return tr("%s", done)
		}
		return tr("%s · %s", done, summary)
	}}
}

func coreNote(tool string, res *core.Result) text {
	return text{key: "core result", format: func(tr func(string, ...any) string) string {
		return core.FormatNote(tool, res, tr)
	}}
}

func geoNote(res *geo.Result) text {
	return text{key: "geoip result", format: func(tr func(string, ...any) string) string {
		return geo.FormatNote(res, tr)
	}}
}
