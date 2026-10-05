package config

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/joho/godotenv"
)

// LoadDotEnv loads every existing file in paths into the environment, in
// order. Missing files are skipped (godotenv.Load stops at the first missing
// one). Variables already set in the real environment are never overridden,
// and earlier files win over later ones.
//
// An empty value followed by a comment ("APP_TOKEN=   # optional") is read
// as empty; godotenv would otherwise return the comment text as the value.
func LoadDotEnv(paths ...string) error {
	for _, p := range paths {
		if _, err := os.Stat(p); errors.Is(err, fs.ErrNotExist) {
			continue
		}
		vars, err := godotenv.Read(p)
		if err != nil {
			return fmt.Errorf("load %s: %w", p, err)
		}
		for k, v := range vars {
			if _, set := os.LookupEnv(k); set {
				continue
			}
			if strings.HasPrefix(strings.TrimSpace(v), "#") {
				v = ""
			}
			if err := os.Setenv(k, v); err != nil {
				return fmt.Errorf("set %s from %s: %w", k, p, err)
			}
		}
	}
	return nil
}
