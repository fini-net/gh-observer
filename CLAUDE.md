# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Repository Purpose

This is a GitHub repository template that implements best practices for open source projects. It's designed to be cloned and customized for new repositories. The template includes GitHub community standards compliance, automated workflows, and a command-line driven development process.

## Development Workflow

This repo uses `just` (command runner) for all development tasks. The workflow is entirely command-line based using `just` and the GitHub CLI (`gh`).

### Standard development cycle

1. `just branch <name>` - Create a new feature branch (format: `$USER/YYYY-MM-DD-<name>`)
2. Make changes and commit (first commit message becomes PR title)
3. `just pr` - Create PR, push changes, and watch checks
4. `just merge` - Squash merge PR, delete branch, return to main, and pull latest
5. `just sync` - Return to main branch and pull latest (escape hatch)

### AI Review Workflows

**Copilot Review:**

1. `just pr` - Create PR (triggers initial Copilot review if enabled)
2. Make changes based on review feedback
3. `just copilot_refresh` - Request fresh review after changes
4. `just copilot_pick` - Browse suggestions interactively using gum
5. Address suggestions and iterate

**Claude Review:**

1. `just pr` - Create PR (triggers initial Claude review if enabled)
2. `just claude_review` - View Claude's latest PR code review comments
3. Make changes based on review feedback
4. Iterate as needed

### Additional commands

- `just` or `just list` - Show all available recipes
- `just prweb` - Open current PR in browser
- `just again` - Push changes, update PR description, and watch GHAs
- `just pr_update` - Update the "Done" section of PR description with current commits
- `just pr_verify` - Add or append to "Verify" section from stdin (with timestamp)
- `just copilot_pick` - Interactively browse and select Copilot PR review suggestions using gum
- `just copilot_refresh` - Request a new Copilot review on current PR
- `just claude_review` - View Claude's latest PR code review comments
- `just pr_body_test` - Test PR body update logic
- `just template_sync_test` - Test template sync logic
- `just release <version>` - Create a GitHub release with auto-generated notes
- `just release_age` - Check how long ago the last release was
- `just clean_template` - Generate a clean README from template (strips template documentation) and removes other files not needed in new repos
- `just compliance_check` - Run custom repo compliance checks
- `just shellcheck` - Run shellcheck on all bash scripts in just recipes
- `just cue-verify` - Verify `.repo.toml` validity and flag configuration
- `just cue-sync-from-github` - Sync description and topics from GitHub API into `.repo.toml`
- `just repo_toml_generate` - Generate shell variables from `.repo.toml`
- `just repo_toml_check` - Check if generated file is up-to-date
- `just claude_permissions_sort` - Sort Claude Code permissions in canonical order
- `just claude_permissions_check` - Check Claude Code permissions structure
- `just utcdate` - Print UTC date in ISO format (used in branch names)
- `just checksums_generate` - Generate versioned checksums from git history (template-repo only)
- `just checksums_verify` - Check local .just files against template versions
- `just checksums_diff <file>` - Show diff between local and latest template version
- `just update_from_template` - Update .just modules from template-repo (safe, preserves local mods)

## Architecture

### Modular justfile structure

The main `justfile` imports ten modules:

- `.just/compliance.just` - Custom compliance checks for repo health (validates all GitHub community standards)
- `.just/gh-process.just` - Git/GitHub workflow automation (core PR lifecycle)
- `.just/pr-hook.just` - Optional pre-PR hooks for project-specific automation (e.g., Hugo rebuilds)
- `.just/shellcheck.just` - Shellcheck linting for bash scripts in just recipes
- `.just/cue-verify.just` - File format validation using Cue
- `.just/claude.just` - Claude Code permission management
- `.just/copilot.just` - GitHub Copilot integration recipes (interactive suggestion picker, review refresh)
- `.just/repo-toml.just` - Repository metadata extraction and shell variable generation
- `.just/testing.just` - Test recipes for PR body updates and template sync logic
- `.just/template-sync.just` - Template synchronization and update system

### Repository metadata extraction

The `.just/repo-toml.just` module provides shell variable generation:

- **Variable generation** - Exports `.repo.toml` data as sourceable shell variables
- **Automatic derivation** - Computes `ORG_NAME` and `REPO_NAME` from URLs
- **Format conversion** - Handles TOML arrays as both bash arrays and CSV strings
- **Conditional logic** - Enables flag-based feature toggling in recipes

The generated `.just/repo-toml.sh` file is gitignored and regenerated on demand using `just repo_toml_generate`.

### Git/GitHub workflow details

The `.just/gh-process.just` module implements the entire PR lifecycle:

- **Branch creation** - Dated branches with `$USER/YYYY-MM-DD-<name>` format
- **PR creation** - First commit message becomes PR title, all commits listed in "Done" section of PR body
- **Sanity checks** - Prevents empty PRs, enforces branch strategy via hidden recipes (`_on_a_branch`, `_has_commits`, `_main_branch`, `_on_a_pull_request`, `_wait_for_checks`)
- **Check watching** - Polls GitHub checks every 5 seconds with smart waiting (waits up to 30s for checks to start)
- **AI integration** - After PR checks complete, conditionally displays GitHub Copilot and Claude Code review comments based on `.repo.toml` flags (`copilot-review` and `claude-review`)
- **Merge automation** - Squash merge, delete remote branch, return to main, pull latest
- **PR updates** - `pr_update` refreshes the "Done" section with current commits; `pr_verify` adds timestamped verification outputs
- **copilot_pick** - Fetches Copilot PR review suggestions and presents them in an interactive picker (requires gum)

### Repository metadata system

The `.repo.toml` file contains structured metadata:

- **about section** - Description, topics, and license
- **urls section** - Git SSH and web URLs
- **flags section** - Boolean feature flags (claude, claude-review, copilot-review, standard-release)

