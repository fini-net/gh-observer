+++
title = 'Announcing gh-observer: Because Waiting for CI Should Not Be a Mystery'
date = 2026-03-08T02:00:00-00:00
draft = false
description = 'I Got Tired of Watching `gh pr checks --watch` Fail Me, So I Built Something Better'
cover.image = '/posts/2026-FIX.png'
cover.hidden = false
keywords = ["github-actions", "github-cli-extension"]
tags = ["github", "programming"]
ShowToc = true
+++

Look, I know what you're thinking: "Another dev scratched their own itch and
now they want to tell me about it." Guilty. But hear me out, because this
particular itch has probably been annoying you too, and the scratch is
genuinely useful.

## The Problem Nobody Talks About

Here's the scenario that broke me: I push a PR, immediately run `gh pr checks
--watch`, and... it bombs out with an error because GitHub Actions hasn't
queued anything yet. So I wait. I run it again. Maybe it works, maybe it
doesn't. And when it finally does start showing me checks, I'm staring at a
list of job names with no idea whether that `3m 52s` I've been waiting is
normal or a sign that something's silently wedged.

The standard `gh pr checks --watch` has had some real gaps for a while now:

- **It doesn't handle startup delay.** GitHub Actions typically takes 30-90
  seconds to queue jobs after a PR is created or pushed to. The built-in
  watcher just... gives up during that window.
- **No queue latency.** You can see a job is "in progress," but you have no
  idea if it's been sitting in a queue for 2 seconds or 45 seconds before it
  started.
- **No runtime metrics.** Is that job that's been "running" for a while
  actually running, or has GitHub just not updated the status yet? Who knows!

So naturally, I did what any reasonable developer does when something annoys
them enough: I spent way more time building a solution than I would have lost
just dealing with the annoyance. Classic.

## Introducing gh-observer

`gh-observer` is a GitHub CLI extension that replaces `gh pr checks --watch`
with something that actually tells you what's going on. It's a full TUI
(terminal UI) that polls GitHub's API every 5 seconds and shows you the
information you actually care about.

Here's what a typical run looks like:

```ShellOutput
PR #5: 🔶 [claude] /init 21:04:15 UTC
Updated 0s ago  •  Pushed 43h 8m 11s ago

Startup   Workflow/Job                                Duration

  15s ✗ MarkdownLint / lint                             5s
   .github:13 - Failed with exit code: 1

  15s ✓ Auto Assign / run                               5s
  15s ✓ CUE Validation / verify                         6s
  15s ✓ Checkov / scan                                 27s
  15s ✓ Claude Code Review / claude-review          3m 52s
  15s ✓ Lint GitHub Actions workflows / actionlint      8s
  39s ✓ Checkov                                         2s

Press q to quit
```

That `15s` in the Startup column? That's how long GitHub sat on the job before
actually starting it. The `3m 52s` at the end? That's the total runtime. Now
you know the Claude review is just slow, not broken.

## The Startup Phase Thing

This is the part I'm most proud of, honestly. Instead of bombing out when there
are no checks yet, `gh-observer` shows you a helpful waiting message:

```ShellOutput
PR #123: Add new feature

Startup Phase (37s elapsed):
  ⏳ Waiting for Actions to start...
  💡 GitHub typically takes 30-90s to queue jobs after PR creation
```

It tracks how long you've been waiting, reminds you that this is normal, and
just... keeps watching. No manual intervention required. No re-running the
command. It transitions smoothly into showing actual check status once jobs
start appearing.

## Features Worth Knowing About

**Queue latency and runtime metrics** are the headline features, but there's more:

- **Workflow/Job naming** — Instead of just the job name, you see "MarkdownLint
  / lint" so you know which workflow the job belongs to. Uses GitHub's GraphQL
  API to pull this efficiently in a single query.
- **Error log integration** — Failed checks show the first line of their error
  output right there in the terminal. No more clicking through to GitHub to
  find out *why* something failed.
- **Rate limit awareness** — When you're getting close to GitHub API limits, it
  automatically backs off and polls less frequently. Keeps your rate limit
  healthy without any manual configuration.
- **CI-friendly snapshot mode** — When stdout isn't a TTY (like in a script or
  pipeline), it prints a plain text snapshot and exits with an appropriate exit
  code. So `gh-observer && deploy.sh` actually works.
- **Configurable colors** — ANSI 256-color support via
  `~/.config/gh-observer/config.yaml` if you're particular about your terminal
  aesthetics.

## Installation

The easiest path is the precompiled binary via GitHub CLI extensions — no Go
toolchain required:

```bash
gh extension install fini-net/gh-observer
```

Precompiled binaries exist for macOS (Intel and Apple Silicon), Linux (x86-64
and ARM64), and Windows (x86-64). All binaries include build attestations for
supply chain security verification.

## How to Use It

Auto-detect your current branch's PR:

```bash
gh observer
```

Watch a specific PR number:

```bash
gh observer 123
```

## Under the Hood (for the curious)

It's built in Go using [Bubbletea](https://github.com/charmbracelet/bubbletea)
for the TUI, which follows the Elm Architecture pattern — if you've done any
Elm or Redux, the Model/Update/View pattern will feel familiar. Lipgloss
handles the terminal styling.

The interesting bit technically is that it uses GitHub's GraphQL API to pull
check run data — same approach as `gh pr checks`, but in a single query that
returns both the workflow name and the job status together. This is why it can
show "MarkdownLint / lint" instead of just "lint": it's joining the workflow
and job name in one efficient API call.

Queue latency is calculated as the delta between when you pushed the commit and
when the check actually started. Runtime is `time.Now() - check.StartedAt` for
in-progress checks. Simple math, but surprisingly useful information.

## The Code

Everything's at
[fini-net/gh-observer](https://github.com/fini-net/gh-observer). It's open
source, and I'm genuinely interested in feedback and contributions. If you hit
a weird edge case or have a feature idea, open an issue.

And yes, before you ask — `gh observer` was partially built using `gh observer`
to watch its own CI. It's turtles all the way down.  Or the dog food tastes
great.  Pick your own adventure.

---

*If you find it useful, a star on the repo goes a long way. And if you find a
bug, please do tell me so we can fix it rather than quietly suffering.*

## Embedded metadata

- I tried Claude and OpenCode for writing drafts for this.  Claude was much
  closer to what I wanted.  Most of my changes were things that needed to be
  corrected elsewhere in the docs.  You can see the drafts I started from at
  <https://github.com/fini-net/gh-observer/tree/main/docs/drafts> .
- This will get cross-posted on linked-in.
