# Announcing gh-observer: Because Waiting for CI Shouldn't Be a Mystery

Remember the early days of CI? Back when Jenkins was the new hotness and we'd stare at those build logs scrolling by, wondering if this was the run that would finally pass? We've come a long way since then, but here's a dirty little secret: watching GitHub Actions checks still feels surprisingly primitive sometimes.

You push a PR, run `gh pr checks --watch`, and... nothing. The terminal sits there mute while GitHub queues up jobs. Is it working? Did something break? Should I go get coffee? Then the first check starts and you realize you've been staring at a blank screen for 45 seconds.

Or worse: a check has been running for what *feels* like forever, but you have no idea how long it's actually been. Was that 2 minutes or 20? And when did GitHub actually start it versus when it was just sitting in the queue?

## Enough Was Enough

Look, I'm not proud of how many times I've refreshed the GitHub UI thinking "maybe this time the checks page will load faster." But after one too many cycles of push-wait-refresh-worry, I decided to scratch my own itch and built **gh-observer**.

It's a GitHub CLI extension that does what `gh pr checks --watch` does, but with the stuff I always wished it had:

- Runtime metrics showing exactly how long checks have been running
- Queue latency so you know how long GitHub made you wait before starting
- Graceful handling of that awkward 30-90 second startup phase
- Real-time updates every 5 seconds without any manual polling

The output looks like this:

```text
PR #123: Add new authentication flow
Updated 0s ago  •  Pushed 2m 15s ago

Startup   Workflow/Job                         Duration

  12s ✓ Build / test                            45s
  12s ✓ Lint                                    12s
  18s ✓ Security Scan                          1m 3s
   8s ⟳ Integration Tests                   2m 31s
  18s ✓ Code Review                             28s

Press q to quit
```

See how you can tell the integration tests have been running for 2 minutes and 31 seconds? Or that they spent 8 seconds waiting in GitHub's queue before starting? That's the kind of visibility that turns anxiety into actionable information.

## The Startup Phase Problem

Here's the thing that really drove me nuts: GitHub Actions doesn't snap its fingers and instantly start your jobs. There's this black hole period after you push where nothing's happening yet, and the stock watcher just... gives up. Shows you nothing. Leaves you hanging.

gh-observer does the opposite. During that startup phase, you get:

```text
PR #42: Implement dark mode toggle

Startup Phase (23s elapsed):
  ⏳ Waiting for Actions to start...
  💡 GitHub typically takes 30-90s to queue jobs after PR creation
```

It's not just prettier—it actually tells you what's happening and sets reasonable expectations. Revolutionary concept, I know.

## Installation So Easy It Feels Like Cheating

The best part? No need to compile anything or mess with Go toolchains:

```bash
gh extension install fini-net/gh-observer
```

That's it. Prebuilt binaries for macOS (Intel and Apple Silicon), Linux (amd64 and ARM64), and Windows. Done.

Then just run it from any branch with an open PR:

```bash
gh-observer
```

Or point it at a specific PR:

```bash
gh-observer 123
```

## CI Integration (Because We're Adults Here)

If you're like me, you've got pipelines that need to wait for checks to pass. gh-observer plays nice with that too:

```bash
gh-observer && echo "All checks passed!"
```

It returns proper exit codes: 0 for success, 1 for failures. No funky exit code parsing required. I learned that lesson the hard way back when I thought checking for "BUILD SUCCESS" in log output was a reasonable approach. We've all been there.

## Under the Hood

Built with [Bubbletea](https://github.com/charmbracelet/bubbletea) for the TUI, which is honestly just delightful to work with if you've ever enjoyed functional programming patterns. The Elm Architecture makes the whole interactive piece feel natural—model, view, update, repeat.

I also leaned into GraphQL for fetching check runs because, well, why make 47 API calls when one will do? Your rate limit will thank you.

The whole thing's configurable too. Want it to poll every 3 seconds? Want different colors for your terminal theme? Drop a config file at `~/.config/gh-observer/config.yaml` and you're set:

```yaml
refresh_interval: 3s
colors:
  success: 10
  failure: 9
  running: 11
  queued: 8
```

## What's Next?

This started as a weekend project to make my own life less frustrating, but I'm genuinely curious if others find it useful. The code's open source at [github.com/fini-net/gh-observer](https://github.com/fini-net/gh-observer) and I'm happy to hear feedback, bug reports, or suggestions.

Fair warning: I built this for myself first, so it handles the workflows I care about. If there are edge cases I haven't considered, I'd love to hear about them. That's how these things get better.

## Give It a Spin

If you're tired of the refresh-worry-wonder cycle when watching your CI:

```bash
gh extension install fini-net/gh-observer
```

Then next time you push a PR, give it a try. Let me know if it saves you some sanity.

After all, we've got better things to do than stare at terminals wondering if our builds are actually running.
