# LinkedIn Announcement Post

---

I got tired of `gh pr checks --watch` bailing on me every time I pushed a PR,
so I built a replacement. Introducing **gh-observer** — a GitHub CLI extension
that actually tells you what's going on with your CI.

The two problems that finally pushed me over the edge:

**Startup delay.** GitHub Actions takes 30-90 seconds to queue jobs after a PR
push. The built-in watcher just errors out during that window. gh-observer
shows you a helpful waiting message and keeps going. No more "run it again and
hope."

**No timing context.** Is that job that's been "running" for a few minutes
actually running, or is it stuck? gh-observer shows you queue latency (how
long before the job started) and runtime side-by-side, so you know the
difference between "slow" and "broken."

It looks like this:

```ShellOutput
PR #5: 🔶 [claude] /init 21:04:15 UTC

Startup  Workflow/Job                              Duration

  15s ✗ MarkdownLint / lint                           5s
   .github:13 - Failed with exit code: 1

  15s ✓ CUE Validation / verify                       6s
  15s ✓ Claude Code Review / claude-review         3m 52s
```

That `15s` startup column is queue latency — how long GitHub sat on the job
before starting it. Now you know the Claude review is just slow, not wedged.

Also ships with:

- Workflow/job naming via GraphQL ("MarkdownLint / lint" not just "lint")
- Inline error output for failed checks
- CI-friendly snapshot mode (works in scripts, exits with meaningful codes)
- Rate limit awareness that backs off automatically

No Go toolchain needed — just install via gh CLI extensions:

```bash
gh extension install fini-net/gh-observer
```

macOS, Linux, and Windows binaries available, all with build attestations.

Full writeup on the blog: <https://www.chicks.net/posts/2026-03-08-announce-gh-observer/>

Code at: <https://github.com/fini-net/gh-observer>

Stars and bug reports both welcome. It's turtles all the way down — gh-observer
was partially built using gh-observer to watch its own CI.

 #GitHub #GitHubActions #DevTools #OpenSource #Go #DeveloperExperience
