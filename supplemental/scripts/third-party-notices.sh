#!/bin/sh
# Writes THIRD_PARTY_NOTICES: the license (and NOTICE) texts of every Go module compiled into
# vigil and vigil-agent, of every npm package bundled into the web UI, and of its font. Their
# MIT/BSD/Apache licenses require these notices to ship with the binaries, so the release
# generates the file and puts it in the archives, the .deb and the hub image.
#
# Usage: supplemental/scripts/third-party-notices.sh [output]   (default: THIRD_PARTY_NOTICES)
# Needs Go, network access for go-licenses, and `pnpm install` done in internal/site.
# Fails if a Go dependency has a forbidden, restricted (e.g. GPL) or unknown license.
set -eu

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
OUT=${1:-THIRD_PARTY_NOTICES}
case "$OUT" in /*) ;; *) OUT="$PWD/$OUT" ;; esac
GO_LICENSES="github.com/google/go-licenses/v2@v2.0.1"
OWN_MODULE="github.com/Gu1llaum-3/vigil"
# Build targets of .goreleaser.yml: platform-specific dependencies differ per OS.
GOOS_LIST="linux darwin windows"

TMP=$(mktemp -d)
trap 'rm -rf "$TMP"' EXIT
cd "$ROOT"

# Build the tool for this machine once: GOOS below only selects the dependency set.
GOBIN="$TMP" GOOS= GOARCH= go install "$GO_LICENSES"
LICENSES_BIN="$TMP/go-licenses"
# go-licenses tells standard-library packages apart through GOROOT, which `go run` would
# export but a plain binary does not see (notably with a GOTOOLCHAIN-downloaded toolchain).
GOROOT=$(go env GOROOT)
export GOROOT

# '|'-separated: tabs are IFS whitespace, so an empty column would shift the others.
printf '{{range .}}{{.Name}}|{{.Version}}|{{.LicenseName}}|{{.LicensePath}}\n{{end}}' >"$TMP/tsv.tpl"
for goos in $GOOS_LIST; do
	GOOS=$goos "$LICENSES_BIN" check --ignore "$OWN_MODULE" \
		--disallowed_types=forbidden,restricted,unknown ./internal/cmd/hub ./internal/cmd/agent 2>"$TMP/check.err" || {
		cat "$TMP/check.err" >&2
		exit 1
	}
	GOOS=$goos "$LICENSES_BIN" report --ignore "$OWN_MODULE" --template "$TMP/tsv.tpl" \
		./internal/cmd/hub ./internal/cmd/agent >>"$TMP/go.tsv" 2>"$TMP/report.err" || {
		cat "$TMP/report.err" >&2
		exit 1
	}
done
sort -u "$TMP/go.tsv" >"$TMP/go-sorted.tsv"

{
	printf 'Third-party notices for Vigil\n\n'
	printf 'Vigil (see LICENSE) includes the following third-party software.\n'
	printf 'Go modules are compiled into the vigil and vigil-agent binaries; npm packages are\n'
	printf 'bundled into the web UI embedded in the vigil hub.\n'

	printf '\n\n######## Go modules ########\n'
	printf '\n================================================================================\n'
	printf 'Go standard library %s (BSD-3-Clause)\n' "$(go env GOVERSION)"
	printf -- '--------------------------------------------------------------------------------\n'
	cat "$GOROOT/LICENSE"
	while IFS='|' read -r name version license path; do
		[ -n "$name" ] || continue
		if [ -z "$path" ]; then
			echo "No license file found for Go module $name" >&2
			exit 1
		fi
		printf '\n================================================================================\n'
		printf '%s %s (%s)\n' "$name" "$version" "$license"
		printf -- '--------------------------------------------------------------------------------\n'
		cat "$path"
		for notice in "$(dirname "$path")"/NOTICE*; do
			[ -f "$notice" ] || continue
			printf '\n--- %s ---\n' "$(basename "$notice")"
			cat "$notice"
		done
	done <"$TMP/go-sorted.tsv"

	printf '\n\n######## npm packages (web UI) ########\n'
	(cd internal/site && pnpm licenses list --prod --json) >"$TMP/npm-prod.json"
	(cd internal/site && pnpm licenses list --dev --json) >"$TMP/npm-dev.json"
	# devDependencies whose code still ends up in the bundle (CSS pulled in by src/index.css).
	BUNDLED_DEV_PACKAGES="tailwindcss tw-animate-css" node -e '
const fs = require("fs")
const path = require("path")
const read = (f) => Object.values(JSON.parse(fs.readFileSync(f, "utf8"))).flat()
const bundledDev = new Set(process.env.BUNDLED_DEV_PACKAGES.split(" "))
const pkgs = [...read(process.argv[1]), ...read(process.argv[2]).filter((p) => bundledDev.has(p.name))]
	.sort((a, b) => a.name.localeCompare(b.name))
const missing = [...bundledDev].filter((n) => !pkgs.some((p) => p.name === n))
if (missing.length > 0) {
	console.error("Bundled devDependencies not found: " + missing.join(", "))
	process.exit(1)
}
for (const pkg of pkgs) {
	const dir = pkg.paths[0]
	const files = fs.readdirSync(dir).filter((f) => /^(licen[cs]e|copying|notice)/i.test(f)).sort()
	process.stdout.write("\n" + "=".repeat(80) + "\n")
	process.stdout.write(`${pkg.name} ${pkg.versions.join(", ")} (${pkg.license})\n`)
	process.stdout.write("-".repeat(80) + "\n")
	if (files.length === 0) {
		process.stdout.write(`License: ${pkg.license} (no license file in the package; see ${pkg.homepage || "its repository"})\n`)
	}
	for (const f of files) {
		process.stdout.write(fs.readFileSync(path.join(dir, f), "utf8").trimEnd() + "\n")
	}
}
' "$TMP/npm-prod.json" "$TMP/npm-dev.json"

	printf '\n\n######## Fonts (web UI) ########\n'
	printf '\n================================================================================\n'
	printf 'Inter (internal/site/public/static/InterVariable.woff2) (OFL-1.1)\n'
	printf -- '--------------------------------------------------------------------------------\n'
	cat supplemental/licenses/Inter-OFL.txt
} >"$OUT"

echo "Wrote $OUT ($(grep -c '^================' "$OUT") packages)" >&2
