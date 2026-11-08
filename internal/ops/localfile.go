package ops

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/iodesystems/para/internal/model"
)

// localFile checks a source or evidence that names a file on this machine:
// an absolute path, or one that starts with ~/. The file must exist at the
// time of the write and must not be a directory. It returns the path as
// given, cleaned; ~/ is kept, not expanded. Nothing leaves the machine.
func localFile(ref string) (string, error) {
	ref = strings.TrimSpace(ref)
	if !model.IsLocalPath(ref) {
		return "", fmt.Errorf("%s is not an absolute path or ~/path", ref)
	}
	abs, keep := filepath.Clean(ref), filepath.Clean(ref)
	if rest, ok := strings.CutPrefix(ref, "~/"); ok {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("%s: no home folder to expand ~: %w", ref, err)
		}
		rest = strings.TrimPrefix(filepath.Clean("/"+rest), "/")
		abs, keep = filepath.Join(home, rest), "~/"+rest
	}
	fi, err := os.Stat(abs)
	switch {
	case err != nil:
		return "", fmt.Errorf("%s: no such file on this machine", ref)
	case fi.IsDir():
		return "", fmt.Errorf("%s is a folder: name a file", ref)
	}
	return keep, nil
}
