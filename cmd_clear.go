// Implements `kb clear`.

package main

import (
	"fmt"
	"os"

	"github.com/qbtrix/kb-go/internal/store"
)

func cmdClear(args []string) {
	scope := flagStr(args, "--scope", "default")
	jsonOut := flagBool(args, "--json")

	root := store.ScopeDir(scope)
	os.RemoveAll(root)
	store.EnsureDirs(scope)

	if jsonOut {
		printJSON(map[string]any{"ok": true, "scope": scope})
	} else {
		fmt.Printf("Cleared knowledge base: %s\n", scope)
	}
}
