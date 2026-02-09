# .just directory

Just recipe files live in this directory.

## Source of Truth

The master copy of these project files lives in the
[FINI template-repo](https://github.com/fini-net/template-repo)
and you should be able to copy them into your
project for updates.

You should read our
[release notes](https://github.com/fini-net/template-repo/blob/main/.just/RELEASE_NOTES.md)
to learn more about the history of how we have evolved the github PR process.

We use the
[repos-summary script](https://github.com/chicks-net/chicks-home/blob/main/bin/repos-summary)
to see which of our repos need updates of the just files.

## Included Just Files

### gh-process.just - Git/GitHub Workflow Automation

Core PR lifecycle management with these features:

#### Branch Management

- **Branch creation** - `just branch <name>` creates dated branches in `$USER/YYYY-MM-DD-<name>` format
- **Branch escape** - `just sync` returns to main branch and pulls latest changes
- **UTC date utility** - `just utcdate` prints UTC date in ISO format (used internally for branch names)

#### PR Creation and Updates

- **PR creation** - `just pr` creates PRs using first commit message as title, all commits in body
- **PR description updates** - `just pr_update` regenerates the Done section with current commits while preserving other sections
- **PR verification logs** - `just pr_verify` adds or appends timestamped content to a Verify section (reads from stdin)
- **Iterative workflow** - `just again` chains push, PR update, and checks monitoring for rapid iteration
- **Web viewing** - `just prweb` opens current PR in browser

#### PR Checks and AI Reviews

- **PR checks monitoring** - `just pr_checks` watches GitHub Actions checks with 5-second polling
- **AI integration** - Displays both GitHub Copilot and Claude Code review comments after checks complete
- **Standalone Claude review** - `just claude_review` shows Claude's latest PR comment without re-running checks

#### Merge and Release

- **PR merge** - `just merge` squash merges, deletes remote branch, returns to main, and pulls latest
- **Releases** - `just release <version>` creates GitHub releases with auto-generated notes and pulls the new tag locally
- **Release monitoring** - `just release_age` checks how long ago the last release was published, warns if >60 days old

#### Safety and Automation

- **Sanity checks** - Hidden recipes (`_on_a_branch`, `_has_commits`, `_main_branch`, `_on_a_pull_request`) prevent mistakes
- **Pre-PR hooks** - Optional integration with `pr-hook.just` for project-specific automation

### compliance.just - Repository Health Checks

Custom compliance validation for GitHub community standards:

- **README.md** validation
- **LICENSE** file check
- **Code of Conduct** verification (`.github/CODE_OF_CONDUCT.md`)
- **Contributing Guide** check (`.github/CONTRIBUTING.md`)
- **Security Policy** validation (`.github/SECURITY.md`)
- **Pull Request Template** check (`.github/pull_request_template.md`)
- **Issue Templates** directory validation (`.github/ISSUE_TEMPLATE/`)
- **Repository description** check via GitHub API
- **CODEOWNERS** file validation
- **`.gitignore`** file check
- **`.gitattributes`** file verification
- **`justfile`** presence check
- **`.editorconfig`** validation

All checks include colorized output with helpful (and sometimes sarcastic) messages.

### shellcheck.just - Bash Script Linting

Automated shellcheck validation for bash scripts in just recipes:

- **Automatic detection** - Scans all justfiles in repo (`justfile` and `.just/*.just`)
- **Script extraction** - Uses awk to extract bash scripts (looks for `#!/usr/bin/env bash` or `#!/bin/bash`)
- **Temporary file handling** - Creates temp files for each script and cleans up on exit
- **Shellcheck execution** - Runs shellcheck with `-x -s bash` flags for thorough validation
- **Detailed reporting** - Shows which file and recipe each issue is in, with colorized output
- **Statistics** - Reports total scripts checked and issues found
- **Exit codes** - Returns 1 if issues found, 0 if all scripts pass

### pr-hook.just - Pre-PR Hook Template

Optional pre-PR automation hook:

- **Called automatically** - Invoked by `just pr` if this file exists
- **Placeholder implementation** - Currently just prints a message
- **Customizable** - Replace with project-specific tasks (e.g., Hugo rebuilds, asset compilation)
- **Hidden recipe** - Uses `_pr-hook` naming to indicate internal use only

### install-prerequisites.sh - Prerequisites Installation Helper

Standalone shell script that automates installation and verification of all required tools:

- **Automatic detection** - Checks for all required tools: `just`, `gh`, `shellcheck`, `markdownlint-cli2`, `jq`
- **Smart installation** - On macOS with Homebrew, automatically installs missing tools
- **Manual guidance** - On Linux, provides appropriate installation commands for apt-get, dnf, or pacman
- **Clear reporting** - Shows what's already installed vs. what's missing with color-coded output
- **Error handling** - Detects missing package managers, provides helpful error messages
- **Verification mode** - Run multiple times to verify installations completed successfully

Run `./.just/lib/install-prerequisites.sh` to check your environment or install missing tools. Makes onboarding new contributors significantly smoother.
