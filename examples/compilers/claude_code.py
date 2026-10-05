#!/usr/bin/env python3
"""kb compiler that runs headless Claude Code (`claude -p`) on the user's own
Claude login: no API key, no tools, no MCP, no CLAUDE.md, no user settings,
no extended thinking.

kb runs this once per article: the compile prompt arrives on stdin; this runs
`claude -p --output-format json`, takes the model's answer, and prints ONE JSON
object with a `usage` block (model, input tokens incl. cache, output tokens,
total_cost_usd as Claude Code reports it), so `kb stats` can total the spend.
A JSON array (the `kb lint --llm` review) is printed unchanged. Failures exit
non-zero with a message on stderr.

If you don't need usage numbers, skip this script and pass the plain
`claude -p ...` command from README "Compiling articles" as --compiler.

Environment:
  KB_COMPILE_MODEL  Claude model alias or id (default: haiku)
  CLAUDE_BIN        path to the claude executable (default: claude)

Usage:
  kb build ./docs --scope docs --terse --compiler "python examples/compilers/claude_code.py"

Standard library only; Python 3.8+.
"""
import json
import os
import re
import subprocess
import sys

SYSTEM = "You are a knowledge compiler. Output only valid JSON. No markdown fences."


def fail(msg):
    print(msg, file=sys.stderr)
    sys.exit(1)


def extract_json(text):
    """Parse the model's answer: drop ``` fences, then try the whole text,
    then the outermost {...} or [...] span."""
    text = re.sub(r"^```(?:json)?\s*|\s*```$", "", text.strip()).strip()
    try:
        return json.loads(text)
    except ValueError:
        pass
    for open_c, close_c in (("{", "}"), ("[", "]")):
        i, j = text.find(open_c), text.rfind(close_c)
        if i >= 0 and j > i:
            try:
                return json.loads(text[i : j + 1])
            except ValueError:
                continue
    fail("model output is not JSON; starts: %r" % text[:200])


def main():
    prompt = sys.stdin.buffer.read()
    if not prompt.strip():
        fail("empty prompt on stdin")
    model = os.environ.get("KB_COMPILE_MODEL", "haiku")
    cmd = [
        os.environ.get("CLAUDE_BIN", "claude"), "-p",
        "--safe-mode",               # no CLAUDE.md, skills, plugins, hooks, MCP servers
        "--setting-sources", "",     # no user/project/local settings (e.g. an advisor model)
        "--strict-mcp-config",
        "--tools", "",               # no tools: one turn, prompt in, JSON out
        "--settings", '{"alwaysThinkingEnabled":false}',
        "--no-session-persistence",
        "--model", model,
        "--system-prompt", SYSTEM,
        "--output-format", "json",
    ]
    proc = subprocess.run(cmd, input=prompt, capture_output=True)
    if proc.returncode != 0:
        fail("claude exited %d: %s" % (proc.returncode, proc.stderr.decode("utf-8", "replace")[:500]))
    try:
        env = json.loads(proc.stdout.decode("utf-8"))
    except ValueError:
        fail("claude did not print JSON: %r" % proc.stdout[:300])
    if env.get("is_error"):
        fail("claude reported an error: %s" % str(env.get("result"))[:500])

    result = extract_json(env.get("result") or "")
    if isinstance(result, dict):
        u = env.get("usage") or {}
        models = list((env.get("modelUsage") or {}).keys())
        result["usage"] = {
            "model": "+".join(models) or model,
            "input_tokens": int(u.get("input_tokens") or 0)
            + int(u.get("cache_read_input_tokens") or 0)
            + int(u.get("cache_creation_input_tokens") or 0),
            "output_tokens": int(u.get("output_tokens") or 0),
            "cost_usd": float(env.get("total_cost_usd") or 0),
        }
    sys.stdout.write(json.dumps(result) + "\n")


if __name__ == "__main__":
    main()
