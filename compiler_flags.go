// CLI side of the `--compiler` hook: resolves --compiler / KB_COMPILER and
// --compiler-timeout from the command line, rejects the removed --model flag,
// and exits 2 with guidance when a command needs a compiler and has none.

package main

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// compilerFromArgs resolves the hook: --compiler wins over KB_COMPILER.
// --compiler-timeout takes a Go duration ("90s", "2m") or whole seconds.
func compilerFromArgs(args []string) (compilerSpec, error) {
	spec := compilerSpec{
		Command: flagStr(args, "--compiler", os.Getenv("KB_COMPILER")),
		Timeout: defaultCompilerTimeout,
	}
	if raw := flagStr(args, "--compiler-timeout", ""); raw != "" {
		d, err := time.ParseDuration(raw)
		if err != nil {
			n, nerr := strconv.Atoi(raw)
			if nerr != nil {
				return spec, fmt.Errorf("--compiler-timeout %q: want seconds or a duration like 90s", raw)
			}
			d = time.Duration(n) * time.Second
		}
		if d <= 0 {
			return spec, fmt.Errorf("--compiler-timeout must be positive")
		}
		spec.Timeout = d
	}
	return spec, nil
}

// mustCompilerFromArgs is compilerFromArgs for commands: a bad flag is a usage
// error (exit 2), and the removed --model flag is rejected with a pointer to
// --compiler instead of being silently ignored.
func mustCompilerFromArgs(args []string) compilerSpec {
	if flagBool(args, "--model") {
		usageExit("--model was removed in kb v0.4.0: kb no longer calls an LLM itself.\n" +
			"  Pick the model inside your --compiler command (e.g. `claude -p --model haiku ...`,\n" +
			"  or KB_COMPILE_MODEL for examples/compilers/openai_compatible.py).")
	}
	spec, err := compilerFromArgs(args)
	if err != nil {
		usageExit(err.Error())
	}
	return spec
}

// requireCompiler exits 2 with guidance when no compiler is configured.
func requireCompiler(spec compilerSpec, command, alternative string) {
	if spec.enabled() {
		return
	}
	msg := fmt.Sprintf("kb %s needs a compiler: kb does not call an LLM itself.\n"+
		"  Pass --compiler \"<command>\" (or set KB_COMPILER): kb writes each prompt to the\n"+
		"  command's stdin and reads one JSON article from its stdout.\n", command)
	if alternative != "" {
		msg += "  " + alternative + "\n"
	}
	msg += "  Recipes: README.md, \"Compiling articles\"."
	usageExit(msg)
}

func usageExit(msg string) {
	fmt.Fprintln(os.Stderr, "Error: "+msg)
	os.Exit(2)
}
