package gomod

import (
	"fmt"
	"os"
	"path/filepath"

	"golang.org/x/mod/modfile"
)

func FindGoModDir(start string, moduleName string) (string, error) {
	dir, err := filepath.Abs(start)
	if err != nil {
		return "", fmt.Errorf("failed to get absolute path for %s: %w", start, err)
	}
	for {
		modPath := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(modPath); err == nil {
			data, err := os.ReadFile(modPath)
			if err != nil {
				return "", fmt.Errorf("failed to read %s: %w", modPath, err)
			}
			modFile, err := modfile.Parse(modPath, data, nil)
			if err != nil {
				return "", fmt.Errorf("failed to parse %s: %w", modPath, err)
			}
			if modFile.Module != nil && modFile.Module.Mod.Path == moduleName {
				return dir, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			return "", fmt.Errorf("go.mod with module path %q not found", moduleName)
		}
		dir = parent
	}
}
