# Release Notes: gh-process.just

This file tracks the evolution of the Git/GitHub workflow automation module.

## January 2026 - Avila Beach is awesome

### v5.6 - Release Version Validation (2026-01-30)

**Related PR:** [#92](https://github.com/fini-net/template-repo/pull/92)

Added version format validation to the `release` recipe to prevent accidental releases with malformed version numbers. Previously, you could run `just release anything` and it would attempt to create a GitHub release with whatever string you provided, potentially creating confusing or invalid release tags.

**New safety check:**

- **Version format validation** - The `release` recipe now validates that the version parameter starts with 'v' followed by a digit (e.g., v1.0.0, v2.3.4)
- **Clear error message** - Displays red error message if format is invalid: "Error: Release version must start with 'v' followed by a digit"
- **Early exit** - Fails fast with exit code 1 before attempting to create the GitHub release
- **Shellcheck compatibility** - Added disable comment for SC2050 since shellcheck doesn't understand just's templating syntax (`{{rel_version}}`)

**Implementation details:**

- Uses bash regex pattern `^v[0-9]` to match the required format
- Validation runs after the `standard-release` flag check but before the actual `gh release create` command
- Prevents common typos like `just release 1.0.0` (missing 'v') or `just release vNext` (no digit)

This change makes the release workflow safer by catching version format errors upfront rather than creating malformed tags that would need manual cleanup. Complements the existing `standard-release` flag system from v5.3.

### v5.5 - Robust Copilot Suggestion Application (2026-01-28)

Fixes issue [#76](https://github.com/fini-net/template-repo/issues/76)
**Related PR:** [#88](https://github.com/fini-net/template-repo/pull/88)

Enhanced the `copilot_pick` recipe with the ability to directly apply Copilot suggestions, plus comprehensive safety improvements based on Claude Code review feedback. Previously, `copilot_pick` was read-only - you could view suggestions but had to manually apply them. Now you can apply suggestions directly with proper backup and rollback capabilities.

**New feature:** Interactive suggestion application with safety nets

- **Apply suggestions** - When viewing a suggestion, choose to apply it directly to the file
- **Automatic backup** - Creates timestamped backups in `.just/copilot_backups/` before making changes
- **Multi-line support** - Properly handles both single-line and multi-line code suggestions
- **Visual preview** - Shows diff-like before/after display with line counts for multi-line changes
- **Git integration** - Offers to push applied changes back to the PR automatically
- **Confirmation workflow** - Two-stage confirmation (view → apply → push) prevents accidents

**Critical safety fixes** from Claude Code review:

- **Fixed dangerous sed command** - Now properly escapes special characters and uses `|` delimiter instead of `/` to handle paths with slashes
- **Improved code extraction** - Enhanced AWK logic handles multiple code blocks and avoids naive truncation
- **Enhanced backup system** - Stores full file paths for proper restoration, prevents path traversal issues
- **Added integrity validation** - Checks backup readability, file size, and warns about uncommitted changes before restoration
- **Better UX for multi-line changes** - Shows first line plus line count instead of truncating output

**New recipe:** `copilot_rollback`

- **Interactive restoration** - Browse available backups and restore files with confirmation
- **Path reconstruction** - Properly converts safe backup filenames back to original file paths
- **Safety checks** - Validates backup integrity and checks for uncommitted changes before overwriting
- **Git integration** - Offers to push restored files back to PR

**Implementation details:**

- Uses relative git paths for backup storage to work across different working directories
- Escapes all sed special characters (`[ ] * ^ $ ( ) + ? { } | \`) to handle complex code suggestions
- Improved error handling with consistent backup system and proper cleanup
- Enhanced diff display for both single and multi-line suggestions
- Added `.just/copilot_backups/` to `.gitignore` (prevents committing backups)

The feature makes Copilot suggestions much more actionable while maintaining the safety-first approach that's central to this workflow. You can now iterate on Copilot feedback without leaving the terminal, with robust rollback capabilities if something goes wrong.

### v5.3 - Configurable Release Workflow (2026-01-28)

Fixes issue [#82](https://github.com/fini-net/template-repo/issues/82)
**Related PR:** [#84](https://github.com/fini-net/template-repo/pull/84)

Added the `standard-release` flag to `.repo.toml` that allows projects to disable the default release recipes when they need custom release mechanisms. Previously, all repos inherited the standard `release` and `release_age` recipes whether they needed them or not, which could cause confusion in projects with specialized release workflows.

**New flag:** `standard-release` in `.repo.toml`

- **Default behavior** - When `standard-release = true` (or unset), provides standard `release` and `release_age` recipes
- **Custom workflows** - Set `standard-release = false` to disable standard recipes for projects with custom release processes
- **Graceful messaging** - Disabled recipes display informational messages and exit cleanly (exit 0)
- **Claude Code integration** - Updated `release_age` recipe to be more Claude-friendly with structured output and clear messaging

**Implementation details:**

- Modified `.just/gh-process.just` to check the flag before executing release logic
- Sources `.just/repo-toml.sh` for flag access (integrates with v4.6 metadata system)
- Both `release` and `release_age` recipes respect the flag
- Informational messages explain why the recipe is disabled when flag is false
- Maintains backwards compatibility - repos without the flag get standard behavior

This allows template-repo to serve both simple projects that want the standard release workflow and complex projects that need custom release automation, all while using the same template base.

### v5.2 - Template Sync System (2026-01-27)

Fixes issue [#55](https://github.com/fini-net/template-repo/issues/55)
**Related PR:** [#83](https://github.com/fini-net/template-repo/pull/83)

Implemented a safe update mechanism that allows derived repos to pull changes from template-repo while preserving local customizations.

**New module:** `.just/template-sync.just`

- **Multi-version checksum tracking** - `.just/CHECKSUMS.json` manifest tracks historical checksums of all `.just/*.just` files from git history
- **Safe updates** - Only modifies files whose checksums match a known template version
- **Local preservation** - Files with modifications are skipped with clear warnings
- **Diagnostic tools** - `checksums_verify` and `checksums_diff` for preview and inspection

**Four new recipes:**

1. `checksums_generate` - Generate versioned checksums from git history (template-repo only)
2. `update_from_template` - Update .just modules from template-repo (derived repos)
3. `checksums_verify` - Check local files against template versions
4. `checksums_diff <file>` - Show diff between local and latest template version

**Implementation details:**

- `.just/lib/generate_checksums.sh` - Extracts checksums from git history with version tagging
- `.just/lib/template_update.sh` - Core update logic with retry, backup, and rollback
- `.just/lib/template_sync_test.sh` - Test suite with fixtures
- Platform-compatible checksums (sha256sum on Linux, shasum on macOS)
- Network retry logic (3 attempts with exponential backoff)
- Automatic backup and restore on download failures

**Test coverage:**

- Test fixtures in `.just/test/fixtures/template_sync/`
- Scenarios: unmodified file update, modified file skip, already latest
- GitHub Actions workflow for continuous validation

### v5.1 - On-Demand Copilot Reviews (2026-01-XX)

Add `copilot_refresh` recipe to request new Copilot reviews on demand.

**Fixes issue:** [#77](https://github.com/fini-net/template-repo/issues/77)
**Related PR:** [#79](https://github.com/fini-net/template-repo/pull/79)

- **copilot_refresh recipe** - Request a fresh Copilot review on current PR
  - Uses GitHub REST API to add copilot-pull-request-reviewer[bot] as reviewer
  - Waits for review completion with animated spinner (via gum) or dots fallback
  - Displays suggestion count and points to copilot_pick for interactive browsing
  - Smart error handling for common failure scenarios

Implementation details:

- Polls every 3 seconds for up to 45 seconds for review completion
- Uses same GraphQL query as copilot_pick to detect completed reviews
- Does not check copilot-review flag (user-initiated action)
- Complements existing copilot_pick (#67, #72) workflow

### v5.0 - Robust PR Body Updates with HTML Markers

Completely rewrote the `pr_update` recipe to eliminate data loss when updating
PR descriptions. The previous AWK-based implementation (v4.0-4.4) was fragile
and could corrupt or lose manual edits to PR descriptions, especially when
custom sections existed between Done and Meta, or when code blocks contained
`## Done` markers.

**Fixes issue:** [#57](https://github.com/fini-net/template-repo/issues/57)

The new implementation uses HTML comment markers for reliable section boundaries
and includes comprehensive test coverage:

- **HTML comment markers** - New PRs now include invisible `<!-- PR_BODY_DONE_START -->`
  and `<!-- PR_BODY_DONE_END -->` markers that provide precise boundaries for
  the Done section, eliminating ambiguity from section header detection.

- **Standalone library script** - Extracted PR body manipulation into
  `.just/lib/update_pr_body.sh` - a standalone, testable bash script with
  state machine parsing, code block tracking to avoid false positives on
  `## Done` inside code examples, and backwards compatibility with old PRs.

- **Comprehensive test suite** - Added 13 test cases in `.just/test/fixtures/pr_bodies/`
  covering basic scenarios (Done + Meta, custom sections, multiple sections),
  edge cases (code blocks, nested markdown, missing sections, empty body), and
  data preservation (checkboxes, tables, HTML comments, verify sections).

- **Test infrastructure** - New `.just/lib/pr_body_test.sh` test runner and
  `just pr_body_test` recipe for running tests. Automated in CI via new
  `.github/workflows/pr-body-tests.yml` workflow.

- **Backwards compatibility** - Old PRs without HTML markers continue to work
  using section header detection as fallback. When updated, they automatically
  receive the new markers for future reliability.

- **State machine parser** - Uses BEFORE_DONE → IN_DONE → AFTER_DONE state
  transitions with proper code block tracking, ensuring `## Done` inside
  triple-backtick blocks doesn't confuse the parser.

- **Smart section insertion** - When no Done section exists, intelligently
  inserts it after introductory content but before the first section header,
  maintaining proper PR structure.

Implementation details:

- Modified `pr` recipe to insert HTML markers when creating new PRs
- Replaced complex 48-line AWK logic in `pr_update` with 10-line call to
  library script
- All scripts pass shellcheck validation
- Tests run automatically on relevant file changes via GitHub Actions
- Created new `.just/testing.just` module for test recipes

This is a breaking change internally (complete rewrite of PR body update logic)
but maintains the same external interface. The change makes the workflow
significantly more robust - custom sections, verification timestamps, task
lists, tables, and other manual edits are now reliably preserved across
updates.

### v4.9 - Copilot Suggestion Count

Enhanced the Copilot review display to show a count of suggestions instead of
raw JSON output, making it easier to quickly assess how many items need attention.
Previously, after PR checks completed, you'd see the full JSON dump of all Copilot
suggestions, which was hard to scan at a glance. Now you get a clear summary of
how many suggestions there are, and the count appears both immediately after checks
and at the end of Claude's review output.

**Fixes issue:** [#73](https://github.com/fini-net/template-repo/issues/73)
**Related PR:** [#74](https://github.com/fini-net/template-repo/pull/74)

- **Count display** - Shows "Total Copilot suggestions: N" after PR checks complete,
  or "No Copilot suggestions - looks good!" when clean. Replaces the immediate
  JSON dump with human-friendly feedback.

- **Summary in claude_review** - Displays the same count summary at the end of the
  `claude_review` recipe output, providing a quick reference after you've read
  Claude's feedback. Makes it easy to remember if there are Copilot items to
  address without scrolling back through terminal history.

- **Safer temp file naming** - Uses PR metadata (owner, repo name, PR number) to
  generate unique temp filenames at `/tmp/copilot_count_${OWNER}_${REPO}_${PR}`.
  Prevents collisions when working with multiple PRs across different repositories.
  File is automatically cleaned up after display.

- **Maintained behavior** - Still outputs the full JSON for those who want to parse
  it programmatically or review detailed suggestions. The count is additive, not a
  replacement.

The change makes the workflow feel more polished - you get immediate actionable
feedback ("3 suggestions to review") rather than having to eyeball JSON arrays.
Pairs nicely with the `copilot_pick` recipe from v4.8 for diving into specific
suggestions when needed.

### v4.8 - Copilot Suggestion Picker

Added an interactive picker for browsing and viewing GitHub Copilot PR review
suggestions without leaving the terminal. Previously, you could see a JSON dump
of all Copilot suggestions after PR checks completed, but navigating through
multiple suggestions was cumbersome. The new `copilot_pick` recipe provides a
streamlined interface for exploring Copilot feedback.

- **`copilot_pick`** - Interactive recipe using `gum` to display Copilot
  suggestions in a browsable list. Shows `file:line - preview` format for
  quick scanning, then displays the full suggestion body when selected.
  Requires the `gum` tool for interactive selection.

- **Prerequisite checks** - Validates that both `gum` (for interactive UI)
  and `jq` (for JSON parsing) are installed before proceeding, with helpful
  installation instructions if missing.

- **GraphQL integration** - Fetches Copilot review comments using GitHub's
  GraphQL API with proper limits documented (last 20 reviews, first 100
  comments per review). Sufficient for most PRs but noted in case of very
  active discussions.

- **Robust error handling** - Validates line number extraction with regex
  check to prevent cryptic jq errors. Shows clear error messages if selection
  format is unexpected or if no suggestions are found.

- **Cleanup handling** - Properly manages temporary files with a single trap
  that cleans up both temp files on exit. Fixed an initial bug where dual
  traps would overwrite each other, potentially leaving temp files behind if
  errors occurred early in execution.

The recipe fills the gap between the automated post-checks JSON dump and
opening the PR in a browser - perfect for quickly reviewing specific Copilot
suggestions while staying in your terminal workflow. Exit gracefully with
Ctrl+C if you don't want to view any suggestions.

**Fixes issue:** [#67](https://github.com/fini-net/template-repo/issues/67)
**Related PR:** [#72](https://github.com/fini-net/template-repo/pull/72)

### v4.6 - Conditional AI Review Display

Added repository metadata extraction system that enables flag-based conditional
display of AI code reviews. Previously, Copilot and Claude reviews were always
displayed after PR checks completed, regardless of whether they were enabled or
relevant for the project. Now you can control this behavior via `.repo.toml`
flags.

The new `.just/repo-toml.just` module generates a sourceable shell script
(`.just/repo-toml.sh`) containing all repository metadata as shell variables.
This eliminates repeated parsing of `.repo.toml` throughout the codebase and
provides a single source of truth for configuration data.

- **`repo_toml_generate`** - Exports `.repo.toml` to shell variables with automatic
  derivation of org/repo names from URLs, conversion of TOML arrays to both bash
  arrays and CSV strings, and feature flags as strings ("true"/"false")
- **`repo_toml_check`** - Validates generated file exists and checks staleness
  (warns if `.repo.toml` modified since last generation)
- **Conditional reviews** - Modified `pr_checks` and `claude_review` recipes to
  source the generated metadata and only display reviews when corresponding flags
  (`copilot-review`, `claude-review`) are enabled in `.repo.toml`
- **Graceful degradation** - If generated file is missing, warns user and defaults
  flags to false rather than failing hard

The generated file is gitignored since it's environment-specific and regenerated
on demand. This architecture enables future recipes to access repository metadata
without parsing overhead, and provides a clean pattern for flag-based feature
toggles across the workflow.

**Fixes issue:** [#63](https://github.com/fini-net/template-repo/issues/63)
**Related PR:** [#64](https://github.com/fini-net/template-repo/pull/64)

### v4.7 - Stale Review Detection

Enhanced the `claude_review` recipe to detect and warn when Claude's PR review
feedback doesn't apply to the latest code. Previously, the recipe would blindly
display Claude's most recent comment even if you'd pushed new commits since the
review was written, leading to confusion about whether the feedback was still
relevant.

Now the recipe compares timestamps between Claude's latest comment and your most
recent commit to provide context-aware status:

- **Missing review** - Shows informational message with latest commit SHA and
  suggests re-running `just claude_review` or checking browser for workflow
  status. Helpful when the review workflow is still running.

- **Stale review** - Displays yellow warning with both timestamps (review
  created vs. latest commit), age difference in minutes, and clear disclaimer
  that feedback may not apply to latest code. Still shows the comment content
  but prepends "⚠️ Claude Code Review exists but is STALE" header.

- **Current review** - Normal display with "(current)" indicator confirming the
  feedback applies to your latest code.

Implementation details:

- **Cross-platform date handling** - Works on both Linux (GNU date) and macOS
  (BSD date) with proper fallbacks
- **Graceful degradation** - If date parsing fails, shows warning but continues
  to display comment
- **Always exits 0** - Won't break workflow chains (maintains `pr_checks &&
  claude_review` compatibility)
- **Uses timestamp comparison** - After investigating `.github/workflows/claude-code-review.yml`,
  discovered Claude uses `gh pr comment` which creates IssueComments (not
  PullRequestReviews), so commit SHA association isn't available. Timestamp
  comparison is the correct approach.

The recipe now provides better UX by setting clear expectations about whether
you're looking at fresh feedback or outdated suggestions. Particularly useful
during rapid iteration when you're pushing frequent commits and want to know if
you should wait for a new review.

**Fixes issue:** [#69](https://github.com/fini-net/template-repo/issues/69)
**Related PR:** [#70](https://github.com/fini-net/template-repo/pull/70)

### v4.5 - Smart Polling for PR Checks

Replaced the fixed 8-second sleep in the `pr` recipe with an intelligent polling
loop that waits for GitHub checks to actually start running. Previously, we'd
always wait 8 seconds after creating a PR before watching checks - wasting time
when GitHub responded quickly (2-3 seconds) and occasionally failing when GitHub
was slow (10+ seconds).

The new `_wait_for_checks` recipe polls the GitHub API every 2 seconds with a
30-second timeout, exiting immediately when checks appear. This provides:

- **Faster feedback** - No wasted time when GitHub responds quickly (typically 2-6s vs fixed 8s)
- **More reliable** - Handles slow API responses gracefully (up to 30 seconds)
- **Better UX** - Animated spinner via `gum spin` shows "Waiting for GitHub checks to start..."
- **Graceful degradation** - Falls back to simple progress dots when `gum` not available
- **Smart timeout** - Continues with warning message if checks never appear

The polling function is declared separately so it can be exported to `gum spin`'s
subshell context. Uses colored output (GREEN for success, YELLOW for timeout) and
the `USING_GUM` environment variable to conditionally show progress indicators
based on available tooling.

**Fixes issue:** [#60](https://github.com/fini-net/template-repo/issues/60)
**Related PR:** [#61](https://github.com/fini-net/template-repo/pull/61)

## December 2025 - Finer refinements

### v4.4 - PR Update Blank Line Preservation

Fixed a bug in the `pr_update` recipe where blank lines after the Done section
were being incorrectly removed. The AWK script that preserves sections after
Done had a logic error - after setting `after_done=1`, it continued to match
and skip blank lines because the condition `in_done && /^$/` remained true
even after transitioning to the "after done" state.

- Added `!after_done` guard to blank line and commit matching conditions
- Prevents eating blank lines once we've moved past the Done section
- Preserves original spacing between sections (e.g., Done and Meta)

The fix ensures that when `pr_update` regenerates the Done section with current
commits, it maintains proper markdown formatting with blank lines separating
different sections of the PR description.

**Related PR:** [#50](https://github.com/fini-net/template-repo/pull/50)

### v4.3 - Release Tag Visibility

Enhanced the `release` recipe to automatically pull the newly created tag so it's
immediately visible in your local repository. Previously, after running
`just release v1.2.3`, the tag would exist on GitHub but wouldn't show up in
`git tag` locally until you manually ran `git pull`. Now the workflow handles
this for you.

- Added `git pull` command after `gh release create`
- Included 1-second sleep to allow GitHub API to finish processing
- Makes the release workflow feel more complete and immediate

This is a small quality-of-life improvement that removes a tiny paper cut from
the release process. When you create a release, you should see it locally right
away without extra steps.

### v4.2 - Prerequisites Installation Script

Added a standalone shell script to automate installation and verification of all
prerequisites needed to run the just recipes in this repository:

- **`install-prerequisites.sh`** - Intelligent installation helper that checks
  for all required tools (just, gh, shellcheck, markdownlint-cli2, jq) and
  either auto-installs them (macOS with Homebrew) or provides the appropriate
  installation commands (Linux with apt-get, dnf, or pacman). Shows what's
  already installed vs. what's missing with clear colored output. Includes
  proper error handling for missing package managers and Node.js/npm for
  markdownlint-cli2. Makes onboarding new contributors or setting up new
  development environments significantly smoother.

The script is fully executable, passes shellcheck validation, and provides a
friendly user experience with color-coded output and helpful error messages.
Run `./.just/lib/install-prerequisites.sh` to check your environment or install
missing tools.

**Related PR:** [#48](https://github.com/fini-net/template-repo/pull/48)

### v4.1 - Release Monitoring and Iteration Workflow

Added three new recipes to improve release management and iterative PR workflows:

- **`release_age`** - Checks how long ago the last release was published and
  provides actionable feedback. Displays the release tag, publication date, age
  in days, and commit count since release. Warns (in yellow) if the release is
  more than 60 days old, suggesting it might be time for a new release. Works
  cross-platform with both GNU date (Linux) and BSD date (macOS). Uses `gh`
  API for robust JSON parsing of release data.

- **`claude_review`** - Broke out Claude Code review comment display into its
  own standalone recipe. Previously only callable via `pr_checks`, it can now
  be run independently to quickly see Claude's latest PR comment without
  re-running all the checks. Still chains automatically from `pr_checks` for
  the full workflow.

- **`again`** - Convenience recipe for iterative PR development. Chains
  together the common workflow of pushing new commits, updating the PR
  description with current commits, and re-watching the PR checks. Saves
  typing when you're in the flow of making changes, getting feedback, and
  iterating. Includes a 2-second sleep between PR update and check watching
  to give GitHub's API time to catch up.

These recipes improve different phases of the development cycle - `release_age`
for project maintenance awareness, `claude_review` for quick feedback access,
and `again` for rapid PR iteration.

**Related PR:** [#46](https://github.com/fini-net/template-repo/pull/46)

## November 2025 - The Polish Updates

### v4.0 - PR Description Management

Added two new recipes for managing pull request descriptions dynamically:

- **`pr_update`** - Updates the "Done" section of the PR description with the
  current list of commits from the branch. Extracts commits using `git cherry`,
  preserves other sections (Meta, Verify, etc.), and updates the PR body via
  `gh pr edit`. Useful when you add commits after PR creation and want to keep
  the description in sync.

- **`pr_verify`** - Adds or appends content to a "Verify" section in the PR
  description. Reads from stdin, timestamps each entry, and formats as a code
  block. If no Verify section exists, creates one before the Meta section. If
  one exists, appends new timestamped entries. Perfect for logging test results
  or verification steps.

Both recipes include a new sanity check (`_on_a_pull_request`) that verifies
you're on a branch with an active pull request before attempting updates. This
prevents cryptic errors when running these commands outside of PR context.

Other improvements in this release:

- Simplified bash strict mode settings (removed `-x` tracing flag)
- Standardized PR existence checks across recipes
- Better error handling with exit code 103 for missing PRs
- Initialize awk variables properly to avoid undefined behavior
- Updated documentation to show new recipes

**Related PR:** [#44](https://github.com/fini-net/template-repo/pull/44)

### v3.9 - Shellcheck Error Fixes

Before adding the shellcheck tooling in v3.8bis, we knew there were a bunch of
shellcheck warnings in the gh-process module itself. This release fixes all
of those issues - better variable quoting, improved conditional syntax, and
other shellcheck best practices. Nothing user-facing changed, but the code is
now cleaner and more robust. This should also mean that our future AI code
reviews will have less trivial stuff to complain about.

**Related PR:** [#40](https://github.com/fini-net/template-repo/pull/40)

### v3.8bis - Shellcheck Integration

Added a whole new module for linting bash scripts embedded in just recipes.
The `shellcheck` recipe extracts bash scripts from all justfiles in the repo,
writes them to temporary files, and runs shellcheck on each one. It's pretty
meta - using just to check just recipes.

- New `.just/shellcheck.just` module with 138 lines of awk magic
- Automatically finds recipes with bash shebangs
- Detailed reporting showing which file and recipe each issue is in
- Purple section headings because why not
- Returns proper exit codes for CI integration

This immediately found issues in our own code, which led to v3.9.

**Related PRs:** [#37](https://github.com/fini-net/template-repo/pull/37), [#39](https://github.com/fini-net/template-repo/pull/39)

### v3.8 - Git Alias Expansion

Expanded all git aliases to use standard git commands, making this justfile
work for everyone without requiring custom git configuration. Previously,
you needed my personal git aliases (`stp`, `pushup`, `co`) configured to use
this workflow. Now it just works out of the box.

- `git stp` → `git status --porcelain`
- `git pushup` → `git push -u origin HEAD`
- `git co` → `git checkout`

Added inline comments showing the old alias names for reference, so if you're
used to seeing `stp` in the output, you know what's happening.

**Related PR:** [#35](https://github.com/fini-net/template-repo/pull/35)

### v3.7 - Pre-PR Hook Support

Added support for optional pre-PR hooks to allow project-specific automation
before creating pull requests. The `pr` recipe now checks for
`.just/pr-hook.just` and runs it if present. This is particularly useful for
projects that need to rebuild assets (like Hugo sites) before pushing.

- Added conditional hook execution in PR workflow
- Hidden `_pr-hook` recipe (internal use)
- Updated documentation with workflow versioning

**Related PRs:** [#32](https://github.com/fini-net/template-repo/pull/32), [#33](https://github.com/fini-net/template-repo/pull/33)

### v3.6 - Quote Consistency

Improved shell script robustness with more consistent quoting of variables and
just template parameters. Small change, but makes the scripts more reliable
when dealing with branch names or paths that might contain spaces.

**Related PR:** [#31](https://github.com/fini-net/template-repo/pull/31)

### v3.5 - Spacing and Multi-Commit Handling

Cleaned up the codebase and improved handling of branches with multiple commits:

- Better formatting and spacing throughout
- Cleaned up vestigial variables from earlier iterations
- Improved quoting of just variables
- More consistent handling of multiple commits on a branch

**Related PR:** [#30](https://github.com/fini-net/template-repo/pull/30)

## October 2025 - The AI Review Update

### v3.4 - Graceful Failure Handling

Fixed an issue where broken GitHub Actions would prevent the review comments
from being displayed. The workflow now continues to show AI reviews even if
some checks fail, because you probably want to see those reviews even more when
things are broken.

**Related PR:** [#26](https://github.com/fini-net/template-repo/pull/26)

### v3.3 - Copilot and Claude Review Integration

This was a big one. After PR checks complete, the workflow now automatically
fetches and displays comments from both GitHub Copilot and Claude Code reviews
right in your terminal. No more switching to the browser to see what the bots
think.

- Added GraphQL query to fetch Copilot PR review comments
- Displays Copilot suggestions after PR checks complete
- Shows Claude's most recent comment
- Uses `jq` to parse and filter review data

**Related PR:** [#25](https://github.com/fini-net/template-repo/pull/25)

## August 2025 - The Safety Update

### v3.2 - Commit Verification

Added a sanity check to prevent accidentally creating empty PRs. The `pr` recipe now verifies that your branch actually has commits before allowing you to create a pull request. Uses `git cherry` to compare against the release branch.

- New `_has_commits` dependency check
- Clear error message when branch is empty
- Exit code 101 for tracking

**Related PR:** [#21](https://github.com/fini-net/template-repo/pull/21)

### Faster PR Check Monitoring

Changed the PR checks watcher to poll every 5 seconds instead of the default. Because who wants to wait around? GitHub's API might be lazy, but we don't have to be.

**Related PR:** [#20](https://github.com/fini-net/template-repo/pull/20)

## June 2025 - The Beginning of this file

### v3.1 - Initial Release

Created as part of a larger refactoring effort to modularize the main justfile.
This file extracted all the Git/GitHub workflow automation into a separate
module.

Core recipes included from day one:

- `sync` - Return to main and pull latest
- `pr` - Create PR from current branch
- `merge` - Squash merge and clean up
- `branch` - Create dated feature branches
- `prweb` - Open PR in browser
- `release` - Create GitHub releases
- `pr_checks` - Watch PR checks (later enhanced)

Plus a bunch of sanity check helpers (`_on_a_branch`, `_main_branch`, etc.) to
keep you from footgunning yourself.

**Related PR:** [#11](https://github.com/fini-net/template-repo/pull/11)

---

## Pre-history

Earlier versions of this code came from the `/justfile` in
[this repo](https://github.com/fini-net/template-repo/blob/main/justfile)
and some of my other repos, primarily
[www-chicks-net](https://github.com/chicks-net/www-chicks-net/blob/main/justfile).
It all started [very simply](https://github.com/chicks-net/www-chicks-net/commit/06f28b13d82e445951b10af1a57488a1dc9e1069).

I think there were some 2.x versions, but I haven't found them again.
