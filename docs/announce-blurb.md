Since you're reading this, I'm guessing you also get frustrated with `gh pr checks --watch`
bombing as soon as you push a new PR, sitting there useless until the first job
actually starts running? Or maybe you're scratching your head trying to figure
out how long your checks have been queued up, and those URLs just don't help at
all? I've been there too. That's why I built **gh-observer** — it handles the
startup phase gracefully, shows queue latency and runtime metrics right in the
terminal, and skips the noise. Give it a spin:
`gh extension install fini-net/gh-observer`
