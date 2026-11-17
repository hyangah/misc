// Copyright 2026 The Go Authors. All rights reserved.
// Use of this source code is governed by a BSD-style
// license that can be found in the LICENSE file.

package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log"
	"os"
	"strings"

	"github.com/hyangah/misc/internal/skills"
)

var usageMessage = `zulipc reads, searches, and summarizes Zulip streams and topics.

usage:
	zulipc <command> [flags] [arguments]

commands:
	read      read messages from a thread URL or channel and topic
	search    search messages across channels and topics
	catchup   fetch and group messages with local read tracking
	topics    list topics in a channel
	channels  list subscribed or public channels
	whoami    show current user identity and server details
	skill     output or install the agent skill definition

Flags follow the command they belong to.
Use "zulipc <command> -h" for more information about a command.

Configuration is read from ZULIP_CONFIG, ~/.zuliprc, or $XDG_CONFIG_HOME/zulip/zuliprc.
Set ZULIP_SITE to override the server URL from the configuration file.
`

func usage() {
	fmt.Fprint(os.Stderr, usageMessage)
}

// newFlags returns a flag set for a subcommand with its description.
func newFlags(name, desc, args string) *flag.FlagSet {
	f := flag.NewFlagSet(name, flag.ContinueOnError)
	f.SetOutput(os.Stderr)
	f.Usage = func() {
		fmt.Fprintf(os.Stderr, "usage: zulipc %s %s\n\n%s\n\nflags:\n", name, args, desc)
		f.PrintDefaults()
	}
	return f
}

func run(args []string) error {
	if len(args) == 0 {
		usage()
		return errors.New("no command specified")
	}

	switch args[0] {
	case "read":
		return cmdRead(args[1:])
	case "search":
		return cmdSearch(args[1:])
	case "catchup", "digest":
		return cmdCatchupNamed(args[0], args[1:])
	case "topics":
		return cmdTopics(args[1:])
	case "channels", "streams":
		return cmdChannels(args[1:])
	case "whoami":
		return cmdWhoami(args[1:])
	case "skill", "skills":
		return cmdSkill(args[1:])
	case "help", "-h", "-help", "--help":
		usage()
		return nil
	default:
		if strings.HasPrefix(args[0], "-") {
			usage()
			return fmt.Errorf("unexpected top-level flag %q: flags must follow subcommands", args[0])
		}
		usage()
		return fmt.Errorf("unknown command %q", args[0])
	}
}

func main() {
	log.SetPrefix("zulipc: ")
	log.SetFlags(0)

	if err := run(os.Args[1:]); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			os.Exit(0)
		}
		if notFound, ok := errors.AsType[*ConfigNotFoundError](err); ok {
			fmt.Fprintf(os.Stderr, "zulipc: %s\n", notFound.Error())
			os.Exit(1)
		}
		fmt.Fprintf(os.Stderr, "zulipc: %v\n", err)
		os.Exit(1)
	}
}

func cmdRead(args []string) error {
	f := newFlags("read", "Read messages from a thread URL or channel and topic.", "[-c channel] [-t topic] [-since time] [-until time] [-unread] [-id msgid] [-json] [-utc] [-n limit] [url]")
	channel := f.String("c", "", "channel / stream name")
	topic := f.String("t", "", "topic name")
	since := f.String("since", "", "fetch messages since duration (e.g. 24h) or date")
	until := f.String("until", "", "fetch messages until duration (e.g. 1h) or date")
	unread := f.Bool("unread", false, "fetch unread messages")
	msgID := f.Int64("id", 0, "message ID context")
	asJSON := f.Bool("json", false, "output JSON format")
	utc := f.Bool("utc", false, "format timestamps in UTC")
	limit := f.Int("n", 50, "maximum number of messages to fetch")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() > 1 {
		return fmt.Errorf("unexpected extra arguments: %s", strings.Join(f.Args()[1:], " "))
	}
	var rawURL string
	if f.NArg() == 1 {
		rawURL = f.Args()[0]
	}

	opts := ReadOptions{
		URL:     rawURL,
		Channel: *channel,
		Topic:   *topic,
		Since:   *since,
		Until:   *until,
		Unread:  *unread,
		MsgID:   *msgID,
		AsJSON:  *asJSON,
		UTC:     *utc,
		Limit:   *limit,
	}

	cfg, err := LoadConfig("")
	if err != nil {
		return err
	}
	client, err := NewClient(cfg)
	if err != nil {
		return err
	}
	return runRead(context.Background(), client, cfg, opts, os.Stdout)
}

func cmdSearch(args []string) error {
	f := newFlags("search", "Search messages across channels and topics.", "[-c channel] [-t topic] [-sender email] [-since time] [-until time] [-json] [-utc] [-n limit] [query...]")
	channel := f.String("c", "", "channel / stream name")
	topic := f.String("t", "", "topic name")
	sender := f.String("sender", "", "filter by sender email")
	since := f.String("since", "", "fetch messages since duration or date")
	until := f.String("until", "", "fetch messages until duration or date")
	asJSON := f.Bool("json", false, "output JSON format")
	utc := f.Bool("utc", false, "format timestamps in UTC")
	limit := f.Int("n", 20, "maximum number of messages to fetch")
	if err := f.Parse(args); err != nil {
		return err
	}

	query := strings.Join(f.Args(), " ")

	opts := SearchOptions{
		Query:   query,
		Channel: *channel,
		Topic:   *topic,
		Sender:  *sender,
		Since:   *since,
		Until:   *until,
		AsJSON:  *asJSON,
		UTC:     *utc,
		Limit:   *limit,
	}

	cfg, err := LoadConfig("")
	if err != nil {
		return err
	}
	client, err := NewClient(cfg)
	if err != nil {
		return err
	}
	return runSearch(context.Background(), client, cfg, opts, os.Stdout)
}

