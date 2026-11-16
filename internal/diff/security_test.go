package diff

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func write(t *testing.T, p, s string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func hasKind(r *Report, kind string) bool {
	for _, v := range r.Violations {
		if v.Kind == kind {
			return true
		}
	}
	return false
}

func TestPolicyTamperAndSnapshot(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, PolicyFile), "no_fly_zones:\n  - secret.txt\n")
	write(t, filepath.Join(root, "secret.txt"), "a\n")
	if _, _, err := Arm(root, ""); err != nil {
		t.Fatal(err)
	}

	// Weaken the live policy and touch the protected file.
	write(t, filepath.Join(root, PolicyFile), "no_fly_zones: []\n")
	write(t, filepath.Join(root, "secret.txt"), "b\n")
	rep, err := Analyze(root)
	if err != nil {
		t.Fatal(err)
	}
	if !hasKind(rep, "policy_tamper") || !hasKind(rep, "no_fly_zone") {
		t.Fatalf("want tamper + no-fly violations, got %+v", rep.Violations)
	}

	// Deleting the policy is also tampering.
	os.Remove(filepath.Join(root, PolicyFile))
	rep, err = Analyze(root)
	if err != nil || !hasKind(rep, "policy_tamper") {
		t.Fatalf("deleted policy not flagged: %v %+v", err, rep)
	}

	// Tampering with the snapshot itself must be detected, not trusted.
	write(t, filepath.Join(root, StateDir, PolicySnapshotFile), "no_fly_zones: []\n")
	if _, err := Analyze(root); err == nil {
		t.Fatal("modified policy snapshot was trusted")
	}
}

func TestSymlinkGuard(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	write(t, filepath.Join(outside, "victim.txt"), "precious\n")
	write(t, filepath.Join(root, "real.txt"), "x\n")
	fileLink := os.Symlink(filepath.Join(outside, "victim.txt"), filepath.Join(root, "link.txt")) == nil
	linkDir := os.Symlink(outside, filepath.Join(root, "linkdir")) == nil
	if !linkDir && runtime.GOOS == "windows" { // junctions need no privilege
		linkDir = exec.Command("cmd", "/c", "mklink", "/J", filepath.Join(root, "linkdir"), outside).Run() == nil
	}
	if !linkDir {
		t.Skip("cannot create links on this system")
	}
	flight, warns, err := Arm(root, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := flight.Files["link.txt"]; ok {
		t.Fatal("symlink was snapshotted")
	}
	if len(warns) < 1 {
		t.Fatalf("want a warning for the escaping link, got %v", warns)
	}
	paths := []string{"linkdir/victim.txt"}
	if fileLink {
		paths = append(paths, "link.txt")
	}
	for _, p := range paths {
		if _, err := Revert(root, filepath.Join(root, filepath.FromSlash(p))); err == nil {
			t.Fatalf("revert of %s should be refused", p)
		}
	}
	data, _ := os.ReadFile(filepath.Join(outside, "victim.txt"))
	if !strings.Contains(string(data), "precious") {
		t.Fatal("outside file was modified")
	}
}