The `cue-verify` recipe validates `.repo.toml` in three stages:

1. Cue validates TOML structure and types against `docs/repo-toml.cue` schema
2. Validates flags match actual configuration (checks for required files)
3. Validates GitHub metadata sync (description and topics match GitHub API)

### Shellcheck integration

The `.just/shellcheck.just` module extracts and validates bash scripts:

- **Script extraction** - Uses awk to identify recipes with bash shebangs (`#!/usr/bin/env bash` or `#!/bin/bash`)
- **Automatic detection** - Scans all justfiles in repo (main `justfile` and `.just/*.just`)
- **Temporary file handling** - Creates temporary files for each script and runs shellcheck with `-x -s bash` flags
- **Detailed reporting** - Shows which file and recipe each issue is in, with colored output
- **Exit code** - Returns 1 if issues found, 0 if all scripts pass

### Claude Code permission management

The `.just/claude.just` module manages `.claude/settings.local.json`:

- **Canonical sorting** - Groups permissions by type (Bash, WebFetch, WebSearch, Other) and sorts alphabetically within groups
- **Structure validation** - Checks for required JSON structure and permission arrays
- **Backup handling** - Creates backups before modifications and restores on error
- **Permission analytics** - Reports counts and breakdown by permission type

### Standard release workflow

The template includes a standard release workflow controlled by the `standard-release` flag:

- **Default behavior** - When enabled (`standard-release = true`), provides `release` and `release_age` recipes
- **Release creation** - `just release <version>` creates GitHub releases with auto-generated notes
- **Release monitoring** - `just release_age` checks release freshness and commit count since last release
- **Custom workflows** - Set `standard-release = false` in `.repo.toml` to disable standard recipes for projects with custom release mechanisms
- **Graceful degradation** - When disabled, recipes display informational messages and exit cleanly (exit 0)

### Template sync system

The `.just/template-sync.just` module enables safe updates from template-repo:

- **Multi-version checksums** - Tracks all historical versions of .just modules in `.just/CHECKSUMS.json`
- **Safe updates** - Only modifies files matching a known template version
- **Local preservation** - Files with modifications are skipped and reported
- **Clear reporting** - Shows updated/skipped/new files with version info
- **Diagnostic tools** - `checksums_verify` and `checksums_diff` for preview and inspection

The system uses three core scripts in `.just/lib/`:

- **generate_checksums.sh** - Extract checksums from git history with version tracking
- **template_update.sh** - Core update logic (compare, download, verify, rollback on failure)
- **template_sync_test.sh** - Test suite with fixtures in `.just/test/fixtures/template_sync/`

### GitHub Actions

Workflows in `.github/workflows/`:

- **markdownlint.yml** - Enforces markdown standards using `markdownlint-cli2`
- **checkov.yml** - Security scanning for GitHub Actions (continues on error, outputs SARIF)
- **actionlint.yml** - Lints GitHub Actions workflow files
- **auto-assign.yml** - Automatically assigns issues/PRs to `chicks-net`
- **claude-code-review.yml** - Claude AI review automation
- **claude.yml** - Additional Claude integration
- **cue-verify.yml** - Validates `.repo.toml` format and flags
- **pr-body-tests.yml** - Tests PR body update logic
- **template-sync.yml** - Tests template synchronization system

### Testing infrastructure

The `.just/testing.just` module provides automated testing:

- **pr_body_test** - Tests PR body update logic using `.just/lib/pr_body_test.sh`
- **template_sync_test** - Tests template sync logic using `.just/lib/template_sync_test.sh`
- **Test fixtures** - Located in `.just/test/fixtures/template_sync/`

Both test recipes run via GitHub Actions on every PR to ensure core functionality works.

### Markdown linting

Configuration in `.markdownlint.yml`:

- MD013 (line length) is disabled
- MD041 (first line h1) is disabled
- MD042 (no empty links) is disabled
- MD004 (list style) enforces dashes
- MD010 (tabs) ignores code blocks

Run locally: `markdownlint-cli2 **/*.md`

## Template customization

When using this template for a new project:

1. Search and replace:
   - `fini-net` → your GitHub org
   - `template-repo` → your repo name
   - `chicks-net` → your username (especially in `.github/workflows/auto-assign.yml`)
2. Update `.repo.toml` with your repository metadata
3. Run `just clean_template` to strip template documentation and files from README
4. Run `just cue-sync-from-github` to sync description and topics from GitHub
5. Run `just compliance_check` to verify all community standards files are in place

## Important implementation notes

- All git commands in `.just/gh-process.just` use standard git (no aliases required)
- The `pr` recipe runs optional pre-PR hooks if `.just/pr-hook.just` exists
- PR checks poll every 5 seconds for faster feedback, with smart startup waiting
- The `.just` directory contains modular just recipes that can be copied to other projects for updates
- just catches errors from commands when the recipe isn't a shebang form that runs another scripting engine
- just colors come from built-in constants: `{{GREEN}}`, `{{BLUE}}`, `{{RED}}`, `{{YELLOW}}`, `{{NORMAL}}`
- Hidden recipes (prefixed with `_`) are internal helpers and not shown in `just --list`
- The `again` recipe is for iterating on PRs: push, update description, watch checks
- Release notes for workflow changes are tracked in `.just/RELEASE_NOTES.md`

## Dependencies

### Required tools

- `just` - Command runner for all recipes
- `gh` - GitHub CLI for PR and release management
- `git` - Version control

### Optional tools

- **gum** - Interactive selection tool for `copilot_pick` recipe
  - Install: `brew install gum` (macOS) or see <https://github.com/charmbracelet/gum>
  - Usage: `just copilot_pick` (must be run from a branch with an open PR)
