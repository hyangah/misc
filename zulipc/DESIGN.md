# Design Document: `zulipc`

**Package**: `github.com/hyangah/misc/zulipc`  
**Status**: Draft  
**Target Go Version**: Go 1.26+  

---

## 1. Overview & Motivation

The Go project and contributor community use [Zulip](https://zulip.com) for real-time discussions, design proposals, issue coordination, and release management. While Zulip provides a rich web application, developers and LLM agents need a fast, lightweight command-line interface to:

1. **Read threads and topics directly from Zulip URLs** (e.g., links shared in GitHub issues, Gerrit reviews, or chat).
2. **Search and discover active topics and channels** without leaving the terminal or context window.
3. **Provide structured, clean Markdown output** consumable by humans and LLMs with local timezone support.
4. **Self-document agent capabilities** via a built-in `zulipc skill` command (modeled after `rsc.io/cmd/review`'s `review skills`).

`zulipc` is a standalone Go CLI tool designed specifically for developer workflows and agent integrations.

---

## 2. Goals & Non-Goals

### Goals
- **Read-Only MVP**: Prioritize robust, high-fidelity thread reading, search, channel/topic discovery, and auth diagnostics.
- **First-Class URL Support**: Parse standard Zulip URL fragments, including channel, stream, topic, and message anchors (`/with/<msg_id>`, `/near/<msg_id>`, `/id/<msg_id>`).
- **Topic Ergonomics**: Handle long, sentence-like topics through URL input, substring/fuzzy matching, and indexed selection.
- **Incremental & Unread Reading**: Support `-unread` (oldest unread message anchor) and `-since` / `-until` time ranges (supporting relative durations like `24h` and local/UTC dates) with backward pagination.
- **Timezone Awareness**: Render timestamps in the user's local timezone (with UTC reference) by default.
- **Agent Skill Output**: Emit formatted agent skill instructions (`zulipc skill`) for one-line agent installation.
- **Zero External Dependencies**: Built entirely with the Go standard library (`net/http`, `encoding/json`, `flag`, `testing/synctest`, etc.).

### Non-Goals (MVP)
- **Write Operations**: No message posting, replying, editing, or deleting in the MVP.
- **Direct Messages (DMs)**: Defer 1:1 and private group DMs to future iterations.
- **Live Streaming / Tailing**: Defer long-polling event streams to future iterations.
- **Multi-source / Env Var Overrides**: Skip environment variable credential overrides in MVP; rely strictly on `.zuliprc`.

---

## 3. Architecture & Tech Stack

```
+-----------------------------------------------------------------------+
|                              zulipc CLI                               |
|                                                                       |
|  +--------------+  +--------------+  +--------------+  +-----------+  |
|  |     read     |  |    topics    |  |    search    |  |  whoami   |  |
|  |  (Markdown)  |  |  & channels  |  |   & filter   |  |  & skill  |  |
|  +-------+------+  +-------+------+  +-------+------+  +-----+-----+  |
|          |                 |                 |               |        |
|          +-----------------+--------+--------+---------------+        |
|                                     |                                 |
|                         +-----------v-----------+                     |
|                         |    Zulip API Client   |                     |
|                         |  (Go stdlib net/http) |                     |
|                         +-----------+-----------+                     |
+-------------------------------------|---------------------------------+
                                      | HTTP Basic Auth (email + key)
                                      v
                        +---------------------------+
                        |      Zulip REST API       |
                        |   /api/v1/messages        |
                        |   /api/v1/users/me        |
                        |   /api/v1/streams         |
                        |   /api/v1/users/me/...    |
                        +---------------------------+
```

### Dependencies
- **Runtime**: Standard library only (`net/http`, `encoding/json`, `flag`, `fmt`, `os`, `strings`, `time`, `net/url`).
- **Testing**: `testing`, `net/http/httptest`, and `testing/synctest` (utilize `synctest` for time-dependent or concurrent test cases without real-clock sleeps).
- **Coverage Tooling**: `go test -cover` and `rsc.io/uncover` for identifying untested branches.

---

## 4. Authentication & Configuration

`zulipc` follows the standard Zulip `.zuliprc` convention established by `zulip-terminal` and the official Python API bindings.

### Configuration Resolution
1. **Environment variable**: `ZULIP_CONFIG` (e.g. `export ZULIP_CONFIG=/path/to/.zuliprc`).
2. **Default configuration file**: `~/.zuliprc` (or `$XDG_CONFIG_HOME/zulip/zuliprc`).

If `ZULIP_SITE` is set (e.g. `export ZULIP_SITE=https://your-org.zulipchat.com`), it overrides `site` from the file, and the file may leave `site` out. It is empty by default.

### `.zuliprc` Format
```ini
[api]
email = alice@example.com
key = 32charhexapikeyabcdef0123456789
site = https://zulip.example.com
insecure = false # optional: skip TLS cert checks if true
```

### Missing Configuration Behavior
If no `.zuliprc` file is found, `zulipc` does not silently fail or fallback. It immediately prints step-by-step setup instructions and exits with code 1:

```text
zulipc: no configuration found at ~/.zuliprc

To configure zulipc:
  1. Open your Zulip organization in a browser (e.g. https://zulip.example.com)
  2. Navigate to: Settings (Gear icon) -> Personal settings -> Account & privacy -> API key
  3. Click "Download .zuliprc" (or "Show/change your API key")
  4. Save the file to ~/.zuliprc (permissions 0600):
     [api]
     email = your-email@example.com
     key   = your-api-key
     site  = https://zulip.example.com
```

---

## 5. URL & Narrow Parsing

Zulip uses client-side URL hash fragments for navigation. `zulipc` parses these URLs into standard API narrow parameters:

| URL Fragment Pattern | Extracted Narrow Query | Description |
| :--- | :--- | :--- |
| `#narrow/channel/<id>-<name>/topic/<topic>/with/<msg_id>` | `channel: <id/name>`, `topic: <topic>`, `anchor: <msg_id>`, `num_before: 20`, `num_after: 20` | Topic focused around a specific message |
| `#narrow/channel/<id>-<name>/topic/<topic>/near/<msg_id>` | `channel: <id/name>`, `topic: <topic>`, `anchor: <msg_id>`, `num_before: 20`, `num_after: 20` | Topic anchored near a message ID |
| `#narrow/channel/<id>-<name>/topic/<topic>` | `channel: <id/name>`, `topic: <topic>` | Full topic thread |
| `#narrow/stream/<id>-<name>/topic/<topic>` | `channel: <id/name>`, `topic: <topic>` | Legacy stream URL syntax |
| `#narrow/near/<msg_id>` | `anchor: <msg_id>`, `num_before: 20`, `num_after: 20` | Context around message |
| `#narrow/id/<msg_id>` | `anchor: <msg_id>`, `num_before: 0`, `num_after: 0` | Specific single message |
| `#narrow/channel/<id>-<name>` | `channel: <id/name>` | Channel-wide messages |

### Real-World Example
```text
https://zulip.example.com/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes/with/619610069
```
Parsed into:
- **Site**: `https://zulip.example.com`
- **Channel**: `615773` (`general`)
- **Topic**: `closing stale watchflakes` (unescaping `.20` $\rightarrow$ space)
- **Anchor**: `619610069` (`with` context)

---

## 6. CLI Specification (MVP Commands)

### 6.1 `zulipc read`
Fetches and formats messages for a topic, URL, or message ID.

```bash
# Read via URL (supports /with/<msg_id>, /near/<msg_id>, and /topic/...)
zulipc read "https://zulip.example.com/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes/with/619610069"

# Read via channel and topic (exact or fuzzy substring)
zulipc read -c general -t "closing stale watchflakes"

# Read unread messages only (starting at oldest unread)
zulipc read -c general -t "closing stale watchflakes" -unread

# Read messages since duration or local/UTC date
zulipc read -c general -t "closing stale watchflakes" -since 24h
zulipc read -c general -t "closing stale watchflakes" -since 24h -until 1h
zulipc read -c general -t "closing stale watchflakes" -since "2026-08-28 14:00"
zulipc read -c general -t "closing stale watchflakes" -since 2026-08-28T14:00:00Z

# Read context around a specific message ID
zulipc read -id 619610069

# Output formats & time preferences
zulipc read -c general -t "closing stale watchflakes" -json  # raw structured JSON
zulipc read -c general -t "closing stale watchflakes" -n 20   # limit message count
zulipc read -c general -t "closing stale watchflakes" -utc    # force UTC timestamps
```

#### Markdown Output Format (with Local Time)
```markdown
# [general] closing stale watchflakes
URL: https://zulip.example.com/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes/with/619610069

---
**Morgan Price** (2026-08-28 10:15 EDT / 14:15 UTC) [ID: 619610069]:
I have gone through the list of stale watchflakes and closed the resolved ones.

---
**Alex Chen** (2026-08-28 11:02 EDT / 15:02 UTC) [ID: 619610075]:
Thanks! Let's monitor the builder queue over the weekend.
```

---

### 6.2 `zulipc search`
Searches message history across the realm or scoped to a channel/topic.

```bash
# Search across all channels
zulipc search "watchflakes"

# Scoped search
zulipc search -c general "watchflakes"
zulipc search -c general -t "watchflakes" "stale"
zulipc search -sender morgan@example.com -since 7d
zulipc search -c general -since 24h -until 1h
zulipc search "proposal" -n 10 -json
```

---

### 6.3 `zulipc topics`
Lists active topics in a channel, sorted by most recent activity.

```bash
# List recent topics in channel 'general'
zulipc topics general

# Search/filter topics within a channel
zulipc topics general "watchflakes"

# Output:
# [1] closing stale watchflakes (14 msgs, 2h ago)
# [2] proposal review for some procedural deprecations? (8 msgs, 5h ago)
```

---

### 6.4 `zulipc channels` (alias `zulipc streams`)
Lists accessible channels.

```bash
zulipc channels         # Subscribed channels
zulipc channels -all    # All accessible public channels
zulipc channels -desc   # Include descriptions
```

---

### 6.5 `zulipc whoami`
Verifies authentication credentials, profile, and server connectivity.

```bash
zulipc whoami
# Site:        https://zulip.example.com
# User:        Alex Chen (alex@example.com)
# Role:        Member
# Server:      Zulip Server 9.0
# Config:      ~/.zuliprc [api]
```

---

### 6.6 `zulipc digest`
Generates periodic or daily activity summaries across subscribed channels, tracking read progress locally.

```bash
# Generate digest since last check (or past 24h on first run)
zulipc digest

# Scope digest to a channel
zulipc digest -c general

# Specific time interval
zulipc digest -since 24h -until 1h

# Dry-run without updating the local read cursor
zulipc digest -no-update

# Advance local cursor to now without printing
zulipc digest -mark

# Structured JSON output
zulipc digest -json
```

---

### 6.7 `zulipc skill` (or `zulipc skills`)
Prints the Markdown skill specification (including YAML frontmatter) to standard output.

```bash
# Display skill
zulipc skill

# Install directly into agent skills directory
zulipc skill > ~/.gemini/config/skills/zulip/SKILL.md
```

---

## 7. Code Structure

All code lives in `github.com/hyangah/misc/zulipc`:

```text
zulipc/
├── DESIGN.md          # This design document
├── main.go            # CLI entrypoint and subcommand dispatcher
├── config.go          # .zuliprc parser & setup helper
├── config_test.go     # Config parsing tests
├── state.go           # Local cursor state tracker (~/.cache/zulipc/state.json)
├── state_test.go      # State persistence tests
├── digest.go          # Daily digest generator and topic overview
├── digest_test.go     # Digest subcommand tests
├── url.go             # Zulip URL & narrow fragment parser
├── url_test.go        # URL parser test cases (including /with/, /near/, etc.)
├── client.go          # Zulip REST API client (messages, topics, streams, users)
├── client_test.go     # Client tests using httptest.Server mocks & synctest
├── time.go            # Local/UTC time formatting & relative duration parsing
├── time_test.go       # Time parser & synctest tests
├── read.go            # 'read' command logic & Markdown formatter
├── read_test.go       # Read formatting and flag filtering tests
├── search.go          # 'search' command logic
├── search_test.go     # Search command tests
├── topics.go          # 'topics' and 'channels' commands + fuzzy topic resolver
├── topics_test.go     # Topics listing & fuzzy matching tests
├── whoami.go          # 'whoami' connection check command
├── whoami_test.go     # 'whoami' command tests
├── skill.go           # 'skill' / 'skills' embedded Markdown generator
└── skill_test.go      # Skill output validation tests
```

---

## 8. Implementation Principles

1. **Reasonable Test Coverage for Every Subcommand**:
   - Every subcommand and package must have corresponding unit tests.
   - Use `net/http/httptest` to mock Zulip API responses without making external network calls.
   - Utilize `testing/synctest` when testing time-dependent or concurrent logic.
   - Run `go test -cover` and inspect uncovered branches using `rsc.io/uncover` to ensure edge cases and error paths are tested.
2. **Use `jj` (Jujutsu)**:
   - All VCS operations must use `jj`.
   - Work on top of the parent commit cleanly (`jj new`, `jj describe`, `jj squash` as appropriate).
3. **Small, Logically Separated Reviewable Commits**:
   - Break implementation into small, atomic changes:
     1. `zulipc: initial skeleton and config parser`
     2. `zulipc: add URL and narrow parser`
     3. `zulipc: add Zulip REST API client`
     4. `zulipc: add whoami and skill subcommands`
     5. `zulipc: add read subcommand and markdown renderer`
     6. `zulipc: add topics and channels subcommands`
     7. `zulipc: add search subcommand`
4. **Strict Focus on MVP**:
   - Implement only the read-only features listed in this document.
   - Defer write operations, reply features, and complex background streaming to subsequent milestones.

---

## 9. Future Work

- **Interactive/Write Features**: `zulipc reply <url> -m "..."`, `zulipc post -c <channel> -t <topic> -m "..."`.
- **Direct Messages**: `zulipc dms`, `zulipc dm <user>`.
- **Mention Notifications**: `zulipc mentions` to check where the user was @-mentioned.
- **Streaming / Tailing**: `zulipc read -f` / `zulipc tail` using Zulip event queues (`POST /api/v1/register`, `GET /api/v1/events`).
- **Profile Switcher & Env Overrides**: Multi-profile management (`zulipc login`, `zulipc --profile work`, `ZULIP_*` env vars).
