# gh-observer

![GitHub Issues](https://img.shields.io/github/issues/fini-net/gh-observer)
![GitHub Pull Requests](https://img.shields.io/github/issues-pr/fini-net/gh-observer)
![GitHub License](https://img.shields.io/github/license/fini-net/gh-observer)
![GitHub watchers](https://img.shields.io/github/watchers/fini-net/gh-observer)

A GitHub PR check watcher that improves on `gh pr checks --watch` by showing
runtime metrics, queue latency, and better handling of startup delays.

![project banner: abstract representation of code flowing through a pull request, using interconnected nodes and lines.](docs/gh-observer-banner.jpeg)

## Why?

The existing `gh pr checks --watch` doesn't show how long checks have been running, doesn't handle the 30-90s startup delay well, and doesn't show queue latency. This creates anxiety when watching CI runs - "is it stuck or just slow?"

## Features

- **Real-time status updates** - Poll GitHub API every 5s (configurable)
- **Runtime metrics** - Shows elapsed time for running checks
- **Queue latency** - Displays how long checks waited before starting
- **Startup phase handling** - Helpful messages during GitHub Actions startup delay
- **Rate limit awareness** - Backs off automatically if approaching limits
- **Exit codes** - Returns 0 for success, 1 for any failures

## Installation

```bash
go install github.com/fini-net/gh-observer@latest
```

Or build from source:

```bash
git clone https://github.com/fini-net/gh-observer.git
cd gh-observer
just build
```

## Usage

### Auto-detect PR from current branch

```bash
gh-observer
```

### Watch specific PR

```bash
gh-observer 123
```

### Use in CI pipelines

```bash
# Wait for checks to complete and exit with their status
gh-observer && echo "All checks passed!"
```

## Configuration

Create `~/.config/gh-observer/config.yaml` to customize settings:

```yaml
# Refresh interval for polling GitHub API
refresh_interval: 5s

# Color codes for terminal output (ANSI 256-color palette)
colors:
  success: 10  # Green - completed successfully
  failure: 9   # Red - completed with failure
  running: 11  # Yellow - currently in progress
  queued: 8    # Gray - waiting to start
```

See `.config.example.yaml` for reference.

## Authentication

gh-observer uses GitHub authentication in this order:

1. `GITHUB_TOKEN` environment variable
2. `gh` CLI authentication (`gh auth token`)

Make sure you have either set up.

## Example Output

```ShellOuptut
PR #123: Add new feature

Startup Phase (37s elapsed):
  ⏳ Waiting for Actions to start...
  💡 GitHub typically takes 30-90s to queue jobs after PR creation

Checks:
  ✓ markdownlint          [completed in 12s]        (queued: 41s)
  ✓ shellcheck            [completed in 8s]         (queued: 41s)
  ⏳ go-test              [running: 2m 34s]         (queued: 45s)
  ⏳ integration-tests    [running: 1m 12s]         (queued: 52s)
  ⏸️  deploy-preview      [queued: 3m 15s]

Last updated: 2s ago

Press q to quit
```

## Development

This project uses [just](https://github.com/casey/just) for task automation:

```bash
# Build the binary
just build

# Run on current PR
./gh-observer

# Create a PR
just pr

# Merge a PR
just merge
```

## Architecture

Built with:

- [Bubbletea](https://github.com/charmbracelet/bubbletea) - TUI framework
- [Lipgloss](https://github.com/charmbracelet/lipgloss) - Terminal styling
- [go-github](https://github.com/google/go-github) - GitHub API client
- [Viper](https://github.com/spf13/viper) - Configuration management

See `CLAUDE.md` for detailed implementation notes.

## Contributing

- [Code of Conduct](.github/CODE_OF_CONDUCT.md)
- [Contributing Guide](.github/CONTRIBUTING.md) includes a step-by-step guide to our
  [development process](.github/CONTRIBUTING.md#development-process).

## Support & Security

- [Getting Support](.github/SUPPORT.md)
- [Security Policy](.github/SECURITY.md)
