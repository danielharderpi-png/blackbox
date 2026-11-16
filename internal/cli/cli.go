// Package cli implements the blackbox command line (shared by the blackbox
// and bbox binaries).
package cli

import (
	"bufio"
	"errors"
	"flag"
	"fmt"
	"io"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/danielharderpi-png/blackbox/internal/claims"
	"github.com/danielharderpi-png/blackbox/internal/diff"
)

const policyFile = diff.PolicyFile

const defaultPolicy = `# blackbox policy
version: 1

# Glob patterns excluded from snapshots. A leading "/" anchors to the repo
# root; otherwise patterns match at any depth. "**" spans directories.
ignore:
  - .git/**
  - .blackbox/**
  - node_modules/**
  - .venv/**
  - __pycache__/**
  - dist/**
  - build/**
  - .next/**
  - vendor/**

# Paths the AI session must not touch (same glob syntax). Any added,
# modified or deleted match fails ` + "`blackbox verify`" + `.
no_fly_zones:
  - .env
  - .env.*
  - "**/*.pem"
  - "**/*.key"
  - .github/workflows/**

# Regular expressions (Go syntax). Any newly added line that matches fails
# ` + "`blackbox verify`" + `.
sensitive_patterns:
  - 'AKIA[0-9A-Z]{16}'
  - '-----BEGIN [A-Z ]*PRIVATE KEY-----'
  - '(?i)(api[_-]?key|secret|token|password)\s*[:=]\s*["''][^"'']{8,}["'']'
`

// errViolations signals a policy failure (exit code 1) after output is printed.
var errViolations = errors.New("policy violations")

// Main runs the CLI with os.Args and exits the process.
func Main() {
	if len(os.Args) < 2 {
		usage(os.Stderr)
		os.Exit(2)
	}
	var err error
	switch os.Args[1] {
	case "init":
		err = cmdInit()
	case "arm":
		err = cmdArm()
	case "verify":
		err = cmdVerify(os.Args[2:])
	case "revert":
		err = cmdRevert(os.Args[2:])
	case "-h", "--help", "help":
		usage(os.Stdout)
		return
	default:
		fmt.Fprintf(os.Stderr, "blackbox: unknown command %q\n\n", os.Args[1])
		usage(os.Stderr)
		os.Exit(2)
	}
	switch {
	case err == nil:
	case errors.Is(err, errViolations):
		os.Exit(1)
	case errors.Is(err, flag.ErrHelp):
	default:
		fmt.Fprintln(os.Stderr, "blackbox:", err)
		os.Exit(2)
	}
}

func usage(w io.Writer) {
	fmt.Fprint(w, `blackbox (bbox) - flight recorder for AI coding sessions

Usage:
  blackbox <command>

Commands:
  init     Create .blackbox.yml and add .blackbox/ to .gitignore
  arm      Capture a pre-session snapshot into .blackbox/flight.json
  verify   Audit changes since arm [--summary "..."] [--summary-file <path>]
  revert   Restore one file to its armed baseline: revert <file>

Exit codes: 0 ok, 1 policy violations (verify), 2 error.
`)
}

func cmdInit() error {
	if _, err := os.Stat(policyFile); err == nil {
		fmt.Println(policyFile, "already exists, leaving unchanged")
	} else if errors.Is(err, fs.ErrNotExist) {
		if err := os.WriteFile(policyFile, []byte(defaultPolicy), 0o644); err != nil {
			return err
		}
		fmt.Println("created", policyFile)
	} else {
		return err
	}
	added, err := ensureGitignore(diff.StateDir + "/")
	if err != nil {
		return err
	}
	if added {
		fmt.Println("added", diff.StateDir+"/", "to .gitignore")
	} else {
		fmt.Println(".gitignore already ignores", diff.StateDir+"/")
	}
	return nil
}

func ensureGitignore(entry string) (bool, error) {
	data, err := os.ReadFile(".gitignore")
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	sc := bufio.NewScanner(strings.NewReader(string(data)))
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == entry || l == strings.TrimSuffix(entry, "/") || l == "/"+entry {
			return false, nil
		}
	}
	content := string(data)
	if content != "" && !strings.HasSuffix(content, "\n") {
		content += "\n"
	}
	content += entry + "\n"
	return true, os.WriteFile(".gitignore", []byte(content), 0o644)
}

