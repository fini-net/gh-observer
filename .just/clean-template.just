# .just/clean-template.just

# clean debris out of template
[group('Utility')]
clean_template: _clean_readme _remove_template_files

# generate a clean README
[group('Utility')]
[no-cd]
_clean_readme: _on_a_branch
    #!/usr/bin/env bash
    set -euo pipefail # strict mode without tracing

    GIT_ORIGIN=$(git config --get remote.origin.url | sed -e 's/^.*://' -e 's/[.]git$//')
    #echo "$GIT_ORIGIN"

    GITHUB_ORG=$(echo "$GIT_ORIGIN" | sed -e 's/[/].*$//')
    echo "org={{BLUE}}$GITHUB_ORG{{NORMAL}}"

    GITHUB_REPO=$(echo "$GIT_ORIGIN" | sed -e 's/^.*[/]//')
    echo "repo={{BLUE}}$GITHUB_REPO{{NORMAL}}"

    cat > README.md << END_OF_HEREDOC
    # ${GITHUB_REPO}

    ![GitHub Issues](https://img.shields.io/github/issues/${GITHUB_ORG}/${GITHUB_REPO})
    ![GitHub Pull Requests](https://img.shields.io/github/issues-pr/${GITHUB_ORG}/${GITHUB_REPO})
    ![GitHub License](https://img.shields.io/github/license/${GITHUB_ORG}/${GITHUB_REPO})
    ![GitHub watchers](https://img.shields.io/github/watchers/${GITHUB_ORG}/${GITHUB_REPO})

    A good starting place for something. (TODO)

    ## Contributing

    - [Code of Conduct](.github/CODE_OF_CONDUCT.md)
    - [Contributing Guide](.github/CONTRIBUTING.md) includes a step-by-step guide to our
      [development process](.github/CONTRIBUTING.md#development-process).

    ## Support & Security

    - [Getting Support](.github/SUPPORT.md)
    - [Security Policy](.github/SECURITY.md)
    END_OF_HEREDOC

# remove template-specific files
[group('Utility')]
[no-cd]
_remove_template_files: _on_a_branch
    #!/usr/bin/env bash
    set -euo pipefail # strict mode without tracing

    echo "{{YELLOW}}Removing template-specific files...{{NORMAL}}"

    # Remove banner PNG if it exists
    if [[ -f "docs/template-repo-banner.png" ]]; then
        echo "Removing docs/template-repo-banner.png"
        rm -f "docs/template-repo-banner.png"
        # Remove docs directory if empty
        if [[ -d "docs" && -z "$(ls -A docs)" ]]; then
            echo "Removing empty docs directory"
            rmdir docs
        fi
    fi

    # Remove testing infrastructure
    if [[ -f ".just/testing.just" ]]; then
        echo "Removing .just/testing.just"
        rm -f ".just/testing.just"
    fi

    if [[ -d ".just/test" ]]; then
        echo "Removing .just/test directory"
        rm -rf ".just/test"
    fi

    if [[ -f ".github/workflows/pr-body-tests.yml" ]]; then
        echo "Removing .github/workflows/pr-body-tests.yml"
        rm -f ".github/workflows/pr-body-tests.yml"
    fi

    # Remove test scripts
    if [[ -f ".just/lib/pr_body_test.sh" ]]; then
        echo "Removing .just/lib/pr_body_test.sh"
        rm -f ".just/lib/pr_body_test.sh"
    fi

    if [[ -f ".just/lib/template_sync_test.sh" ]]; then
        echo "Removing .just/lib/template_sync_test.sh"
        rm -f ".just/lib/template_sync_test.sh"
    fi

    # Remove this cleanup script after use
    if [[ -f ".just/clean-template.just" ]]; then
        echo "Removing .just/clean-template.just"
        rm -f ".just/clean-template.just"
    fi

    if [[ -f "justfile" ]]; then
        if grep -q "import.*clean-template.just" justfile; then
            echo "Removing clean-template.just import from justfile"
            sed -i.tmp '/import.*clean-template.just/d' justfile
            rm -f justfile.tmp
        fi
    fi

    # Remove testing import from main justfile
    if [[ -f "justfile" ]]; then
        if grep -q "import.*testing.just" justfile; then
            echo "Removing testing.just import from justfile"
            sed -i.tmp '/import.*testing.just/d' justfile
            rm -f justfile.tmp
        fi
    fi

    echo "{{GREEN}}Template cleanup completed!{{NORMAL}}"
