package copilotadapter

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

func checkPolicy() error {
	root, err := windows.KnownFolderPath(windows.FOLDERID_ProgramData, 0)
	if err != nil {
		return fmt.Errorf("cannot locate Copilot machine policy directory: %w", err)
	}
	if err := checkPolicyDirectory(filepath.Join(root, "GitHub", "Copilot", "policy.d")); err != nil {
		return err
	}
	// Cover native releases that resolve ProgramData from the environment too.
	for _, entry := range os.Environ() {
		key, value, _ := strings.Cut(entry, "=")
		if strings.EqualFold(key, "ProgramData") && value != "" {
			if err := checkPolicyDirectory(filepath.Join(value, "GitHub", "Copilot", "policy.d")); err != nil {
				return err
			}
		}
	}
	for _, view := range []uint32{registry.WOW64_64KEY, registry.WOW64_32KEY} {
		key, err := registry.OpenKey(registry.LOCAL_MACHINE, `Software\Policies\GitHub\Copilot`, registry.READ|view)
		if errors.Is(err, registry.ErrNotExist) {
			continue
		}
		if err != nil {
			return fmt.Errorf("cannot exclude Copilot registry policy hooks: %w", err)
		}
		key.Close()
		return errors.New("copilot machine policy registry entries prevent isolated reviews")
	}
	return nil
}
