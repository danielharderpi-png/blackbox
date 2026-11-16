package diff

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Status is the kind of change made to a file since arm.
type Status string

// Change statuses.
const (
	Added    Status = "added"
	Modified Status = "modified"
	Deleted  Status = "deleted"
)

// Change is one file-level difference against the baseline.
type Change struct {
	Path         string
	Status       Status
	Added        int  // lines added
	Removed      int  // lines removed
	Binary       bool // no line counts for binary files
	LinesKnown   bool // false when baseline content was not stored
	Approx       bool // line counts are approximate (huge diff)
	AddedLines   []string
	RemovedLines []string
}

// Violation is a policy breach.
type Violation struct {
	Kind    string // "no_fly_zone" or "sensitive_pattern"
	Path    string
	Status  Status
	Line    int    // 1-based line in the new file (sensitive_pattern only)
	Pattern string // the pattern that matched
}

// Report is the result of comparing the working tree to the baseline.
type Report struct {
	Flight     *Flight
	Changes    []Change
	Violations []Violation
	Warnings   []string
}

// Totals returns summed added/removed line counts.
func (r *Report) Totals() (added, removed int) {
	for _, c := range r.Changes {
		added += c.Added
		removed += c.Removed
	}
	return
}

// Analyze compares the working tree under root against the recorded baseline.
func Analyze(root string) (*Report, error) {
	flight, err := LoadFlight(root)
	if err != nil {
		return nil, err
	}
	pol, err := LoadArmedPolicy(root, flight)
	if err != nil {
		return nil, err
	}
	var sens []*regexp.Regexp
	for _, p := range pol.SensitivePatterns {
		re, err := regexp.Compile(p)
		if err != nil {
			return nil, fmt.Errorf("invalid sensitive_patterns entry %q: %w", p, err)
		}
		sens = append(sens, re)
	}
	cur, warns, err := Snapshot(root, pol.Ignore, false)
	if err != nil {
		return nil, err
	}

	seen := map[string]bool{}
	var paths []string
	for p := range flight.Files {
		seen[p] = true
		paths = append(paths, p)
	}
	for p := range cur {
		if !seen[p] {
			paths = append(paths, p)
		}
	}
	sort.Strings(paths)

	rep := &Report{Flight: flight, Warnings: warns}
	if st, bad := policyTamper(root, flight); bad {
		rep.Violations = append(rep.Violations, Violation{Kind: "policy_tamper", Path: PolicyFile, Status: st})
	}
	for _, p := range paths {
		base, inBase := flight.Files[p]
		now, inNow := cur[p]
		ch := Change{Path: p}
		var newLines []string
		var addedIdx []int
		switch {
		case inBase && !inNow:
			if Match(p, pol.Ignore) {
				continue // newly ignored by policy, not deleted
			}
			ch.Status = Deleted
			ch.Binary = base.Binary
			if base.Binary || !base.Stored {
				break
			}
			data, err := ReadObject(root, base.SHA256)
			if err != nil {
				return nil, err
			}
			old := splitLines(string(data))
			ch.LinesKnown, ch.Removed, ch.RemovedLines = true, len(old), old
		case !inBase && inNow:
			ch.Status = Added
			ch.Binary = now.Binary
			if now.Binary {
				break
			}
			data, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
			if err != nil {
				return nil, err
			}
			newLines = splitLines(string(data))
			ch.LinesKnown, ch.Added, ch.AddedLines = true, len(newLines), newLines
			for i := range newLines {
				addedIdx = append(addedIdx, i)
			}
		case base.SHA256 == now.SHA256:
			continue
		default:
			ch.Status = Modified
			ch.Binary = base.Binary || now.Binary
			if ch.Binary || !base.Stored {
				break
			}
			odata, err := ReadObject(root, base.SHA256)
			if err != nil {
				return nil, err
			}
			ndata, err := os.ReadFile(filepath.Join(root, filepath.FromSlash(p)))
			if err != nil {
				return nil, err
			}
			old := splitLines(string(odata))
			newLines = splitLines(string(ndata))
			add, rem, approx := Lines(old, newLines)
			ch.LinesKnown, ch.Approx = true, approx
			ch.Added, ch.Removed = len(add), len(rem)
			for _, i := range add {
				ch.AddedLines = append(ch.AddedLines, newLines[i])
			}
			for _, i := range rem {
				ch.RemovedLines = append(ch.RemovedLines, old[i])
			}
			addedIdx = add
		}
		rep.Changes = append(rep.Changes, ch)

		if Match(p, pol.NoFlyZones) {
			rep.Violations = append(rep.Violations, Violation{Kind: "no_fly_zone", Path: p, Status: ch.Status})
		}
		for _, i := range addedIdx {
			for _, re := range sens {
				if re.MatchString(newLines[i]) {
					rep.Violations = append(rep.Violations, Violation{
						Kind: "sensitive_pattern", Path: p, Status: ch.Status, Line: i + 1, Pattern: re.String(),
					})
				}
			}
		}
	}
	return rep, nil
}

// splitLines splits text into lines without the trailing empty element.
func splitLines(s string) []string {
	if s == "" {
		return nil
	}
	lines := strings.Split(s, "\n")
	if lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	return lines
}