func cmdCatchup(args []string) error {
	return cmdCatchupNamed("catchup", args)
}

func cmdDigest(args []string) error {
	return cmdCatchupNamed("digest", args)
}

func cmdCatchupNamed(cmdName string, args []string) error {
	f := newFlags(cmdName, "Fetch and group messages with local read tracking.", "[-c channel] [-since time] [-until time] [-state path] [-no-update] [-mark] [-status] [-reset] [-json] [-utc] [-n limit]")
	channel := f.String("c", "", "channel / stream name to scope catchup to")
	since := f.String("since", "", "fetch messages since duration or date")
	until := f.String("until", "", "fetch messages until duration or date")
	stateFile := f.String("state", "", "path to state file tracking read cursor")
	noUpdate := f.Bool("no-update", false, "do not update state file cursor")
	markOnly := f.Bool("mark", false, "mark state cursor up to now without printing messages")
	status := f.Bool("status", false, "show current catchup status and cursor time")
	reset := f.Bool("reset", false, "reset catchup state cursor")
	asJSON := f.Bool("json", false, "output JSON format")
	utc := f.Bool("utc", false, "format timestamps in UTC")
	limit := f.Int("n", 200, "maximum number of messages to fetch")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() > 0 {
		return fmt.Errorf("unexpected extra arguments: %s", strings.Join(f.Args(), " "))
	}

	opts := CatchupOptions{
		Channel:   *channel,
		Since:     *since,
		Until:     *until,
		StateFile: *stateFile,
		NoUpdate:  *noUpdate,
		MarkOnly:  *markOnly,
		Status:    *status,
		Reset:     *reset,
		AsJSON:    *asJSON,
		UTC:       *utc,
		Limit:     *limit,
	}

	if opts.Status || opts.Reset {
		return runCatchup(context.Background(), nil, nil, opts, os.Stdout)
	}

	cfg, err := LoadConfig("")
	if err != nil {
		return err
	}
	client, err := NewClient(cfg)
	if err != nil {
		return err
	}
	return runCatchup(context.Background(), client, cfg, opts, os.Stdout)
}

func cmdTopics(args []string) error {
	f := newFlags("topics", "List topics in a channel.", "[-json] [-n limit] channel [filter]")
	asJSON := f.Bool("json", false, "output JSON format")
	limit := f.Int("n", 30, "maximum number of topics to fetch")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() < 1 {
		return errors.New("missing channel argument: usage: zulipc topics [-json] [-n limit] channel [filter]")
	}
	if f.NArg() > 2 {
		return fmt.Errorf("unexpected extra arguments: %s", strings.Join(f.Args()[2:], " "))
	}

	channel := f.Args()[0]
	var filter string
	if f.NArg() == 2 {
		filter = f.Args()[1]
	}

	opts := TopicOptions{
		Channel: channel,
		Filter:  filter,
		AsJSON:  *asJSON,
		Limit:   *limit,
	}

	cfg, err := LoadConfig("")
	if err != nil {
		return err
	}
	client, err := NewClient(cfg)
	if err != nil {
		return err
	}
	return runTopics(context.Background(), client, opts, os.Stdout)
}

func cmdChannels(args []string) error {
	f := newFlags("channels", "List subscribed or public channels.", "[-all] [-desc] [-json] [-n limit]")
	all := f.Bool("all", false, "include all public channels, not just subscribed")
	desc := f.Bool("desc", false, "include channel descriptions")
	asJSON := f.Bool("json", false, "output JSON format")
	limit := f.Int("n", 0, "maximum number of channels to display (0 for unlimited)")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(f.Args(), " "))
	}

	opts := ChannelOptions{
		All:         *all,
		Description: *desc,
		AsJSON:      *asJSON,
		Limit:       *limit,
	}

	cfg, err := LoadConfig("")
	if err != nil {
		return err
	}
	client, err := NewClient(cfg)
	if err != nil {
		return err
	}
	return runChannels(context.Background(), client, opts, os.Stdout)
}

func cmdWhoami(args []string) error {
	f := newFlags("whoami", "Show current user identity and server details.", "[-json]")
	asJSON := f.Bool("json", false, "output JSON format")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(f.Args(), " "))
	}
	cfg, err := LoadConfig("")
	if err != nil {
		return err
	}
	client, err := NewClient(cfg)
	if err != nil {
		return err
	}
	return runWhoami(context.Background(), client, cfg, *asJSON, os.Stdout)
}

func cmdSkill(args []string) error {
	f := newFlags("skill", "Output or install the agent skill definition.", "[-print] [-install] [-diff] [-force] [-project] [-o dir]")
	doPrint := f.Bool("print", false, "print the skill to standard output (the default)")
	doInstall := f.Bool("install", false, "install the skill for the agents set up on this machine")
	doDiff := f.Bool("diff", false, "show diff between installed skill and embedded version")
	force := f.Bool("force", false, "overwrite existing modified skill files")
	f.BoolVar(force, "f", false, "alias for -force")
	project := f.Bool("project", false, "with -install or -diff, operate on the repository rather than the home directory")
	outDir := f.String("o", "", "with -install or -diff, operate on `dir` and nowhere else")
	if err := f.Parse(args); err != nil {
		return err
	}
	if f.NArg() > 0 {
		return fmt.Errorf("unexpected arguments: %s", strings.Join(f.Args(), " "))
	}
	return runSkillWithOptions(os.Stdout, os.Stderr, skills.RunOptions{
		Print:   *doPrint,
		Install: *doInstall,
		Diff:    *doDiff,
		Force:   *force,
		Project: *project,
		OutDir:  *outDir,
	})
}
