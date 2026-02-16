# gh-observer: A better way to watch PR checks

Since you're reading this, I'm guessing you also get frustrated with `gh pr checks --watch` bombing as soon as you push a new PR, sitting there useless until the first job actually starts running? Or maybe you're scratching your head trying to figure out how long your checks have been queued up, and those URLs just don't help at all?

I've been there too. That's why I built **gh-observer** — a GitHub CLI extension that improves on `gh pr checks --watch` in a few key ways:

- **Handles the startup phase gracefully** — no more bombing out while waiting for GitHub Actions to queue the first job
- **Shows queue latency and runtime metrics** — see exactly how long your checks spent waiting and how long they've been running
- **Clean TUI** — everything stays in the terminal, no need to click through to GitHub
- **Works in CI too** — outputs a snapshot when stdout isn't a TTY, so you can use it in pipelines

Install it with:

```bash
gh extension install fini-net/gh-observer
```

Then run it from any PR branch:

```bash
gh-observer
# or watch a specific PR
gh-observer 123
```

Give it a spin and let me know what you think!

## Meta

- posted at <https://github.com/cli/cli/discussions/12698>
