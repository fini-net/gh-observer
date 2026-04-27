# VEX (Vulnerability Exploitability eXchange) justfile

# generate OpenVEX document from govulncheck scan
[group('Testing/Security')]
vex_generate:
    chmod +x vex/generate-vex.sh
    ./vex/generate-vex.sh -o vex/openvex.json

# show current VEX document
[group('Testing/Security')]
vex_show:
    cat vex/openvex.json

# manually add a not_affected VEX statement for a specific vulnerability
# usage: just vex_add CVE-2025-12345 "vulnerable_code_not_in_execute_path" "govulncheck shows this is not called"
[group('Testing/Security')]
vex_add vuln justification="vulnerable_code_not_in_execute_path" impact="":
    #!/usr/bin/env bash
    set -euo pipefail
    if [[ ! -f vex/openvex.json ]]; then
        echo "{{RED}}No VEX document found. Run 'just vex_generate' first.{{NORMAL}}"
        exit 1
    fi
    VEX_ARGS=(
        --product="pkg:golang/github.com/fini-net/gh-observer"
        --vuln="{{ vuln }}"
        --status="not_affected"
        --justification="{{ justification }}"
        --status-note="Manually added by maintainer"
    )
    IMPACT="{{ impact }}"
    [[ -n "${IMPACT}" ]] && VEX_ARGS+=(--impact-statement="${IMPACT}")
    vexctl add "${VEX_ARGS[@]}" vex/openvex.json