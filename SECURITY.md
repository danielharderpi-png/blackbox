# Security Policy

## Scope and design

`blackbox` runs entirely on your machine. It makes no network connections,
collects no telemetry, and has no external dependencies (Go standard library
only). All state lives in `.blackbox/` inside your repository, which is
git-ignored by `blackbox init`.

What it does touch:

- **Reads** every non-ignored regular file under the directory where you run
  `blackbox arm`, and stores gzip copies in `.blackbox/objects/`. Those copies
  can contain secrets that exist in your working tree. Treat `.blackbox/` like
  the repo itself and never commit or share it.
- **Writes** only inside the repository, and only in two cases: `arm` (writes
  `.blackbox/`) and `revert <file>` (restores or deletes that one file).
- **Symlinks and junctions** are never followed. `arm` skips them (warning if
  they point outside the repo) and `revert` refuses to read, overwrite or
  delete anything reached through one that escapes the repository.

## Limits

`blackbox` is an audit tool, not a sandbox. It detects and reports what changed
between `arm` and `verify`; it does not stop a process from changing files. A
session that edits files and then restores them byte-for-byte before `verify`
is indistinguishable from one that never touched them.

## Reporting a vulnerability

Please report security issues privately, not in a public issue. Use GitHub's
private vulnerability reporting:
<https://github.com/danielharderpi-png/blackbox/security/advisories/new>

Include the version or commit, your OS, and steps to reproduce. You can expect
an acknowledgement within a few days. Please allow time for a fix before
disclosing publicly.
