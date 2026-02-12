# project justfile

import? '.just/compliance.just'
import? '.just/gh-process.just'
import? '.just/pr-hook.just'
import? '.just/shellcheck.just'
import? '.just/cue-verify.just'
import? '.just/claude.just'
import? '.just/copilot.just'
import? '.just/repo-toml.just'
import? '.just/template-sync.just'

# list recipes (default works without naming it)
[group('Utility')]
list:
	just --list
	@echo "{{GREEN}}Your justfile is waiting for more scripts and snippets{{NORMAL}}"

# build the gh-observer binary and install locally
[group('Build')]
build:
	go build -o gh-observer
	gh ext remove observer
	gh ext install .

# check release workflow status and list binaries
[group('Process')]
release_status:
	#!/usr/bin/env bash
	set -euo pipefail

	# Check if standard release workflow is enabled
	if [[ -e ".just/repo-toml.sh" ]]; then
		source .just/repo-toml.sh
	else
		FLAG_STANDARD_RELEASE="true"
	fi

	if [[ "$FLAG_STANDARD_RELEASE" != "true" ]]; then
		echo "{{BLUE}}Standard release workflow is disabled (see .repo.toml flags.standard-release){{NORMAL}}"
		exit 0
	fi

	echo "{{BLUE}}Checking release workflow status...{{NORMAL}}"
	echo ""

	# Get latest release tag
	release_tag=$(gh release list --limit 1 --json tagName -q '.[0].tagName' 2>/dev/null)

	if [[ -z "$release_tag" ]]; then
		echo "{{YELLOW}}No releases found{{NORMAL}}"
		exit 0
	fi

	echo "Latest release: {{CYAN}}$release_tag{{NORMAL}}"
	echo ""

	# Check workflow runs for release.yml
	echo "{{BLUE}}Recent release workflow runs:{{NORMAL}}"
	gh run list --workflow=release.yml --limit 5
	echo ""

	# List release assets
	echo "{{BLUE}}Release assets for $release_tag:{{NORMAL}}"
	gh release view "$release_tag" --json assets --jq '.assets[].name' | while read -r asset; do
		echo "  ✓ $asset"
	done
