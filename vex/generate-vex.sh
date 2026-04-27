#!/usr/bin/env bash
# generate-vex.sh - Generate OpenVEX document from govulncheck output
#
# Runs govulncheck in JSON mode, extracts vulnerability findings, and produces
# an OpenVEX document. Vulnerabilities that govulncheck determines are not
# called by the project are marked as "not_affected" with appropriate
# justifications based on call-graph analysis.
#
# Usage:
#   ./vex/generate-vex.sh                     # Generate to stdout
#   ./vex/generate-vex.sh -o vex/openvex.json # Write to file
#   ./vex/generate-vex.sh --product=...       # Override product pURL
#
# Requires: go, python3, vexctl (go install github.com/openvex/vexctl@latest)

set -euo pipefail

PRODUCT_ID="pkg:golang/github.com/fini-net/gh-observer"
OUTPUT=""
AUTHOR="gh-observer VEX automation"
AUTHOR_ROLE="automated"

while [[ $# -gt 0 ]]; do
    case "$1" in
        -o|--output)
            OUTPUT="$2"
            shift 2
            ;;
        --product)
            PRODUCT_ID="$2"
            shift 2
            ;;
        --author)
            AUTHOR="$2"
            shift 2
            ;;
        *)
            echo "Unknown option: $1" >&2
            exit 1
            ;;
    esac
done

SCRATCH="$(mktemp -d)"
export VEX_SCRATCH="${SCRATCH}"
trap 'rm -rf "${SCRATCH}"' EXIT

echo "Running govulncheck in JSON mode..." >&2
go run golang.org/x/vuln/cmd/govulncheck@latest -json ./... > "${SCRATCH}/govulncheck.json" 2>&1

echo "Parsing govulncheck findings..." >&2

# Extract findings from govulncheck JSON output into a structured format
python3 << 'PYEOF' > "${SCRATCH}/findings.json"
import json, sys, os

scratch = os.environ.get("VEX_SCRATCH", "/tmp")
findings = []

for line in open(os.path.join(scratch, "govulncheck.json")):
    line = line.strip()
    if not line:
        continue
    try:
        obj = json.loads(line)
    except json.JSONDecodeError:
        continue
    if "finding" not in obj:
        continue
    f = obj["finding"]
    vuln_id = f.get("osv", "")
    trace = f.get("trace", [])
    called = False
    for t in trace:
        if t.get("module") == "github.com/fini-net/gh-observer" and t.get("function"):
            called = True
            break
    if called:
        status = "affected"
        justification = ""
        impact = ""
    else:
        status = "not_affected"
        justification = "vulnerable_code_not_in_execute_path"
        impact = (
            "govulncheck call-graph analysis determined this vulnerability "
            "is not reached by any code path in gh-observer"
        )
    findings.append({
        "vuln_id": vuln_id,
        "status": status,
        "justification": justification,
        "impact": impact,
    })

json.dump(findings, sys.stdout)
PYEOF

FINDING_COUNT=$(python3 -c "import json; print(len(json.load(open('${SCRATCH}/findings.json'))))" 2>/dev/null || echo "0")

if [[ "${FINDING_COUNT}" == "0" ]]; then
    echo "No vulnerabilities found by govulncheck." >&2
    TIMESTAMP=$(date -u +"%Y-%m-%dT%H:%M:%SZ")
    python3 -c "
import json, sys
doc = {
    '@context': 'https://openvex.dev/ns/v0.2.0',
    '@id': 'https://openvex.dev/docs/public/vex-gh-observer-scan-${TIMESTAMP}',
    'author': '${AUTHOR}',
    'role': '${AUTHOR_ROLE}',
    'version': 1,
    'statements': [],
    'timestamp': '${TIMESTAMP}',
    'metadata': {
        'scan_tool': 'govulncheck',
        'scan_result': 'no_vulnerabilities_found',
    },
}
json.dump(doc, sys.stdout, indent=2)
print()
" > "${SCRATCH}/openvex.json"

    if [[ -n "${OUTPUT}" ]]; then
        cp "${SCRATCH}/openvex.json" "${OUTPUT}"
        echo "VEX document (empty, no findings) written to ${OUTPUT}" >&2
    else
        cat "${SCRATCH}/openvex.json"
    fi
    exit 0
fi

echo "Found ${FINDING_COUNT} vulnerability finding(s), generating VEX statements..." >&2

FIRST=true
while IFS= read -r finding; do
    vuln_id=$(echo "${finding}" | python3 -c "import json,sys; print(json.load(sys.stdin)['vuln_id'])")
    status=$(echo "${finding}" | python3 -c "import json,sys; print(json.load(sys.stdin)['status'])")
    justification=$(echo "${finding}" | python3 -c "import json,sys; print(json.load(sys.stdin)['justification'])")
    impact=$(echo "${finding}" | python3 -c "import json,sys; print(json.load(sys.stdin)['impact'])")

    if [[ "${FIRST}" == "true" ]]; then
        VEX_ARGS=(
            --product="${PRODUCT_ID}"
            --vuln="${vuln_id}"
            --status="${status}"
            --author="${AUTHOR}"
            --author-role="${AUTHOR_ROLE}"
            --status-note="Auto-generated from govulncheck call-graph analysis"
        )
        [[ -n "${justification}" ]] && VEX_ARGS+=(--justification="${justification}")
        [[ -n "${impact}" ]] && VEX_ARGS+=(--impact-statement="${impact}")
        vexctl create "${VEX_ARGS[@]}" --file="${SCRATCH}/openvex.json"
        FIRST=false
    else
        VEX_ARGS=(
            --product="${PRODUCT_ID}"
            --vuln="${vuln_id}"
            --status="${status}"
            --status-note="Auto-generated from govulncheck call-graph analysis"
        )
        [[ -n "${justification}" ]] && VEX_ARGS+=(--justification="${justification}")
        [[ -n "${impact}" ]] && VEX_ARGS+=(--impact-statement="${impact}")
        vexctl add "${VEX_ARGS[@]}" "${SCRATCH}/openvex.json"
    fi
done < <(python3 -c "import json; [print(json.dumps(f)) for f in json.load(open('${SCRATCH}/findings.json'))]")

if [[ -n "${OUTPUT}" ]]; then
    cp "${SCRATCH}/openvex.json" "${OUTPUT}"
    echo "VEX document written to ${OUTPUT}" >&2
else
    cat "${SCRATCH}/openvex.json"
fi