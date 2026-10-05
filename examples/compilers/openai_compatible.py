#!/usr/bin/env python3
"""kb compiler for any OpenAI-compatible chat endpoint (LiteLLM proxy, LM Studio,
Ollama, vLLM, OpenAI itself).

kb runs this once per article: the compile prompt arrives on stdin, and this
prints ONE JSON object on stdout with a `usage` block filled from the
response, so `kb stats` can total the spend. A JSON array (the `kb lint --llm`
review) is printed unchanged. Any failure exits non-zero with a message on
stderr; kb then writes no article for that item.

Environment:
  OPENAI_BASE_URL    required, e.g. http://localhost:4000/v1 (LiteLLM proxy),
                     http://localhost:1234/v1 (LM Studio),
                     http://localhost:11434/v1 (Ollama)
  KB_COMPILE_MODEL   required, the model name the endpoint knows
  OPENAI_API_KEY     optional bearer token (LiteLLM virtual key, OpenAI key)
  KB_COMPILE_MAX_TOKENS  optional, default 4096
  KB_COMPILE_COST_IN / KB_COMPILE_COST_OUT  optional USD per million tokens,
                     used for cost_usd when the endpoint doesn't report a cost
                     (a LiteLLM proxy does, in the x-litellm-response-cost header)

Usage:
  kb build ./docs --scope docs --compiler "python examples/compilers/openai_compatible.py"

Standard library only; Python 3.8+.
"""
import http.client
import json
import os
import re
import sys
import urllib.error
import urllib.request

SYSTEM = "You are a knowledge compiler. Output only valid JSON. No markdown fences."


def fail(msg):
    print(msg, file=sys.stderr)
    sys.exit(1)


def extract_json(text):
    """Parse the model's answer: drop <think> blocks and ``` fences, then try
    the whole text, then the outermost {...} or [...] span."""
    text = re.sub(r"(?s)<think>.*?</think>", "", text).strip()
    text = re.sub(r"^```(?:json)?\s*|\s*```$", "", text).strip()
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
    base = os.environ.get("OPENAI_BASE_URL", "").rstrip("/")
    model = os.environ.get("KB_COMPILE_MODEL", "")
    if not base or not model:
        fail("set OPENAI_BASE_URL and KB_COMPILE_MODEL")
    prompt = sys.stdin.buffer.read().decode("utf-8")
    if not prompt.strip():
        fail("empty prompt on stdin")

    body = json.dumps({
        "model": model,
        "messages": [
            {"role": "system", "content": SYSTEM},
            {"role": "user", "content": prompt},
        ],
        "max_tokens": int(os.environ.get("KB_COMPILE_MAX_TOKENS", "4096")),
        "temperature": 0.2,
    }).encode("utf-8")
    req = urllib.request.Request(base + "/chat/completions", data=body, method="POST")
    req.add_header("Content-Type", "application/json")
    if os.environ.get("OPENAI_API_KEY"):
        req.add_header("Authorization", "Bearer " + os.environ["OPENAI_API_KEY"])
    try:
        with urllib.request.urlopen(req) as resp:
            data = json.loads(resp.read().decode("utf-8"))
            header_cost = resp.headers.get("x-litellm-response-cost")
    except urllib.error.HTTPError as e:
        fail("HTTP %d from %s: %s" % (e.code, base, e.read().decode("utf-8", "replace")[:500]))
    except urllib.error.URLError as e:
        fail("cannot reach %s: %s" % (base, e.reason))
    except (OSError, http.client.HTTPException, ValueError) as e:
        fail("request to %s failed: %s" % (base, e))

    try:
        content = data["choices"][0]["message"]["content"] or ""
    except (KeyError, IndexError, TypeError):
        fail("unexpected response shape: %r" % str(data)[:300])
    result = extract_json(content)

    if isinstance(result, dict):
        u = data.get("usage") or {}
        usage = {
            "model": data.get("model") or model,
            "input_tokens": int(u.get("prompt_tokens") or 0),
            "output_tokens": int(u.get("completion_tokens") or 0),
        }
        cost = None
        if header_cost:
            try:
                cost = float(header_cost)
            except ValueError:
                cost = None
        if cost is None and os.environ.get("KB_COMPILE_COST_IN"):
            cost = (usage["input_tokens"] * float(os.environ["KB_COMPILE_COST_IN"])
                    + usage["output_tokens"] * float(os.environ.get("KB_COMPILE_COST_OUT", "0"))) / 1e6
        if cost is not None:
            usage["cost_usd"] = cost
        result["usage"] = usage
    sys.stdout.write(json.dumps(result) + "\n")


if __name__ == "__main__":
    main()
