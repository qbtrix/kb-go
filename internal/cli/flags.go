// Minimal flag parsing for the CLI (no external deps), and fatal() for
// print-and-exit errors.

package cli

import (
	"fmt"
	"os"
)

func flagStr(args []string, name, defaultVal string) string {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			return args[i+1]
		}
	}
	return defaultVal
}

func flagBool(args []string, name string) bool {
	for _, a := range args {
		if a == name {
			return true
		}
	}
	return false
}

func flagInt(args []string, name string, defaultVal int) int {
	for i, a := range args {
		if a == name && i+1 < len(args) {
			v := 0
			fmt.Sscanf(args[i+1], "%d", &v)
			if v > 0 {
				return v
			}
		}
	}
	return defaultVal
}

func fatal(format string, a ...any) {
	fmt.Fprintf(os.Stderr, "Error: "+format+"\n", a...)
	os.Exit(1)
}
