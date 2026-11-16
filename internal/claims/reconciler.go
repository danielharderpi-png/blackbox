// Package claims reconciles an AI session summary against actual file changes.
package claims

import (
	"path"
	"regexp"
	"sort"
	"strings"
)

// Input is what the reconciler compares the summary against.
type Input struct {
	Summary     string
	Changed     []string          // paths changed on disk (added/modified/deleted)
	Known       []string          // all other tracked paths (baseline and current)
	IdentInDiff func(string) bool // reports whether text appears in changed lines
}

// Result is the reconciliation outcome. All slices are sorted and unique.
type Result struct {
	Verified         []string // changed files the summary mentioned
	Unreported       []string // changed files the summary never mentioned
	Phantom          []string // mentioned files with no changes on disk
	IdentsVerified   []string // backticked identifiers found in changed lines
	IdentsUnverified []string // backticked identifiers not found in any changed line
}

var (
	backtickRe = regexp.MustCompile("`([^`\n]+)`")
	lineSufRe  = regexp.MustCompile(`:\d+(:\d+)?$`)
	fileLikeRe = regexp.MustCompile(`^[\w@~./-]+$`)
	extRe      = regexp.MustCompile(`^[a-z][a-z0-9]{0,7}$`)
	identRe    = regexp.MustCompile(`^[A-Za-z_$][\w$]*(\.[A-Za-z_$][\w$]*)*(\(\))?$`)
)

// codeExts are extensions trusted as file mentions even for unknown files.
var codeExts = setOf("go", "js", "jsx", "ts", "tsx", "mjs", "cjs", "py", "rb", "rs", "java", "kt",
	"c", "h", "cc", "cpp", "hpp", "cs", "php", "swift", "sh", "ps1", "bat", "md", "txt", "json",
	"yml", "yaml", "toml", "ini", "cfg", "env", "html", "css", "scss", "sql", "xml", "lock", "mod",
	"sum", "proto", "vue", "svelte", "gitignore")

// prose lists framework names that look like files but are not.
var prose = setOf("node.js", "next.js", "vue.js", "react.js", "express.js", "nuxt.js")

func setOf(s ...string) map[string]bool {
	m := map[string]bool{}
	for _, v := range s {
		m[v] = true
	}
	return m
}

type mention struct {
	text       string
	backticked bool
}

// Reconcile compares the summary's claims with reality.
func Reconcile(in Input) Result {
	var ments []mention
	rest := backtickRe.ReplaceAllStringFunc(in.Summary, func(m string) string {
		ments = append(ments, mention{strings.Trim(m, "`"), true})
		return " "
	})
	for _, tok := range strings.Fields(rest) {
		ments = append(ments, mention{tok, false})
	}

	verified, phantom := map[string]bool{}, map[string]bool{}
	idV, idU := map[string]bool{}, map[string]bool{}

	for _, m := range ments {
		raw := strings.TrimSpace(m.text)
		tok := normalize(raw)
		fileTok := tok != "" && looksLikeFile(tok)
		if fileTok {
			if hits := resolve(tok, in.Changed); len(hits) > 0 {
				for _, h := range hits {
					verified[h] = true
				}
				continue
			}
			if len(resolve(tok, in.Known)) > 0 ||
				(codeExts[ext(tok)] && !prose[strings.ToLower(tok)]) {
				phantom[tok] = true
				continue
			}
		}
		if m.backticked && identRe.MatchString(raw) {
			id := strings.TrimSuffix(raw, "()")
			found := in.IdentInDiff != nil && in.IdentInDiff(id)
			if !found && in.IdentInDiff != nil && strings.Contains(id, ".") {
				found = in.IdentInDiff(id[strings.LastIndex(id, ".")+1:])
			}
			if found {
				idV[id] = true
			} else {
				idU[id] = true
			}
		}
	}

	res := Result{
		Verified:         keys(verified),
		Phantom:          keys(phantom),
		IdentsVerified:   keys(idV),
		IdentsUnverified: keys(idU),
	}
	for _, c := range in.Changed {
		if !verified[c] {
			res.Unreported = append(res.Unreported, c)
		}
	}
	sort.Strings(res.Unreported)
	return res
}

// normalize strips surrounding punctuation, ./ prefixes and :line suffixes.
func normalize(tok string) string {
	tok = strings.Trim(tok, "\"'()[]{}<>,;:!?*")
	tok = strings.TrimRight(tok, ".")
	if strings.Contains(tok, "://") {
		return ""
	}
	tok = lineSufRe.ReplaceAllString(tok, "")
	tok = strings.ReplaceAll(tok, "\\", "/")
	for strings.HasPrefix(tok, "./") {
		tok = tok[2:]
	}
	return tok
}

func ext(tok string) string { return strings.ToLower(strings.TrimPrefix(path.Ext(tok), ".")) }

// looksLikeFile reports whether tok has the shape name.ext (or a path).
func looksLikeFile(tok string) bool {
	if !fileLikeRe.MatchString(tok) || !extRe.MatchString(ext(tok)) {
		return false
	}
	base := path.Base(tok)
	if strings.TrimSuffix(base, path.Ext(base)) == "" && !strings.HasPrefix(base, ".") {
		return false
	}
	switch strings.ToLower(tok) {
	case "e.g", "i.e":
		return false
	}
	return true
}

// resolve returns the paths that tok refers to (exact or path-suffix match).
func resolve(tok string, paths []string) []string {
	var out []string
	for _, p := range paths {
		if p == tok || strings.HasSuffix(p, "/"+tok) {
			out = append(out, p)
		}
	}
	return out
}

func keys(m map[string]bool) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}
