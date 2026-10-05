// CLI side of compile-path selection: resolves --compiler / KB_COMPILER,
// --compiler-timeout and the built-in client's settings (ANTHROPIC_API_KEY,
// --model, ANTHROPIC_BASE_URL) from the command line, rejects --model together
// with a compiler, and exits 2 with guidance when a command needs a compile
// path and has none.

package main

import (
	"fmt"
	"os"
	"strconv"
	"time"

	"github.com/qbtrix/kb-go/internal/compile"
)

// compilerFromArgs resolves the compile path: --compiler wins over
// KB_COMPILER, and either wins over the built-in client (ANTHROPIC_API_KEY,
// --model, ANTHROPIC_BASE_URL). --compiler-timeout takes a Go duration
// ("90s", "2m") or whole seconds. --model together with a compiler is an
// error: the compiler picks its own model.
func compilerFromArgs(args []string) (compile.Spec, error) {
	spec := compile.Spec{
		Command: flagStr(args, "--compiler", os.Getenv("KB_COMPILER")),
		Timeout: compile.DefaultTimeout,
		APIKey:  os.Getenv("ANTHROPIC_API_KEY"),
		Model:   flagStr(args, "--model", compile.DefaultModel),
		BaseURL: compile.BaseURLFromEnv(),
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
	if flagBool(args, "--model") && spec.Hook() {
		from := "KB_COMPILER is set"
		if flagBool(args, "--compiler") {
			from = "--compiler is given"
		}
		return spec, fmt.Errorf("--model applies to the built-in Anthropic client, but %s: the\n"+
			"  compiler picks its own model. Drop --model (set the model inside the compiler\n"+
			"  command), or drop --compiler and unset KB_COMPILER to use the built-in client.", from)
	}
	return spec, nil
}

// mustCompilerFromArgs is compilerFromArgs for commands: a bad flag or flag
// combination is a usage error (exit 2).
func mustCompilerFromArgs(args []string) compile.Spec {
	spec, err := compilerFromArgs(args)
	if err != nil {
		usageExit(err.Error())
	}
	return spec
}

// requireCompiler exits 2 with guidance listing every way to compile when no
// compile path is configured. alternative replaces the default third option
// (agent mode) where another escape hatch fits the command better.
func requireCompiler(spec compile.Spec, command, alternative string) {
	if spec.Enabled() {
		return
	}
	if alternative == "" {
		alternative = "Compile in your own agent: `kb prepare` emits the prompts, `kb accept` stores the articles."
	}
	usageExit(fmt.Sprintf("kb %s needs an LLM to compile articles. Pick one:\n"+
		"  1. Set ANTHROPIC_API_KEY to use the built-in Anthropic client (--model picks the\n"+
		"     model; ANTHROPIC_BASE_URL routes it through a proxy such as LiteLLM).\n"+
		"  2. Bring your own compiler: pass --compiler \"<command>\" (or set KB_COMPILER). kb\n"+
		"     writes each prompt to its stdin and reads one JSON article from its stdout.\n"+
		"  3. %s\n"+
		"  Recipes: README.md, \"Compiling articles\".", command, alternative))
}

func usageExit(msg string) {
	fmt.Fprintln(os.Stderr, "Error: "+msg)
	os.Exit(2)
}
