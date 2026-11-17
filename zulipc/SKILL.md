---
name: zulip
description: Read Zulip threads, search messages, generate daily summaries or digests, and discover active topics/channels with the "zulipc" command. Use when the user asks to summarize Zulip activity, get a daily digest, catch up on conversations, read a Zulip link, look up a topic, search discussions, or inspect channels.
---

# Reading Zulip threads and messages

The zulipc command provides fast, read-only access to Zulip streams, topics,
message threads, and search. It formats output as clean Markdown with local
timestamps and UTC references.

Commands:

 1. Read a thread from a Zulip URL:

        zulipc read "https://zulip.example.com/#narrow/channel/615773-general/topic/closing.20stale.20watchflakes/with/619610069"

    URL fragments with /with/<id>, /near/<id>, /id/<id>, /topic/<name>, or
    legacy /stream/<name> are parsed automatically. Dot-escaped hex characters
    (e.g., .20 for space, .28/.29 for parentheses, .2B for +, and emoji) are
    decoded.

 2. Read a thread by channel and topic:

        zulipc read -c general -t "closing stale watchflakes"
        zulipc read -c general -t "closing stale watchflakes" -since 24h
        zulipc read -c general -t "closing stale watchflakes" -since 24h -until 1h
        zulipc read -c general -t "closing stale watchflakes" -unread
        zulipc read -id 619610069
        zulipc read -c general -t "closing stale watchflakes" -json
        zulipc read -c general -t "closing stale watchflakes" -utc

 3. Search messages:

        zulipc search "watchflakes"
        zulipc search -c general "watchflakes"
        zulipc search -c general -t "closing stale watchflakes" "fixed"
        zulipc search -sender alice@example.com -since 7d
        zulipc search -c general -since 24h -until 1h

 4. Catch up on new messages with local read tracking:

        zulipc catchup
        zulipc catchup -c general
        zulipc catchup -since 24h -until 1h
        zulipc catchup -no-update
        zulipc catchup -mark
        zulipc catchup -status
        zulipc catchup -reset
        zulipc catchup -json

    "zulipc catchup" tracks reading progress locally in ~/.cache/zulipc/state.json
    (or ZULIP_STATE). On the first run, it defaults to the past 24 hours. On
    subsequent runs, it fetches messages since your last check, grouping
    messages by channel and topic with participant lists and links.
    "zulipc digest" is supported as an alias.

 5. Discover topics and channels:

        zulipc topics general
        zulipc topics general "watchflakes"
        zulipc channels
        zulipc channels -all

 6. Verify credentials and connectivity:

        zulipc whoami
        zulipc whoami -json

Configuration:

    zulipc reads credentials from ZULIP_CONFIG, ~/.zuliprc, or $XDG_CONFIG_HOME/zulip/zuliprc:

        [api]
        email = your-email@example.com
        key   = your-api-key
        site  = https://zulip.example.com

    Set ZULIP_CONFIG=<path> to specify an alternate configuration file.
    Set ZULIP_SITE=<url> to override the site; then the file may omit site.
    If no configuration is found, zulipc prints setup instructions and exits.

Rules and Best Practices for Agents:

  - When the user asks to catch up, summarize, or get a daily digest of Zulip activity, run "zulipc catchup" (or "zulipc catchup -c <channel>").
  - "zulipc catchup" tracks reading progress locally in ~/.cache/zulipc/state.json, so running it periodically only fetches new activity.
  - To check when the cursor was last updated without fetching messages, run "zulipc catchup -status".
  - To inspect a specific date or time range without advancing the cursor, pass "-since <time> -until <time> -no-update".
  - Pass "-json" when you need structured metadata (topics, participant lists, message IDs) for custom synthesis or reporting.
  - When the user provides a Zulip link, pass the full URL to "zulipc read".
  - Always quote URLs and topic names that contain spaces or special characters.
  - If you need to find a topic name before reading, use "zulipc topics <channel> [filter]".
  - Timestamps in Markdown output include both your local time and UTC.
