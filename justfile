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
import? '.just/vex.just'

# list recipes (default works without naming it)
[group('Utility')]
list:
	just --list
	@echo "{{GREEN}}Your justfile is waiting for more scripts and snippets{{NORMAL}}"

# record a cast for a PR to help AI understand problems
[group('Testing')]
test2cast pr="25": build
	asciinema record -c "./gh-observer {{ pr }}" --overwrite ".cache/pr-{{ pr }}.cast"

# record a cast for repo mode to help AI understand problems
[group('Testing')]
test2cast-repo repo="fini-net/gh-observer": build
	#!/usr/bin/env bash
	set -euo pipefail
	# repo mode is persistent (never auto-quits), so we wrap gh-observer
	# in GNU `timeout`, which sends SIGTERM after 60s; bubbletea treats
	# SIGTERM as a clean QuitMsg and exits, letting asciinema finalize the
	# cast. Requires GNU coreutils `timeout` (brew install coreutils on
	# macOS).
	repo="{{ repo }}"
	owner="${repo%/*}"
	name="${repo#*/}"
	out="docs/repo_${owner}_${name}.cast"
	TIMEOUT="$(command -v timeout || command -v gtimeout)" || {
		echo "error: GNU 'timeout' not found. Install with: brew install coreutils" >&2
		exit 1
	}
	mkdir -p docs
	asciinema record --headless --idle-time-limit 2 --overwrite \
		--command "$TIMEOUT --signal=SIGTERM 60 ./gh-observer --repo ${repo}" "$out"

# record the merged-PR demo cast (feeds 'just gif merged-pr2')
[group('Testing')]
cast-merged-pr pr="": build
	#!/usr/bin/env bash
	set -euo pipefail

	# watches the newest merged PR by a human author (renovate/dependabot
	# PRs are skipped - version bumps make dull demos); pass a PR number
	# to override. Headless with the command echoed first so the GIF
	# shows what was run, and no --idle-time-limit because the README
	# promises this animation is real-time (not accelerated). Produces
	# .cache/merged-pr.cast.

	pr="{{ pr }}"
	if [[ -z "$pr" ]]; then
		list_rc=0
		pr=$(gh pr list --state merged --limit 20 --json number,author \
			--jq '[.[] | select(((.author.login // "") | test("renovate|dependabot"; "i")) | not)][0].number') || list_rc=$?
		if [[ "$list_rc" -ne 0 ]]; then
			echo "{{RED}}Error: 'gh pr list' failed - is gh authenticated?{{NORMAL}}" >&2
			exit 1
		fi
		if [[ -z "$pr" || "$pr" == "null" ]]; then
			echo "{{RED}}Error: no non-bot merged PR in the last 20{{NORMAL}}" >&2
			echo "               pass one explicitly: just cast-merged-pr 123" >&2
			exit 1
		fi
	fi

	mkdir -p .cache
	echo "{{BLUE}}Recording 'gh observer $pr' (headless, real-time)...{{NORMAL}}"
	# echo the command first so the GIF opens with the line a user would
	# type; no --return: gh observer's exit code mirrors the PR's checks
	# (non-blocking red runs are common on this repo's merged PRs), not
	# the recording's success
	inner="echo \"\$ gh observer $pr\"; exec gh observer $pr"
	asciinema record --headless --overwrite \
		--command "bash -c '$inner'" .cache/merged-pr.cast
	if [[ ! -s .cache/merged-pr.cast ]]; then
		echo "{{RED}}Error: asciinema produced no recording{{NORMAL}}" >&2
		exit 1
	fi
	echo "{{GREEN}}✓ recorded .cache/merged-pr.cast{{NORMAL}}"

