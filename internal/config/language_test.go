package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLanguagePreferencePersistence(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	s, err := Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.Language != LanguageEnglish {
		t.Fatalf("default language = %q", s.Language)
	}
	if _, err := os.Stat(filepath.Join(s.AppDir(), "language")); !os.IsNotExist(err) {
		t.Fatalf("default created a language preference: %v", err)
	}
	if err := s.SaveLanguage(LanguageChinese); err != nil {
		t.Fatal(err)
	}
	s.MixedPort = 12345
	if err := s.Save(); err != nil {
		t.Fatal(err)
	}
	s, err = Load()
	if err != nil {
		t.Fatal(err)
	}
	if s.Language != LanguageChinese || s.MixedPort != 12345 {
		t.Fatalf("preferences lost: language=%q port=%d", s.Language, s.MixedPort)
	}
	if err := s.SaveLanguage("invalid"); err == nil {
		t.Fatal("accepted invalid language")
	}
	if language, err := s.LoadLanguage(); err != nil || language != LanguageChinese {
		t.Fatalf("invalid save damaged preference: %q, %v", language, err)
	}
	if err := os.WriteFile(filepath.Join(s.AppDir(), "language"), []byte("invalid"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(); err == nil {
		t.Fatal("silently accepted corrupt language preference")
	}
}
