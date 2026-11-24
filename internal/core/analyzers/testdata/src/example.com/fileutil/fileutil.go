// Package fileutil imports os, though Ext reaches none of it.
package fileutil

import (
	"errors"
	"os"
	"path/filepath"
)

func Ext(name string) string { return filepath.Ext(name) }

func Read(name string) ([]byte, error) { return os.ReadFile(name) }

// Missing compares with a sentinel error os declares, which is pure.
func Missing(err error) bool { return errors.Is(err, os.ErrNotExist) }
