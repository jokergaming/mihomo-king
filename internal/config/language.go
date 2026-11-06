package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	LanguageEnglish = "en"
	LanguageChinese = "zh"
)

// LoadLanguage defaults to English without creating a preference file.
func (s *Settings) LoadLanguage() (string, error) {
	body, err := os.ReadFile(filepath.Join(s.appDir, "language"))
	if os.IsNotExist(err) {
		return LanguageEnglish, nil
	}
	if err != nil {
		return "", fmt.Errorf("read language: %w", err)
	}
	language := strings.TrimSpace(string(body))
	if !validLanguage(language) {
		return "", fmt.Errorf("invalid interface language %q; expected en or zh", language)
	}
	return language, nil
}

// SaveLanguage is independent of settings.yaml so a UI preference save cannot
// overwrite proxy settings changed by another command while it was pending.
func (s *Settings) SaveLanguage(language string) error {
	if !validLanguage(language) {
		return fmt.Errorf("invalid interface language %q; expected en or zh", language)
	}
	f, err := os.CreateTemp(s.appDir, ".language-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	_, err = f.WriteString(language + "\n")
	if closeErr := f.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return err
	}
	return os.Rename(f.Name(), filepath.Join(s.appDir, "language"))
}

func validLanguage(language string) bool {
	return language == LanguageEnglish || language == LanguageChinese
}
