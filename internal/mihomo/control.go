package mihomo

import (
	"cmp"
	"fmt"

	"mihomo-king/internal/api"
	"mihomo-king/internal/config"
)

// Reload rewrites the runtime config from the active subscription and applies
// it: mihomo reloads it over the API, sing-box is restarted. Nothing but the
// config file is touched when the tool is not running.
func Reload(s *config.Settings) error {
	if err := s.WriteActiveFromStore(); err != nil {
		return err
	}
	running, _ := Running(s)
	switch {
	case running && s.Tool() == config.ToolMihomo:
		if err := api.New(s.Controller, s.Secret).ReloadConfig(s.ConfigPath()); err != nil {
			return fmt.Errorf("reload: %w", err)
		}
	case running:
		if err := Restart(s); err != nil {
			return fmt.Errorf("restart: %w", err)
		}
	}
	return nil
}

// SwitchSub applies subscription name as the runtime config. Its config is
// validated first, and if the runtime still rejects it the config of the
// currently active subscription is restored, so a broken subscription never
// replaces a working one. s is not modified: the caller records name as
// active (and saves) once this succeeds.
func SwitchSub(s *config.Settings, name string) error {
	if sub, _ := s.FindSub(name); sub == nil {
		return fmt.Errorf("no subscription named %q", name)
	}
	next := *s
	next.Active = name
	if err := CheckActive(&next); err != nil {
		return fmt.Errorf("%s not activated: %w", name, err)
	}
	if err := Reload(&next); err != nil {
		err = fmt.Errorf("%s not activated: %w", name, err)
		if restoreErr := Reload(s); restoreErr != nil {
			err = fmt.Errorf("%w; restoring %s also failed: %v", err, cmp.Or(s.Active, "previous config"), restoreErr)
		}
		return err
	}
	return nil
}

// ApplyTun applies s.TunEnable (already set and saved by the caller): the
// config file is rewritten, mihomo toggles TUN live, sing-box is restarted.
func ApplyTun(s *config.Settings) error {
	if err := s.WriteActiveFromStore(); err != nil {
		return err
	}
	running, _ := Running(s)
	switch {
	case running && s.Tool() == config.ToolMihomo:
		if err := api.New(s.Controller, s.Secret).SetTun(s.TunEnable); err != nil {
			return fmt.Errorf("tun live toggle: %w", err)
		}
	case running:
		if err := Restart(s); err != nil {
			return fmt.Errorf("restart for tun change: %w", err)
		}
	}
	return nil
}

// ApplyMode applies s.Mode (already set and saved by the caller): mihomo
// switches live, sing-box is restarted with the rewritten config.
func ApplyMode(s *config.Settings) error {
	if err := s.WriteActiveFromStore(); err != nil {
		return err
	}
	running, _ := Running(s)
	switch {
	case running && s.Tool() == config.ToolMihomo:
		if err := api.New(s.Controller, s.Secret).SetMode(s.Mode); err != nil {
			return fmt.Errorf("mode live switch: %w", err)
		}
	case running:
		if err := Restart(s); err != nil {
			return fmt.Errorf("restart for mode change: %w", err)
		}
	}
	return nil
}
