// Package diff snapshots a working tree and compares it to a recorded baseline.
package diff

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strings"
)

// PolicyFile is the name of the policy file in the repo root.
const PolicyFile = ".blackbox.yml"

// Policy holds the lists parsed from .blackbox.yml.
type Policy struct {
	Ignore            []string
	NoFlyZones        []string
	SensitivePatterns []string // regular expressions
}

// LoadPolicy reads root/.blackbox.yml. A missing file yields an empty policy.
func LoadPolicy(root string) (*Policy, error) {
	data, err := os.ReadFile(filepath.Join(root, PolicyFile))
	if errors.Is(err, fs.ErrNotExist) {
		return &Policy{}, nil
	} else if err != nil {
		return nil, err
	}
	return ParsePolicy(data), nil
}

// ParsePolicy parses policy file contents.
func ParsePolicy(data []byte) *Policy {
	lists := parseLists(string(data))
	return &Policy{
		Ignore:            lists["ignore"],
		NoFlyZones:        lists["no_fly_zones"],
		SensitivePatterns: lists["sensitive_patterns"],
	}
}

// parseLists is a minimal YAML reader: top-level `key:` lines followed by
// `- item` lines. Anything else is ignored.
func parseLists(data string) map[string][]string {
	out := map[string][]string{}
	key := ""
	for _, line := range strings.Split(data, "\n") {
		line = strings.TrimRight(line, "\r")
		trim := strings.TrimSpace(line)
		if trim == "" || strings.HasPrefix(trim, "#") {
			continue
		}
		if !strings.HasPrefix(line, " ") && !strings.HasPrefix(line, "\t") && !strings.HasPrefix(trim, "-") {
			if i := strings.Index(trim, ":"); i > 0 {
				key = trim[:i]
			} else {
				key = ""
			}
			continue
		}
		if key != "" && strings.HasPrefix(trim, "-") {
			if v := unquote(strings.TrimSpace(trim[1:])); v != "" {
				out[key] = append(out[key], v)
			}
		}
	}
	return out
}

func unquote(v string) string {
	if v != "" && v[0] == '\'' {
		var sb strings.Builder
		for i := 1; i < len(v); i++ {
			if v[i] == '\'' {
				if i+1 < len(v) && v[i+1] == '\'' {
					sb.WriteByte('\'')
					i++
					continue
				}
				break
			}
			sb.WriteByte(v[i])
		}
		return sb.String()
	}
	if v != "" && v[0] == '"' {
		if j := strings.IndexByte(v[1:], '"'); j >= 0 {
			return v[1 : j+1]
		}
		return v[1:]
	}
	if i := strings.Index(v, " #"); i >= 0 {
		v = v[:i]
	}
	return strings.TrimSpace(v)
}

// Match reports whether the slash-separated relative path, or any of its
// parent directories, matches one of the glob patterns. A leading "/" anchors
// a pattern to the root; otherwise it matches at any depth. "**" spans
// directories and "dir/**" matches dir itself.
func Match(rel string, pats []string) bool {
	segs := strings.Split(rel, "/")
	for _, p := range pats {
		anchored := strings.HasPrefix(p, "/")
		p = strings.TrimSuffix(strings.Trim(p, "/"), "/**")
		if p == "" {
			continue
		}
		psegs := strings.Split(p, "/")
		for k := 1; k <= len(segs); k++ {
			prefix := segs[:k]
			if matchSegs(psegs, prefix) {
				return true
			}
			if !anchored {
				for i := 1; i < len(prefix); i++ {
					if matchSegs(psegs, prefix[i:]) {
						return true
					}
				}
			}
		}
	}
	return false
}

func matchSegs(pat, segs []string) bool {
	if len(pat) == 0 {
		return len(segs) == 0
	}
	if pat[0] == "**" {
		for i := 0; i <= len(segs); i++ {
			if matchSegs(pat[1:], segs[i:]) {
				return true
			}
		}
		return false
	}
	if len(segs) == 0 {
		return false
	}
	if ok, err := path.Match(pat[0], segs[0]); err != nil || !ok {
		return false
	}
	return matchSegs(pat[1:], segs[1:])
}
