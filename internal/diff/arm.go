package diff

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// PolicySnapshotFile is the immutable copy of the policy taken at arm time.
const PolicySnapshotFile = "policy.snapshot.yml"

// FlightVersion is the current flight.json format. Version 2 evaluates rules
// against the armed policy snapshot; version 1 used the live policy file.
const FlightVersion = 2

// Arm snapshots root into .blackbox and writes flight.json plus a copy of the
// policy file. It returns warnings for paths it refused to follow.
func Arm(root, gitHead string) (*Flight, []string, error) {
	state := filepath.Join(root, StateDir)
	if err := os.MkdirAll(state, 0o755); err != nil {
		return nil, nil, err
	}

	// Read the policy once so the parsed rules, the snapshot and the hash
	// all describe exactly the same bytes.
	policyPath := filepath.Join(root, PolicyFile)
	pol := &Policy{}
	policySum := ""
	snapPath := filepath.Join(state, PolicySnapshotFile)
	info, err := os.Lstat(policyPath)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		if err := os.Remove(snapPath); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return nil, nil, err
		}
	case err != nil:
		return nil, nil, err
	case info.Mode()&fs.ModeSymlink != 0:
		return nil, nil, fmt.Errorf("%s is a symlink; refusing to arm", PolicyFile)
	default:
		data, err := os.ReadFile(policyPath)
		if err != nil {
			return nil, nil, err
		}
		sum := sha256.Sum256(data)
		policySum = hex.EncodeToString(sum[:])
		pol = ParsePolicy(data)
		if err := os.WriteFile(snapPath, data, 0o644); err != nil {
			return nil, nil, err
		}
	}

	files, warns, err := Snapshot(root, pol.Ignore, true)
	if err != nil {
		return nil, nil, err
	}
	flight := &Flight{
		Version:      FlightVersion,
		ArmedAt:      time.Now().UTC(),
		Root:         root,
		GitHead:      gitHead,
		TreeHash:     TreeHash(files),
		FileCount:    len(files),
		PolicySHA256: policySum,
		Files:        files,
	}
	data, err := json.MarshalIndent(flight, "", "  ")
	if err != nil {
		return nil, nil, err
	}
	if err := os.WriteFile(filepath.Join(state, FlightFile), append(data, '\n'), 0o644); err != nil {
		return nil, nil, err
	}
	return flight, warns, nil
}

// LoadArmedPolicy returns the policy that was in force when arm ran. For
// current flights that is the verified snapshot; legacy (version 1) flights
// fall back to the live file.
func LoadArmedPolicy(root string, f *Flight) (*Policy, error) {
	if f.Version < 2 {
		return LoadPolicy(root)
	}
	if f.PolicySHA256 == "" {
		return &Policy{}, nil // no policy existed at arm time
	}
	data, err := os.ReadFile(filepath.Join(root, StateDir, PolicySnapshotFile))
	if err != nil {
		return nil, fmt.Errorf("armed policy snapshot unreadable: %w (re-run `blackbox arm`)", err)
	}
	if sum := sha256.Sum256(data); hex.EncodeToString(sum[:]) != f.PolicySHA256 {
		return nil, fmt.Errorf("armed policy snapshot was modified; re-run `blackbox arm`")
	}
	return ParsePolicy(data), nil
}

// policyTamper reports whether the live policy file changed or vanished since
// arm. It never follows a symlink.
func policyTamper(root string, f *Flight) (Status, bool) {
	if f.Version < 2 || f.PolicySHA256 == "" {
		return "", false
	}
	p := filepath.Join(root, PolicyFile)
	info, err := os.Lstat(p)
	if err != nil {
		return Deleted, true
	}
	if info.Mode()&fs.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return Modified, true
	}
	data, err := os.ReadFile(p)
	if err != nil {
		return Modified, true
	}
	sum := sha256.Sum256(data)
	if hex.EncodeToString(sum[:]) != f.PolicySHA256 {
		return Modified, true
	}
	return "", false
}

// within reports whether target is inside (or equal to) base. Both must
// already be absolute and symlink-resolved.
func within(base, target string) bool {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != ".." && !hasDotDotPrefix(rel))
}

func hasDotDotPrefix(rel string) bool {
	return len(rel) >= 3 && rel[:2] == ".." && os.IsPathSeparator(rel[2])
}

func isLink(m fs.FileMode) bool { return m&(fs.ModeSymlink|fs.ModeIrregular) != 0 }

// linkEscapes reports whether the link at p (inside the repo) fails to
// verifiably resolve to a different location inside realRoot. Windows
// junctions may resolve to themselves, so that case counts as unverifiable.
func linkEscapes(realRoot, p string) (string, bool) {
	t, err := filepath.EvalSymlinks(p)
	if err != nil {
		return p, true
	}
	if !within(realRoot, t) {
		return t, true
	}
	parent, err := filepath.EvalSymlinks(filepath.Dir(p))
	if err != nil || strings.EqualFold(t, filepath.Join(parent, filepath.Base(p))) {
		return t, true
	}
	return t, false
}
