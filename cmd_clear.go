// Implements `kb clear`.

package main

import (
	"fmt"
	"os"
)

func cmdClear(args []string) {
	scope := flagStr(args, "--scope", "default")
	jsonOut := flagBool(args, "--json")

	root := scopeDir(scope)
	os.RemoveAll(root)
	ensureDirs(scope)

	if jsonOut {
		printJSON(map[string]any{"ok": true, "scope": scope})
	} else {
		fmt.Printf("Cleared knowledge base: %s\n", scope)
	}
}
