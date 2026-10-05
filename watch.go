// `kb watch`: rebuilds a scope when source files change (fsnotify).

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/fsnotify/fsnotify"
)

func cmdWatch(args []string) {
	if len(args) < 1 {
		fatal("Usage: kb watch <path> [--scope NAME] [--pattern GLOB] [--model MODEL]")
	}

	path := args[0]
	scope := flagStr(args, "--scope", filepath.Base(path))
	pattern := flagStr(args, "--pattern", "*.py")
	model := flagStr(args, "--model", defaultModel)

	absPath, err := filepath.Abs(path)
	if err != nil {
		fatal("Invalid path: %s", path)
	}

	fmt.Printf("Watching %s (scope: %s, pattern: %s)\n", absPath, scope, pattern)
	fmt.Print("Press Ctrl+C to stop.\n\n")

	// Initial build (non-fatal — dir may be empty initially)
	files := scanDir(absPath, pattern)
	if len(files) > 0 {
		fmt.Println("Running initial build...")
		cmdBuild([]string{absPath, "--scope", scope, "--pattern", pattern, "--model", model})
		fmt.Println()
	} else {
		fmt.Println("No matching files yet. Waiting for changes...")
	}

	watcher, err := newRecursiveWatcher(absPath)
	if err != nil {
		fatal("Failed to create watcher: %v", err)
	}
	defer watcher.Close()

	debounce := time.NewTimer(0)
	if !debounce.Stop() {
		<-debounce.C
	}

	for {
		select {
		case event, ok := <-watcher.Events:
			if !ok {
				return
			}
			if event.Op&(fsnotify.Write|fsnotify.Create) == 0 {
				continue
			}
			matched, _ := filepath.Match(pattern, filepath.Base(event.Name))
			if !matched {
				continue
			}
			// Debounce: wait 3s after last change before rebuilding
			debounce.Reset(3 * time.Second)

		case err, ok := <-watcher.Errors:
			if !ok {
				return
			}
			fmt.Fprintf(os.Stderr, "Watcher error: %v\n", err)

		case <-debounce.C:
			fmt.Printf("[%s] Change detected, rebuilding...\n", time.Now().Format("15:04:05"))
			cmdBuild([]string{absPath, "--scope", scope, "--pattern", pattern, "--model", model})
			fmt.Println()
		}
	}
}

func newRecursiveWatcher(root string) (*fsnotify.Watcher, error) {
	watcher, err := fsnotify.NewWatcher()
	if err != nil {
		return nil, err
	}

	skipDirs := map[string]bool{
		".git": true, "node_modules": true, "__pycache__": true,
		".venv": true, "venv": true, ".tox": true, ".mypy_cache": true,
		"dist": true, "build": true, ".eggs": true, ".pytest_cache": true,
	}

	filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			if skipDirs[d.Name()] || (strings.HasPrefix(d.Name(), ".") && path != root) {
				return filepath.SkipDir
			}
			watcher.Add(path)
		}
		return nil
	})

	return watcher, nil
}
