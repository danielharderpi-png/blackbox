package diff

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// RevertResult describes what Revert did.
type RevertResult string

// Revert outcomes.
const (
	Restored  RevertResult = "restored"
	Removed   RevertResult = "removed"
	Unchanged RevertResult = "unchanged"
)

// Revert restores one file to its baseline. target may be absolute or
// relative to the current directory; it must live inside root.
func Revert(root, target string) (RevertResult, error) {
	abs, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	relOS, err := filepath.Rel(root, abs)
	if err != nil || relOS == "." || relOS == ".." || strings.HasPrefix(relOS, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("%s is outside the repository", target)
	}
	rel := filepath.ToSlash(relOS)

	flight, err := LoadFlight(root)
	if err != nil {
		return "", err
	}
	pol, err := LoadArmedPolicy(root, flight)
	if err != nil {
		return "", err
	}
	if err := guardPath(root, abs); err != nil {
		return "", fmt.Errorf("refusing to revert %s: %w", rel, err)
	}
	if Match(rel, pol.Ignore) || rel == StateDir || strings.HasPrefix(rel, StateDir+"/") || rel == ".git" || strings.HasPrefix(rel, ".git/") {
		return "", fmt.Errorf("%s is ignored by blackbox and was not tracked", rel)
	}

	base, tracked := flight.Files[rel]
	if !tracked {
		info, err := os.Lstat(abs)
		if os.IsNotExist(err) {
			return "", fmt.Errorf("%s has no baseline and does not exist", rel)
		} else if err != nil {
			return "", err
		}
		if info.IsDir() {
			return "", fmt.Errorf("%s is a directory; revert works on single files", rel)
		}
		if err := os.Remove(abs); err != nil {
			return "", err
		}
		return Removed, nil
	}
	if !base.Stored {
		return "", fmt.Errorf("refusing to revert %s: baseline content was not stored (binary over 5MB)", rel)
	}
	data, err := ReadObject(root, base.SHA256)
	if err != nil {
		return "", err
	}
	if info, err := os.Lstat(abs); err == nil && info.Mode().IsRegular() {
		if cur, err := os.ReadFile(abs); err == nil {
			if h, _ := hashBytes(cur); h == base.SHA256 {
				return Unchanged, nil
			}
		}
	}
	if err := os.MkdirAll(filepath.Dir(abs), 0o755); err != nil {
		return "", err
	}
	tmp, err := os.CreateTemp(filepath.Dir(abs), ".bbox-revert-*")
	if err != nil {
		return "", err
	}
	_, werr := tmp.Write(data)
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr == nil {
		werr = os.Chmod(tmp.Name(), os.FileMode(base.Mode))
	}
	if werr == nil {
		werr = os.Rename(tmp.Name(), abs)
	}
	if werr != nil {
		os.Remove(tmp.Name())
		return "", werr
	}
	return Restored, nil
}

func hashBytes(b []byte) (string, error) {
	sum := sha256.Sum256(b)
	return hex.EncodeToString(sum[:]), nil
}

// guardPath refuses targets that would read, overwrite or delete anything
// outside the repository: any symlink/junction in a parent directory, and a
// final symlink that does not verifiably resolve to a location inside root.
func guardPath(root, abs string) error {
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return err
	}
	parts := strings.Split(rel, string(filepath.Separator))
	cur := root
	for i, part := range parts {
		cur = filepath.Join(cur, part)
		info, err := os.Lstat(cur)
		if err != nil {
			return nil // nothing further exists, so nothing can be followed
		}
		if !isLink(info.Mode()) {
			continue
		}
		if i < len(parts)-1 {
			return fmt.Errorf("path crosses a symlink or junction (%s)", cur)
		}
		if t, bad := linkEscapes(realRoot, cur); bad {
			return fmt.Errorf("symlink points outside the repository (%s); remove it manually", t)
		}
	}
	return nil
}
