package config

import "path/filepath"

// GeoMixin describes the shared geoip files a generated config may reference.
// Empty means the subscription keeps whatever geo setup it already has.
type GeoMixin struct {
	MihomoMode string            // dat or mmdb; files are already in the data dir
	SingBox    map[string]string // category → absolute .srs path
	Xray       string
}

// GeoMixin reports which shared geoip files are present for the active core.
// A subscription that already names its own geo database or geo URL is left
// alone: some providers ship a database, and mihomo, sing-box and xray can
// also download one themselves.
func (s *Settings) GeoMixin(root map[string]any) GeoMixin {
	switch s.Tool() {
	case ToolSingBox:
		return s.singBoxMixin()
	case ToolXray:
		if s.XrayDir == "" {
			return GeoMixin{}
		}
		path := filepath.Join(s.XrayDir, "geoip.dat")
		if !fileExists(path) {
			return GeoMixin{}
		}
		return GeoMixin{Xray: path}
	default:
		return s.mihomoMixin(root)
	}
}

func (s *Settings) mihomoMixin(root map[string]any) GeoMixin {
	if s.MihomoDir == "" || root == nil {
		return GeoMixin{}
	}
	if _, ok := root["geox-url"]; ok {
		return GeoMixin{}
	}
	if _, ok := root["geo-auto-update"]; ok {
		return GeoMixin{}
	}
	mode := ""
	if value, ok := root["geodata-mode"].(bool); ok {
		if value {
			mode = "dat"
		} else {
			mode = "mmdb"
		}
	}
	dat := filepath.Join(s.MihomoDir, "GeoIP.dat")
	mmdb := filepath.Join(s.MihomoDir, "geoip.metadb")
	switch mode {
	case "dat":
		if !fileExists(dat) {
			return GeoMixin{}
		}
	case "mmdb":
		if !fileExists(mmdb) {
			return GeoMixin{}
		}
	default:
		switch {
		case fileExists(dat):
			mode = "dat"
		case fileExists(mmdb):
			mode = "mmdb"
		default:
			return GeoMixin{}
		}
	}
	return GeoMixin{MihomoMode: mode}
}

func (s *Settings) singBoxMixin() GeoMixin {
	if s.SingBoxDir == "" {
		return GeoMixin{}
	}
	sets := map[string]string{}
	for _, category := range []string{"cn", "private"} {
		path := filepath.Join(s.SingBoxDir, "geoip-"+category+".srs")
		if !fileExists(path) {
			return GeoMixin{}
		}
		sets[category] = path
	}
	return GeoMixin{SingBox: sets}
}

// applyMihomoGeo selects the shared file already installed in mihomo's data
// directory. geox-url is a download URL, not a file path, so the mixin only
// disables mihomo's own download. A subscription that already chose a database
// or a download URL is not modified.
func applyMihomoGeo(root map[string]any, mixin GeoMixin) {
	if mixin.MihomoMode == "" {
		return
	}
	root["geodata-mode"] = mixin.MihomoMode == "dat"
	root["geo-auto-update"] = false
}

// applySingBoxGeo adds local rule-sets for the shared files. It does not add
// routing rules: sing-box conversion does not import subscription GEOIP rules,
// and a remote rule-set would make sing-box download another copy.
func applySingBoxGeo(route map[string]any, mixin GeoMixin) {
	if len(mixin.SingBox) == 0 {
		return
	}
	var sets []map[string]any
	for _, category := range []string{"cn", "private"} {
		path := mixin.SingBox[category]
		if path == "" {
			continue
		}
		sets = append(sets, map[string]any{
			"type":   "local",
			"tag":    "geoip-" + category,
			"format": "binary",
			"path":   path,
		})
	}
	if len(sets) == 0 {
		return
	}
	route["rule_set"] = sets
}
