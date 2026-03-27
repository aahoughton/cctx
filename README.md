# cctx

A CLI for browsing and managing Claude Code's local conversation history.

Claude Code stores everything locally as JSONL files under `~/.claude/projects/`,
but provides no built-in way to search, browse, or manage that data across
sessions. cctx fills that gap.

## Examples

See what you've been working on:

```
$ cctx convs
SESSION   MSGS  MODIFIED              SUMMARY
0c5dc16b  270   2026-03-27T15:33:19Z  native anthropic sdk support
bd66e34e  33    2026-03-27T04:16:56Z  conversation naming best practices
29e5b4b2  123   2026-03-27T02:46:41Z  llm summarization debugging
```

Find that conversation where you fixed the auth bug last week:

```
$ cctx search -f "auth middleware"
4 hit(s) across 2 conversation(s):

  a1b2c3d4  2026-03-20T11:42:00Z  auth middleware rewrite
    [user] the old auth middleware is storing session tokens in a way that doesn't meet compliance...
    [assistant] I'll refactor the middleware to use encrypted session storage. The main changes are in...
    [user] looks good but we also need to handle the refresh token rotation

  e5f6a7b8  2026-03-18T09:15:00Z  api endpoint security audit
    [assistant] Found three issues in the auth middleware: 1) session tokens stored in plaintext...
```

Get a detailed summary of what happened in a session:

```
$ cctx show --llm a1b2c3d4
Session:  a1b2c3d4-...
Modified: 2026-03-20 11:42:00
Messages: 45

Rewrote the auth middleware to use encrypted session storage, driven by
a compliance requirement around token handling. The main changes were in
middleware/auth.go and session/store.go, replacing the plaintext cookie
approach with AES-GCM encrypted tokens and adding refresh token rotation.

Also updated the test suite to use real database connections instead of
mocks, after discovering that the previous mock-based tests had masked
a migration issue in production.
```

Give your sessions meaningful names in bulk:

```
$ cctx rename -a
Found 12 unnamed conversation(s) in /Users/me/src/myproject

  a1b2c3d4  parsed-tumbling-lantern    -> auth middleware rewrite
  e5f6a7b8  spicy-exploring-pebble     -> api endpoint security audit
  ...

Renamed 12 conversation(s).
```

## Install

```sh
go install github.com/aahoughton/cctx@latest
# or
go build -o cctx . && cp cctx ~/bin/

# Shell completions
cctx completion fish > ~/.config/fish/completions/cctx.fish
cctx completion bash > /etc/bash_completion.d/cctx
cctx completion zsh > "${fpath[1]}/_cctx"
```

## Commands

```sh
cctx ls                              # list projects
cctx ls -o                           # orphaned projects only
cctx convs                           # list conversations (cwd project)
cctx convs -p ~/other/project        # specific project

cctx show abc123                     # conversation digest
cctx show -f abc123                  # full transcript
cctx show --llm abc123               # LLM-generated narrative summary

cctx rename abc123 "my new name"     # manual rename
cctx rename abc123                   # auto-name via LLM
cctx rename -a                       # batch-rename all unnamed
cctx rename -an                      # preview batch rename

cctx search "auth middleware"        # search metadata (fast)
cctx search -f "handleRequest"      # full-text search
cctx search -A "refactor"           # search all projects
cctx search -E "fix(ed|ing) bug"    # regex
cctx search -u "please add"         # user messages only
cctx search -a "created file"       # assistant messages only
cctx search -l "auth"               # session IDs only (grep -l style)

cctx rm                              # preview project removal
cctx rm -x                           # apply
cctx mv ~/old/path ~/new/path       # preview path update
cctx mv -x ~/old/path ~/new/path    # apply
cctx merge ~/orphaned ~/current     # preview merge
cctx merge -x ~/orphaned ~/current  # apply
cctx prune                           # preview empty conversation removal
cctx prune -Ax                       # apply across all projects
```

All destructive operations are dry-run by default and require `-x` to apply.

The `-p` flag scopes any command to a specific project (defaults to cwd).

## LLM Configuration

`rename` and `show --llm` use an LLM for summarization. Claude models (any
model starting with `claude-`) use the Anthropic API natively; all others
use the OpenAI-compatible chat completions API (Ollama, LM Studio, vLLM,
OpenAI, Groq, Together, etc).

Configure via (in priority order): flags, environment variables, or config file.

**Config file** (`~/.config/cctx/config.toml`):

```toml
[llm]
url = "http://localhost:11434/v1"   # Ollama, LM Studio, etc.
model = "qwen3-4"                   # fast, good instruction following
api_key = ""                        # optional for local models
```

For Claude models, just set the model — the API key is picked up from
the environment:

```toml
[llm]
model = "claude-haiku-4-5"
# api_key from ANTHROPIC_API_KEY env var
```

**Environment variables**:

```sh
export CONTEXT_LLM_URL=http://localhost:11434/v1
export CONTEXT_LLM_MODEL=llama3
# API key: LLM_API_KEY, OPENAI_API_KEY, or ANTHROPIC_API_KEY
```

**Flags**: `-U`, `-M`, `-K` override everything (available on `rename`
and `show`).

## How it works

Claude Code stores conversations under `~/.claude/projects/` in directories
named by encoding the project's filesystem path (e.g. `/Users/me/src/foo`
becomes `-Users-me-src-foo`). Each conversation is a JSONL file with a UUID
session ID.

The encoding is lossy — literal hyphens in path components are
indistinguishable from directory separators. cctx resolves this ambiguity
by checking `sessions-index.json`, conversation `cwd` fields, and
filesystem probing, in that order.

### Schema versioning

cctx checks the `version` field in `sessions-index.json` against an
expected constant. If Claude changes its storage format, operations fail
with a clear error rather than silently corrupting data. When the index is
missing or incompatible, cctx falls back to parsing JSONL files directly.
