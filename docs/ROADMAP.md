# Roadmap

This is a maintained best-effort statement of direction for the next year.
It is not a commitment; items may be delivered out of order or dropped.

## In scope

- Continue refining the three watch modes (PR, run, repo) — output
  clarity, startup-phase handling, and queue-latency accuracy
- Keep pace with GitHub API changes (new check types, new Actions
  features) so the display stays trustworthy
- TUI quality-of-life: filtering, sorting, and keyboard navigation
- Increase automated test coverage, particularly in the TUI layer
- Performance of repo mode for repositories with many active PRs
- Grow the maintainer base beyond a single person (see
  [GOVERNANCE.md](GOVERNANCE.md))

## Out of scope (for now)

- Live-streaming job logs — blocked on a GitHub API gap; see the
  [known limitation](../README.md#known-limitation-live-logs-for-slow-jobs)
  in the README and the linked GitHub community discussion
- A GUI or web frontend; gh-observer is a terminal tool
- Watching providers other than GitHub

## Maintenance

Security updates and dependency refreshes continue regardless of the
feature roadmap. See [SECURITY.md](../.github/SECURITY.md) for the
support scope and end-of-life policy.