func cmdArm() error {
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	flight, warns, err := diff.Arm(root, gitHead())
	if err != nil {
		return err
	}
	for _, w := range warns {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	fmt.Printf("armed: %d files, tree %s\n", flight.FileCount, flight.TreeHash[:12])
	return nil
}

// gitHead returns the current git commit, or "" if unavailable.
func gitHead() string {
	out, err := exec.Command("git", "rev-parse", "HEAD").Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

func cmdVerify(args []string) error {
	fs := flag.NewFlagSet("verify", flag.ContinueOnError)
	summary := fs.String("summary", "", "AI session summary text")
	summaryFile := fs.String("summary-file", "", "read the AI session summary from `path`")
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return fmt.Errorf("verify: unexpected argument %q", fs.Arg(0))
	}
	text := *summary
	if *summaryFile != "" {
		b, err := os.ReadFile(*summaryFile)
		if err != nil {
			return err
		}
		text += "\n" + string(b)
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	rep, err := diff.Analyze(root)
	if err != nil {
		return err
	}
	printReceipt(rep, text)
	if len(rep.Violations) > 0 {
		return errViolations
	}
	return nil
}

func printReceipt(rep *diff.Report, summary string) {
	for _, w := range rep.Warnings {
		fmt.Fprintln(os.Stderr, "warning:", w)
	}
	rule := strings.Repeat("=", 64)
	thin := strings.Repeat("-", 64)
	fmt.Println(rule)
	fmt.Println(" BLACKBOX AUDIT RECEIPT")
	fmt.Printf(" armed %s (%s ago)\n", rep.Flight.ArmedAt.Local().Format("2006-01-02 15:04:05"),
		time.Since(rep.Flight.ArmedAt).Round(time.Second))
	fmt.Println(rule)

	fmt.Printf("\nPOLICY / RISK ALERTS (%d)\n%s\n", len(rep.Violations), thin)
	if len(rep.Violations) == 0 {
		fmt.Println("  none")
	}
	for _, v := range rep.Violations {
		switch v.Kind {
		case "policy_tamper":
			fmt.Printf("  [POLICY TAMPER] %s was %s during the session\n", v.Path, v.Status)
		case "no_fly_zone":
			fmt.Printf("  [NO-FLY]    %s (%s)\n", v.Path, v.Status)
		default:
			fmt.Printf("  [SENSITIVE] %s:%d (%s) matches /%s/\n", v.Path, v.Line, v.Status, v.Pattern)
		}
	}

	if strings.TrimSpace(summary) != "" {
		changed := make([]string, len(rep.Changes))
		for i, c := range rep.Changes {
			changed[i] = c.Path
		}
		var known []string
		for p := range rep.Flight.Files {
			known = append(known, p)
		}
		res := claims.Reconcile(claims.Input{
			Summary: summary, Changed: changed, Known: known,
			IdentInDiff: func(s string) bool {
				for _, c := range rep.Changes {
					for _, l := range c.AddedLines {
						if strings.Contains(l, s) {
							return true
						}
					}
					for _, l := range c.RemovedLines {
						if strings.Contains(l, s) {
							return true
						}
					}
				}
				return false
			},
		})
		fmt.Printf("\nCLAIM vs REALITY\n%s\n", thin)
		printList("verified claims", "OK", res.Verified)
		printList("phantom claims (mentioned, no changes on disk)", "!!", res.Phantom)
		printList("unreported mutations (changed, never mentioned)", "!!", res.Unreported)
		printList("identifiers found in diff", "OK", res.IdentsVerified)
		printList("identifiers NOT found in any changed line", "??", res.IdentsUnverified)
	}

	add, rem := rep.Totals()
	fmt.Printf("\nACTUAL FILE DIFFS (%d files, +%d/-%d)\n%s\n", len(rep.Changes), add, rem, thin)
	if len(rep.Changes) == 0 {
		fmt.Println("  no changes since arm")
	}
	for _, c := range rep.Changes {
		var tag, stat string
		switch c.Status {
		case diff.Added:
			tag = "A"
		case diff.Modified:
			tag = "M"
		default:
			tag = "D"
		}
		switch {
		case c.Binary:
			stat = "binary"
		case !c.LinesKnown:
			stat = "line counts unavailable (baseline not stored)"
		default:
			stat = fmt.Sprintf("+%d/-%d", c.Added, c.Removed)
			if c.Approx {
				stat += " (approx)"
			}
		}
		fmt.Printf("  %s  %-40s %s\n", tag, c.Path, stat)
	}
	fmt.Println()
	if len(rep.Violations) > 0 {
		fmt.Printf("RESULT: FAIL (%d policy violations)\n", len(rep.Violations))
	} else {
		fmt.Println("RESULT: PASS")
	}
}

func printList(title, mark string, items []string) {
	if len(items) == 0 {
		return
	}
	fmt.Printf("  %s (%d)\n", title, len(items))
	for _, it := range items {
		fmt.Printf("    [%s] %s\n", mark, it)
	}
}

func cmdRevert(args []string) error {
	fs := flag.NewFlagSet("revert", flag.ContinueOnError)
	if err := fs.Parse(args); err != nil {
		return err
	}
	if fs.NArg() != 1 {
		return errors.New("usage: blackbox revert <file>")
	}
	root, err := os.Getwd()
	if err != nil {
		return err
	}
	res, err := diff.Revert(root, fs.Arg(0))
	if err != nil {
		return err
	}
	fmt.Printf("%s %s\n", res, filepath.ToSlash(fs.Arg(0)))
	return nil
}