# record the remote-repo demo cast (feeds 'just gif uncloned-repo')
[group('Testing')]
cast-uncloned-repo url="": build
	#!/usr/bin/env bash
	set -euo pipefail

	# watches a PR by full URL on a repo we do not have cloned locally;
	# defaults to the nautilus URL the README documents (its failing
	# 'Static checks' job is part of the demo). Headless with the command
	# echoed first, and no --idle-time-limit: real-time per README.
	# Produces .cache/uncloned-repo.cast.

	url="{{ url }}"
	if [[ -z "$url" ]]; then
		url="https://github.com/MartinDelille/nautilus/pull/13"
	fi
	mkdir -p .cache
	echo "{{BLUE}}Recording 'gh observer $url' (headless, real-time)...{{NORMAL}}"
	inner="echo \"\$ gh observer $url\"; exec gh observer $url"
	asciinema record --headless --overwrite \
		--command "bash -c '$inner'" .cache/uncloned-repo.cast
	if [[ ! -s .cache/uncloned-repo.cast ]]; then
		echo "{{RED}}Error: asciinema produced no recording{{NORMAL}}" >&2
		exit 1
	fi
	echo "{{GREEN}}✓ recorded .cache/uncloned-repo.cast{{NORMAL}}"

# re-render one README animation GIF from an asciinema cast
[group('Utility')]
gif target="active-pr" speed="" cast="":
	#!/usr/bin/env bash
	set -euo pipefail

	# targets: merged-pr2 (1x), uncloned-repo (1x), active-pr (2x),
	# descriptions2 (10x). Speed defaults follow README.md; pass a
	# number to override. Cast defaults to a fresh .cache recording when
	# one exists, the committed docs cast otherwise (pass a path to
	# override); .cache sources are archived to docs/ next to their GIF
	# for provenance. usage: just gif <target> [speed] [cast]

	target="{{ target }}"
	speed="{{ speed }}"
	cast="{{ cast }}"

	# README-documented playback speed + output/provenance paths per target
	case "$target" in
		merged-pr2)
			doc_speed=1
			gif=docs/gh-observer-merged-pr2.gif
			docs_cast=docs/gh-observer-merged-pr2.cast
			;;
		uncloned-repo)
			doc_speed=1
			gif=docs/gh-observer-uncloned-repo.gif
			docs_cast=docs/gh-observer-uncloned-repo.cast
			;;
		active-pr)
			doc_speed=2
			gif=docs/gh-observer-active-pr.gif
			docs_cast=docs/gh-observer-active-pr.cast
			;;
		descriptions2)
			doc_speed=10
			gif=docs/gh-observer-descriptions2.gif
			docs_cast=docs/gh-observer-descriptions2.cast
			;;
		*)
			echo "{{RED}}Error: unknown target '$target' - valid: merged-pr2, uncloned-repo, active-pr, descriptions2{{NORMAL}}" >&2
			exit 1
			;;
	esac

	if [[ -z "$speed" ]]; then
		speed="$doc_speed"
	fi

	# resolve the source cast: explicit path > fresh .cache recording >
	# committed docs cast (mtime wins; 'just pr' flows land in .cache/)
	if [[ -z "$cast" ]]; then
		case "$target" in
			merged-pr2)
				if [[ -f .cache/merged-pr.cast ]]; then
					cast=.cache/merged-pr.cast
				fi
				;;
			uncloned-repo)
				if [[ -f .cache/uncloned-repo.cast ]]; then
					cast=.cache/uncloned-repo.cast
				fi
				;;
			active-pr)
				for f in .cache/pr*.cast; do
					if [[ -f "$f" ]]; then
						if [[ -z "$cast" || "$f" -nt "$cast" ]]; then
							cast="$f"
						fi
					fi
				done
				if [[ -z "$cast" ]]; then
					echo "{{RED}}Error: no .cache/pr*.cast recordings - create one with 'just pr' or 'just test2cast <pr>'{{NORMAL}}" >&2
					exit 1
				fi
				;;
			descriptions2)
				# newest recording that actually shows super-linter, whose
				# description-field output is the point of this animation
				for f in .cache/pr*.cast; do
					if [[ -f "$f" ]]; then
						if grep -qi "super-linter" "$f"; then
							if [[ -z "$cast" || "$f" -nt "$cast" ]]; then
								cast="$f"
							fi
						fi
					fi
				done
				if [[ -z "$cast" ]]; then
					echo "{{YELLOW}}no .cache recording shows super-linter - using the committed cast{{NORMAL}}" >&2
				fi
				;;
		esac
	fi

	# fall back to the committed provenance cast when nothing fresh exists
	if [[ -z "$cast" ]]; then
		cast="$docs_cast"
		if [[ ! -f "$cast" ]]; then
			echo "{{RED}}Error: $cast not found{{NORMAL}}" >&2
			exit 1
		fi
	fi

	# archive .cache sources to docs/ so each GIF keeps its provenance
	# cast; headless recordings carry no embedded terminal theme (no real
	# terminal answers the OSC color queries), so borrow the committed
	# cast's theme to keep the re-rendered GIF looking established
	if [[ "$cast" == .cache/* ]]; then
		if [[ -f "$docs_cast" ]] \
			&& ! head -n 1 "$cast" | jq -e '.term.theme != null' >/dev/null 2>&1 \
			&& head -n 1 "$docs_cast" | jq -e '.term.theme != null' >/dev/null 2>&1; then
			old_theme=$(head -n 1 "$docs_cast" | jq -c '.term.theme')
			head -n 1 "$cast" | jq -c --argjson theme "$old_theme" '.term.theme = $theme' > "$docs_cast"
			tail -n +2 "$cast" >> "$docs_cast"
			echo "{{BLUE}}archived $cast -> $docs_cast (borrowed theme){{NORMAL}}"
		else
			cp "$cast" "$docs_cast"
			echo "{{BLUE}}archived $cast -> $docs_cast{{NORMAL}}"
		fi
		cast="$docs_cast"
	fi

	if ! command -v agg >/dev/null 2>&1; then
		echo "{{RED}}Error: agg not found on PATH{{NORMAL}}" >&2
		echo "               install with: brew install agg" >&2
		exit 1
	fi
	agg --speed "$speed" "$cast" "$gif"
	echo "{{GREEN}}✓ $gif  <-  $cast  @ ${speed}x{{NORMAL}}"

# re-record the automatable scenarios, then re-render all four README GIFs
[group('Utility')]
gifs: cast-merged-pr cast-uncloned-repo
	#!/usr/bin/env bash
	set -euo pipefail
	# the recipe dependencies re-record fresh demo casts first (needs
	# network + gh auth); speeds follow README.md, and the template's
	# docs/dev-process.gif is never touched.
	just gif merged-pr2
	just gif uncloned-repo
	just gif active-pr
	just gif descriptions2
	echo "{{GREEN}}All four README GIFs re-rendered{{NORMAL}}"

# run unit tests
[group('Testing')]
test:
	go test ./...

# run tests with coverage and print a per-function summary
[group('Testing')]
cover:
	go test ./... -covermode=atomic -coverprofile=coverage.out
	go tool cover -func=coverage.out

# run Go native fuzz targets for short bursts (opt-in)
[group('Testing')]
fuzz time="30s":
    #!/usr/bin/env bash
    set -euo pipefail
    # CI runs the seed corpora as ordinary unit tests via 'just test',
    # so this opt-in recipe is for deeper local exploration. Each target
    # runs for the given -fuzztime.
    for target in ParsePRURL ParseActionsRunURL ParsePRViewWithRepo \
                  ParseRepoArg ParseRunIDFromURL BigIntUnmarshalJSON ParseTimestamp; do
        echo "{{BLUE}}Fuzzing Fuzz${target} for {{ time }}...{{NORMAL}}"
        go test -run='^$' -fuzz="Fuzz${target}" -fuzztime="{{ time }}" ./internal/github/
    done

# update Go dependencies and tidy go.mod/go.sum
[group('Build')]
deps-update:
	go get -u ./...
	go mod tidy

# build the gh-observer binary and install locally
[group('Build')]
build:
	go build -o gh-observer
	gh ext remove gh-observer
	gh ext install .

# check the latest release: workflow runs, signature audit, cosign verify
[group('Process')]
[positional-arguments]
release_status target="":
	#!/usr/bin/env bash
	set -euo pipefail

	# the [positional-arguments] attribute keeps this recipe
	# self-contained in derived repos, whose root justfile lacks
	# `set positional-arguments` (#367)

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

	# Audit release assets: every binary must ship a Sigstore bundle -
	# .sigstore.json since v4.1.1, .bundle on older releases. The v4.1-era
	# upload loops silently skipped missing files and stayed green, so
	# this checks the pairing instead of just listing names with checkmarks.
	echo "{{BLUE}}Release assets for $release_tag:{{NORMAL}}"
	assets=$(gh release view "$release_tag" --json assets --jq '.assets[].name')

	if [[ -z "$assets" ]]; then
		echo "{{RED}}Error: no assets on $release_tag{{NORMAL}}"
		exit 1
	fi

	# binaries = assets matching the release workflow's own binary globs
	# (darwin-* linux-* freebsd-* windows-*), minus the signature
	# artifacts. Mirroring release.yml keeps auxiliary assets (checksums,
	# SBOMs) from being reported as unsigned binaries, and a rename that
	# misses the workflow glob fails loudly here the same way CI does.
	binaries=$(grep -E '^(darwin|freebsd|linux|windows)-' <<<"$assets" \
		| grep -Ev '\.(sigstore\.json|bundle|sig|pem)$' || true)

	if [[ -z "$binaries" ]]; then
		echo "{{RED}}Error: no <os>-<arch> binary assets found on $release_tag{{NORMAL}}"
		exit 1
	fi

	# signature asset for a binary: .sigstore.json preferred, legacy
	# .bundle otherwise; empty when the binary is unsigned. Ends in
	# return 0 so a binary with no bundle doesn't leak grep's non-zero
	# status into the sig=$(sig_of ...) assignment (#326 convention).
	sig_of() {
		if grep -qx -F -- "$1.sigstore.json" <<<"$assets"; then
			printf '%s' "$1.sigstore.json"
		elif grep -qx -F -- "$1.bundle" <<<"$assets"; then
			printf '%s' "$1.bundle"
		fi
		return 0
	}

	missing=""
	while IFS= read -r binary; do
		[[ -z "$binary" ]] && continue
		sig=$(sig_of "$binary")
		if [[ -n "$sig" ]]; then
			echo "  ✓ $binary {{GREEN}}(signed: $sig){{NORMAL}}"
		else
			echo "  ✗ $binary {{RED}}(missing signature bundle){{NORMAL}}"
			missing+="$binary"$'\n'
		fi
	done <<< "$binaries"

	while IFS= read -r asset; do
		[[ -z "$asset" ]] && continue
		case "$asset" in
			*.sigstore.json) echo "  ✓ $asset {{GREEN}}(sigstore bundle){{NORMAL}}" ;;
			*.bundle) echo "  ✓ $asset {{GREEN}}(legacy bundle){{NORMAL}}" ;;
			*.sig) echo "  ? $asset {{YELLOW}}(detached signature){{NORMAL}}" ;;
			*.pem) echo "  ? $asset {{YELLOW}}(certificate){{NORMAL}}" ;;
			*.intoto.jsonl) echo "  ✓ $asset {{GREEN}}(SLSA provenance){{NORMAL}}" ;;
			*)
				grep -qx -F -- "$asset" <<<"$binaries" \
					|| echo "  - $asset {{BLUE}}(auxiliary asset - not signature-audited){{NORMAL}}"
				;;
		esac
	done <<< "$assets"

	if [[ -n "$missing" ]]; then
		echo ""
		echo "{{RED}}Error: unsigned binaries on $release_tag - see #521:{{NORMAL}}"
		printf '%s' "$missing" | sed 's/^/  ✗ /'
		exit 1
	fi

	# Verify signatures with the README "Option 2" verification - bundle +
	# identity + OIDC issuer, the same check users are told to run -
	# against assets downloaded from the release. This is the check that
	# would have caught #521's README documenting verification against
	# files that don't exist. A bad signature fails loudly; missing cosign
	# skips with a distinct status line so callers can tell "verified"
	# from "skipped" without parsing warnings.
	if ! command -v cosign >/dev/null 2>&1; then
		echo ""
		echo "{{YELLOW}}Audit passed; signature verification SKIPPED - cosign not on PATH{{NORMAL}}"
		echo "                            install with: brew install cosign"
		exit 0
	fi

	# Derive the cosign identity from the repo remote so derived repos
	# and forks verify against their own workflow identity, not ours -
	# a hardcoded fini-net/gh-observer would fail every signed asset
	# outside this repo. GitHub names allow only [A-Za-z0-9._-], so
	# escaping the dots makes the name safe to embed in the regexp.
	# Legacy .bundle assets share this identity: the workflow path has
	# been .github/workflows/release.yml since signing landed (#222).
	repo_name=$(gh repo view --json nameWithOwner -q .nameWithOwner)
	identity_regexp="^https://github\\.com/${repo_name//./\\.}/\\.github/workflows/release\\.yml@refs/tags/"

	target="${1:-}"

	# pick the binaries to verify: the named asset, the host platform
	# pair by default, or every binary for 'all'
	case "$target" in
		all)
			verify_binaries="$binaries"
			;;
		"")
			case "$(uname -s)" in
				Darwin) host_os="darwin" ;;
				Linux) host_os="linux" ;;
				FreeBSD) host_os="freebsd" ;;
				*) host_os="" ;;
			esac
			case "$(uname -m)" in
				arm64 | aarch64) host_arch="arm64" ;;
				x86_64 | amd64) host_arch="amd64" ;;
				*) host_arch="" ;;
			esac
			if [[ -n "$host_os" && -n "$host_arch" ]] \
				&& grep -qx -F -- "${host_os}-${host_arch}" <<<"$binaries"; then
				verify_binaries="${host_os}-${host_arch}"
			else
				verify_binaries=$(head -n 1 <<<"$binaries")
				echo "{{YELLOW}}Host platform binary not on $release_tag - verifying $verify_binaries{{NORMAL}}"
			fi
			;;
		*)
			if ! grep -qx -F -- "$target" <<<"$binaries"; then
				echo "{{RED}}Error: '$target' is not a binary asset on $release_tag:{{NORMAL}}"
				printf '%s' "$binaries" | sed 's/^/  /'
				exit 1
			fi
			verify_binaries="$target"
			;;
	esac

	echo ""
	echo "{{BLUE}}Verifying signatures with cosign (README Option 2 command)...{{NORMAL}}"

	tmpdir=$(mktemp -d "${TMPDIR:-/tmp}/release-status.XXXXXX")
	trap 'rm -rf "$tmpdir"' EXIT

	rc=0
	while IFS= read -r binary; do
		[[ -z "$binary" ]] && continue
		sig=$(sig_of "$binary")
		if [[ -z "$sig" ]]; then
			echo "  ✗ $binary {{RED}}(no signature bundle found){{NORMAL}}"
			rc=1
			continue
		fi
		# guarded download (#326 convention): a gh failure lands in the
		# error branch below instead of aborting the whole recipe under
		# set -e before the message can print
		dl_rc=0
		gh release download "$release_tag" --dir "$tmpdir" \
			--pattern "$binary" --pattern "$sig" --clobber \
			|| dl_rc=$?
		if [[ "$dl_rc" -ne 0 ]]; then
			echo "  ✗ $binary {{RED}}(download failed: gh exit $dl_rc){{NORMAL}}"
			rc=1
			continue
		fi
		if [[ ! -f "$tmpdir/$binary" ]]; then
			echo "  ✗ $binary {{RED}}(download failed){{NORMAL}}"
			rc=1
			continue
		fi
		if [[ ! -f "$tmpdir/$sig" ]]; then
			echo "  ✗ $binary {{RED}}(bundle download failed: $sig){{NORMAL}}"
			rc=1
			continue
		fi
		verify_rc=0
		cosign verify-blob "$tmpdir/$binary" --bundle "$tmpdir/$sig" \
			--certificate-identity-regexp "$identity_regexp" \
			--certificate-oidc-issuer https://token.actions.githubusercontent.com \
			|| verify_rc=$?
		if [[ "$verify_rc" -eq 0 ]]; then
			echo "  ✓ $binary {{GREEN}}(signature verified){{NORMAL}}"
		else
			echo "  ✗ $binary {{RED}}(signature verification FAILED){{NORMAL}}"
			rc=1
		fi
		# free each pair after its verdict so 'all' mode does not
		# accumulate ~10MB of binaries on disk before the trap cleans up
		rm -f "$tmpdir/$binary" "$tmpdir/$sig"
	done <<< "$verify_binaries"

	echo ""
	if [[ "$rc" -eq 0 ]]; then
		echo "{{GREEN}}All checked signatures on $release_tag verified{{NORMAL}}"
	else
		echo "{{RED}}Signature verification failed on $release_tag{{NORMAL}}"
	fi
	exit "$rc"

# show openssf scorecard details
[group('Testing/Security')]
openssf_scorecard:
	curl https://api.scorecard.dev/projects/github.com/fini-net/gh-observer | jq .
