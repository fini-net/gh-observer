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
# repo mode is persistent (never auto-quits), so we wrap gh-observer in
# GNU `timeout`, which sends SIGTERM after 60s; bubbletea treats SIGTERM as
# a clean QuitMsg and exits, letting asciinema finalize the cast.
# Requires GNU coreutils `timeout` (brew install coreutils on macOS).
[group('Testing')]
test2cast-repo repo="fini-net/gh-observer": build
	#!/usr/bin/env bash
	set -euo pipefail
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

# run unit tests
[group('Testing')]
test:
	go test ./...

# run Go native fuzz targets for short bursts (opt-in; CI runs the seed
# corpora as ordinary unit tests via 'just test', so this is for deeper
# local exploration). Each target runs for the given -fuzztime.
[group('Testing')]
fuzz time="30s":
    #!/usr/bin/env bash
    set -euo pipefail
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

# [positional-arguments] keeps this recipe self-contained in derived
# repos, whose root justfile lacks `set positional-arguments` (#367)
# check the latest release: workflow runs, signature audit, cosign verify
[group('Process')]
[positional-arguments]
release_status target="":
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
