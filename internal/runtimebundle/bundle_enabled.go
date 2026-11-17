//go:build bundle

package runtimebundle

import (
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// The release build copies the compiled UI and AI engine into assets/ and
// builds the host with `-tags bundle`. The result is a single PopTrans.exe.
//
//go:embed assets/translate-ui.exe
var uiExe []byte

//go:embed assets/ai_engine.exe
var engineExe []byte

// buildID is stamped by the build script (-ldflags -X ...buildID=...) so an
// updated PopTrans.exe extracts into its own directory instead of overwriting
// worker executables that a previous still-running version keeps locked.
var buildID = ""

// bundleVersion returns a filesystem-safe key for this bundle. buildID is
// preferred; if the linker did not stamp one, a content hash keeps different
// releases isolated as well.
func bundleVersion() string {
	if buildID != "" {
		var safe strings.Builder
		for _, r := range buildID {
			if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') ||
				(r >= '0' && r <= '9') || r == '.' || r == '-' || r == '_' {
				safe.WriteRune(r)
			}
		}
		if safe.Len() > 0 {
			return safe.String()
		}
	}
	hash := sha256.New()
	_, _ = hash.Write(uiExe)
	_, _ = hash.Write(engineExe)
	return hex.EncodeToString(hash.Sum(nil)[:8])
}

func Ensure(baseDir string) (Paths, error) {
	version := bundleVersion()

	// Never overwrite the legacy shared runtime directory directly: an older
	// PopTrans may still be running translate-ui.exe from it, which makes the
	// rename fail on Windows. Each build gets its own subdirectory instead.
	roots := make([]string, 0, 2)
	if local := os.Getenv("LOCALAPPDATA"); local != "" {
		roots = append(roots, filepath.Join(local, "PopTrans", DirName))
	}
	roots = append(roots, runtimeDir(baseDir))

	var lastErr error
	seen := make(map[string]bool, len(roots))
	for _, root := range roots {
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true

		dirs := []string{filepath.Join(root, version)}
		// If the versioned directory itself is locked or has a corrupt file,
		// a per-process directory still lets the new build start.
		dirs = append(dirs, filepath.Join(root, version+"-"+strconv.Itoa(os.Getpid())))
		for _, dir := range dirs {
			paths, err := extractInto(dir)
			if err == nil {
				return paths, nil
			}
			lastErr = err
		}
	}
	if lastErr == nil {
		lastErr = errors.New("no writable runtime directory")
	}
	return Paths{}, lastErr
}

func extractInto(dir string) (Paths, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return Paths{}, fmt.Errorf("create runtime dir %s: %w", dir, err)
	}
	uiPath, err := extractIfNeeded(dir, "translate-ui.exe", uiExe)
	if err != nil {
		return Paths{}, err
	}
	enginePath, err := extractIfNeeded(dir, "ai_engine.exe", engineExe)
	if err != nil {
		return Paths{}, err
	}
	if buildID != "" {
		_ = os.WriteFile(filepath.Join(dir, ".build-id"), []byte(buildID), 0o644)
	}
	return Paths{UIPath: uiPath, EnginePath: enginePath}, nil
}

func extractIfNeeded(dir, name string, data []byte) (string, error) {
	target := filepath.Join(dir, name)
	if info, err := os.Stat(target); err == nil && info.Size() == int64(len(data)) {
		return target, nil
	}

	tmp := target + ".tmp"
	if err := os.WriteFile(tmp, data, 0o755); err != nil {
		return "", fmt.Errorf("extract %s: %w", name, err)
	}
	if err := os.Rename(tmp, target); err != nil {
		_ = os.Remove(tmp)
		return "", fmt.Errorf("install %s: %w", name, err)
	}
	return target, nil
}
