package diff

import (
	"bytes"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"time"
)

const (
	// StateDir holds blackbox state inside the repo.
	StateDir = ".blackbox"
	// FlightFile is the baseline manifest inside StateDir.
	FlightFile = "flight.json"

	maxBinaryStore = 5 << 20 // binary files larger than this are hashed, not stored
)

// FileEntry describes one file in a snapshot.
type FileEntry struct {
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
	Mode   uint32 `json:"mode"`
	Binary bool   `json:"binary,omitempty"`
	Stored bool   `json:"stored"` // content saved in .blackbox/objects
}

// Flight is the persisted pre-session state.
type Flight struct {
	Version      int                  `json:"version"`
	ArmedAt      time.Time            `json:"armed_at"`
	Root         string               `json:"root"`
	GitHead      string               `json:"git_head,omitempty"`
	TreeHash     string               `json:"tree_hash"`
	PolicySHA256 string               `json:"policy_sha256,omitempty"` // hash of .blackbox.yml at arm time
	FileCount    int                  `json:"file_count"`
	Files        map[string]FileEntry `json:"files"`
}

// LoadFlight reads root/.blackbox/flight.json.
func LoadFlight(root string) (*Flight, error) {
	data, err := os.ReadFile(filepath.Join(root, StateDir, FlightFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, fmt.Errorf("no baseline found: run `blackbox arm` first")
		}
		return nil, err
	}
	var f Flight
	if err := json.Unmarshal(data, &f); err != nil {
		return nil, fmt.Errorf("corrupt %s: %w", FlightFile, err)
	}
	return &f, nil
}

// Snapshot hashes every non-ignored regular file under root. When store is
// true it also saves gzip copies to .blackbox/objects/<sha256>.gz.
func Snapshot(root string, ignore []string, store bool) (map[string]FileEntry, []string, error) {
	objDir := ""
	if store {
		objDir = filepath.Join(root, StateDir, "objects")
		if err := os.MkdirAll(objDir, 0o755); err != nil {
			return nil, nil, err
		}
	}
	realRoot, err := filepath.EvalSymlinks(root)
	if err != nil {
		return nil, nil, err
	}
	var warns []string
	files := map[string]FileEntry{}
	err = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() && (d.Name() == ".git" || d.Name() == StateDir) {
			return filepath.SkipDir
		}
		rel, err := filepath.Rel(root, p)
		if err != nil {
			return err
		}
		rel = filepath.ToSlash(rel)
		if rel == "." {
			return nil
		}
		if Match(rel, ignore) {
			if d.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Type()&(fs.ModeSymlink|fs.ModeIrregular) != 0 {
			// Symlinks (and Windows junctions) are never followed or hashed; flag ones that escape the repo.
			if target, bad := linkEscapes(realRoot, p); bad {
				warns = append(warns, fmt.Sprintf("skipped symlink/junction %s that points outside the repository or cannot be verified (%s)", rel, target))
			}
			return nil
		}
		if d.IsDir() || !d.Type().IsRegular() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			return err
		}
		entry, err := capture(p, info, objDir)
		if err != nil {
			return err
		}
		files[rel] = entry
		return nil
	})
	return files, warns, err
}

// capture hashes a file and, if objDir is set, saves a gzip copy unless it is
// a binary file larger than maxBinaryStore.
func capture(p string, info fs.FileInfo, objDir string) (FileEntry, error) {
	entry := FileEntry{Size: info.Size(), Mode: uint32(info.Mode().Perm())}
	f, err := os.Open(p)
	if err != nil {
		return entry, err
	}
	defer f.Close()
	head := make([]byte, 8000)
	n, err := io.ReadFull(f, head)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return entry, err
	}
	head = head[:n]
	entry.Binary = bytes.IndexByte(head, 0) >= 0
	if entry.Binary && info.Size() > maxBinaryStore {
		h := sha256.New()
		h.Write(head)
		if _, err := io.Copy(h, f); err != nil {
			return entry, err
		}
		entry.SHA256 = hex.EncodeToString(h.Sum(nil))
		return entry, nil
	}
	rest, err := io.ReadAll(f)
	if err != nil {
		return entry, err
	}
	data := append(head, rest...)
	sum := sha256.Sum256(data)
	entry.SHA256 = hex.EncodeToString(sum[:])
	if objDir != "" {
		if err := storeObject(objDir, entry.SHA256, data); err != nil {
			return entry, err
		}
		entry.Stored = true
	}
	return entry, nil
}

func storeObject(objDir, sum string, data []byte) error {
	dst := filepath.Join(objDir, sum+".gz")
	if _, err := os.Stat(dst); err == nil {
		return nil // content-addressed: already stored
	}
	tmp, err := os.CreateTemp(objDir, "tmp-*")
	if err != nil {
		return err
	}
	zw := gzip.NewWriter(tmp)
	_, werr := zw.Write(data)
	if cerr := zw.Close(); werr == nil {
		werr = cerr
	}
	if cerr := tmp.Close(); werr == nil {
		werr = cerr
	}
	if werr != nil {
		os.Remove(tmp.Name())
		return werr
	}
	if err := os.Rename(tmp.Name(), dst); err != nil {
		os.Remove(tmp.Name())
		return err
	}
	return nil
}

// ReadObject returns the stored content for a hash, verifying its integrity.
func ReadObject(root, sum string) ([]byte, error) {
	f, err := os.Open(filepath.Join(root, StateDir, "objects", sum+".gz"))
	if err != nil {
		return nil, fmt.Errorf("baseline object %s missing: %w", sum[:12], err)
	}
	defer f.Close()
	zr, err := gzip.NewReader(f)
	if err != nil {
		return nil, fmt.Errorf("baseline object %s corrupt: %w", sum[:12], err)
	}
	data, err := io.ReadAll(zr)
	if err != nil {
		return nil, fmt.Errorf("baseline object %s corrupt: %w", sum[:12], err)
	}
	if got := sha256.Sum256(data); hex.EncodeToString(got[:]) != sum {
		return nil, fmt.Errorf("baseline object %s failed hash check", sum[:12])
	}
	return data, nil
}

// TreeHash is a deterministic digest over sorted path/hash pairs.
func TreeHash(files map[string]FileEntry) string {
	paths := make([]string, 0, len(files))
	for p := range files {
		paths = append(paths, p)
	}
	sort.Strings(paths)
	h := sha256.New()
	for _, p := range paths {
		fmt.Fprintf(h, "%s\x00%s\n", p, files[p].SHA256)
	}
	return hex.EncodeToString(h.Sum(nil))
}
