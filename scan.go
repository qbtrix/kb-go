// Source file discovery for builds: directory walks over comma-separated glob
// patterns (skipping vendor/cache dirs) and git-diff based change detection.

package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
)

// changedFilesSinceRef returns the set of file paths (relative to srcPath)
// that have changed since the given git ref. It unions two sets:
//   - files reported by `git diff --name-only <ref>` (modified vs working tree since ref)
//   - files reported by `git ls-files --others --exclude-standard` (untracked new files)
//
// Using the two-argument form without HEAD lets us pick up both committed
// changes (since ref) and working-tree modifications in a single command.
//
// Paths are normalised with filepath.FromSlash so they match the relPath values
// used elsewhere in the build loop.
//
// The ref is first resolved to a concrete commit SHA via `git rev-parse
// --verify <ref>^{commit}`. This serves two purposes: it validates the ref
// exists, and it prevents an arbitrary ref string starting with "-" from
// being interpreted as a git option flag on the subsequent diff call.
//
// Any failure (git not on PATH, not a git repo, ref doesn't exist or doesn't
// resolve to a commit) returns an error with enough context to emit a useful
// warning.
func changedFilesSinceRef(srcPath, ref string) (map[string]bool, error) {
	result := make(map[string]bool)

	// Resolve ref to a concrete commit SHA first. rev-parse rejects anything
	// it can't interpret as a valid ref — including strings starting with "-"
	// that would otherwise be treated as options on the diff call below.
	shaOut, err := exec.Command("git", "-C", srcPath, "rev-parse", "--verify", ref+"^{commit}").Output()
	if err != nil {
		return nil, fmt.Errorf("git rev-parse %q: %w", ref, err)
	}
	sha := strings.TrimSpace(string(shaOut))

	// git diff --name-only <sha> — files changed between resolved ref and working tree.
	// This picks up both committed changes since ref AND unstaged local edits.
	diffOut, err := exec.Command("git", "-C", srcPath, "diff", "--name-only", sha).Output()
	if err != nil {
		return nil, fmt.Errorf("git diff --name-only %s: %w", sha, err)
	}

	// git ls-files --others --exclude-standard — new untracked files.
	untrackedOut, err := exec.Command("git", "-C", srcPath, "ls-files", "--others", "--exclude-standard").Output()
	if err != nil {
		return nil, fmt.Errorf("git ls-files --others: %w", err)
	}

	for _, line := range strings.Split(string(diffOut), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result[filepath.FromSlash(line)] = true
		}
	}
	for _, line := range strings.Split(string(untrackedOut), "\n") {
		line = strings.TrimSpace(line)
		if line != "" {
			result[filepath.FromSlash(line)] = true
		}
	}
	return result, nil
}

func scanDir(root, pattern string) []string {
	var files []string
	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "__pycache__": true,
		".venv": true, "venv": true, ".tox": true, ".mypy_cache": true,
		"dist": true, "build": true, ".eggs": true, ".pytest_cache": true,
	}

	// Support comma-separated patterns: "*.go,*.py,*.ts"
	patterns := strings.Split(pattern, ",")
	for i := range patterns {
		patterns[i] = strings.TrimSpace(patterns[i])
	}

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || strings.HasPrefix(d.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}

		for _, p := range patterns {
			if matched, _ := filepath.Match(p, d.Name()); matched {
				files = append(files, path)
				break
			}
		}
		return nil
	})

	sort.Strings(files)
	return files
}
