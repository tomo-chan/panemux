#!/bin/sh
# Offline black-box tests for the standalone OKF checker. Build once using the
# repository's existing YAML dependency, then run every fixture through the CLI.
# Run with make test-docs-okf.
set -u

scripts_dir=$(CDPATH='' cd -- "$(dirname -- "$0")" && pwd)
work=$(mktemp -d "${TMPDIR:-/tmp}/panemux-docs-okf-test.XXXXXX") || exit 1
trap 'rm -rf "$work"' EXIT
(cd "$scripts_dir/.." && go build -o "$work/checker" scripts/docs_okf_check.go) || exit 1
checks=0
failures=0

expect() {
	name=$1
	want=$2
	needle=$3
	root=$4
	checks=$((checks + 1))
	output=$("$work/checker" "$root" 2>&1)
	got=$?
	if [ "$got" -ne "$want" ]; then
		echo "FAIL $name: wanted exit $want, got $got"
		printf '%s\n' "$output"
		failures=$((failures + 1))
	elif [ -n "$needle" ] && ! printf '%s\n' "$output" | grep -Fq -- "$needle"; then
		echo "FAIL $name: missing diagnostic $needle"
		printf '%s\n' "$output"
		failures=$((failures + 1))
	else
		echo "ok $name"
	fi
}

# case_check <name> <diagnostic-or-empty-for-success> [path content]...
# Contents use printf %b escapes to keep the metadata matrix readable.
case_check() {
	name=$1
	needle=$2
	shift 2
	root="$work/case$checks"
	mkdir -p "$root" || exit 1
	while [ "$#" -gt 0 ]; do
		mkdir -p "$root/$(dirname -- "$1")" || exit 1
		printf '%b' "$2" > "$root/$1" || exit 1
		shift 2
	done
	want=0
	[ -z "$needle" ] || want=1
	expect "$name" "$want" "$needle" "$root"
}

case_check 'concept without index' '' \
	example.md '---\ntype: Specification\ntitle: Example\ndescription: An example contract.\n---\n# Example\n'
case_check 'nested concepts' '' \
	topic/example.md '---\ntype: Specification\ntitle: Example\ndescription: An example contract.\n---\n# Example\n' \
	topic/index.md '# Contracts\n\n* [Example](example.md) - An example contract.\n'
case_check 'root version' '' \
	index.md '---\nokf_version: "0.2"\n---\n# Contracts\n\n* [Example](example.md) - An example contract.\n'
case_check 'root without version' '' \
	index.md '# Contracts\n\n* [Example](example.md) - An example contract.\n'
case_check 'unknown version best effort' '' \
	index.md '---\nokf_version: "0.3"\n---\n# Contracts\n\n* [Example](example.md) - An example contract.\n'
case_check 'unknown type and fields' '' \
	example.md '---\ntype: Future Type\ncustom: {nested: [one, two]}\n---\n'
case_check 'optional fields absent' '' \
	example.md '---\ntype: Guide\n---\n'
case_check 'CRLF frontmatter' '' \
	example.md '---\r\ntype: Specification\r\ntitle: Example\r\ndescription: An example contract.\r\n---\r\n# Example\r\n'
case_check 'quoted and multiline YAML' '' \
	example.md '---\ntype: '"'"'Security Requirement'"'"'\ndescription: >-\n  Two lines\n  of summary.\ntags: [one, two]\n---\n'
case_check translation '' \
	example.ja.md '---\ntype: Specification\ntitle: 日本語訳\ndescription: イベントの契約の日本語訳。\n---\n# 契約\n'
case_check 'README is a concept' 'README.md: missing frontmatter' \
	README.md '# Read me\n'
case_check 'missing frontmatter' 'missing frontmatter' \
	example.md '# Example\n'
case_check 'unclosed frontmatter' 'unclosed frontmatter' \
	example.md '---\ntype: Guide\n'
case_check 'invalid YAML' YAML \
	example.md '---\ntype: [broken\n---\n'
case_check 'empty YAML' YAML \
	example.md '---\n---\n'
case_check 'null root index metadata' YAML \
	index.md '---\nnull\n---\n# Contracts\n* [Example](example.md) - Summary.\n'
case_check 'null nested index metadata' YAML \
	topic/index.md '---\nnull\n---\n# Contracts\n* [Example](example.md) - Summary.\n'
case_check 'duplicate YAML key' YAML \
	example.md '---\ntype: Guide\ntype: Reference\n---\n'
case_check 'YAML sequence' YAML \
	example.md '---\n- type: Guide\n---\n'
case_check 'explicit YAML end marker' '' \
	example.md '---\ntype: Guide\n...\n---\n'
case_check 'trailing invalid YAML' YAML \
	example.md '---\ntype: Guide\n...\ninvalid\n---\n'
case_check 'missing type' 'non-empty string type' \
	example.md '---\ntitle: Example\n---\n'
case_check 'empty type' 'non-empty string type' \
	example.md '---\ntype: "  "\n---\n'
case_check 'null type' 'non-empty string type' \
	example.md '---\ntype:\n---\n'
case_check 'numeric type' 'non-empty string type' \
	example.md '---\ntype: 7\n---\n'
case_check 'list type' 'non-empty string type' \
	example.md '---\ntype: [Guide]\n---\n'
case_check 'nested index metadata' 'only bundle-root index' \
	topic/index.md '---\nokf_version: "0.2"\n---\n# Contracts\n\n* [Example](example.md) - An example contract.\n'
case_check 'root concept metadata' 'only okf_version' \
	index.md '---\ntype: Guide\n---\n# Contracts\n\n* [Example](example.md) - An example contract.\n'
case_check 'index without heading' heading \
	index.md '* [Example](example.md) - Summary.\n'
case_check 'index without entries' entry \
	index.md '# Contracts\n\nIntroductory prose.\n'
case_check 'entry before heading' 'before a heading' \
	index.md '* [Example](example.md) - Summary.\n# Contracts\n'
case_check 'entry without description' description \
	index.md '# Contracts\n* [Example](example.md)\n'
case_check 'empty description' description \
	index.md '# Contracts\n* [Example](example.md) - \n'
case_check 'directory entries' '' \
	index.md '# Topics\n- [Topic](topic/) - Topic contracts.\n'
case_check 'prose and writing rules' '' \
	index.md '# Contracts\n\n* [Example](example.md) - An example contract.\n\n## Writing rules\n- Keep contracts focused.\n\n| Topic | Guide |\n|---|---|\n| Example | [Example](example.md) |\n'
case_check 'fenced entries ignored' entry \
	index.md '# Contracts\n```markdown\n* [Example](example.md) - Summary.\n```\n'
case_check 'fenced headings ignored' 'before a heading' \
	index.md '~~~md\n# Example\n~~~\n* [Example](example.md) - Summary.\n'
case_check 'code examples do not break index' '' \
	index.md '# Contracts\n\n* [Example](example.md) - An example contract.\n\n```md\n* [No description](missing.md)\n```\n'
case_check 'different fence markers stay in code' '' \
	index.md '# Contracts\n\n* [Example](example.md) - An example contract.\n\n````md\n~~~\n```\n* [No description](missing.md)\n````\n'
case_check 'non-markdown resources' 'no markdown' \
	images/sample.png png \
	models/model.als model
case_check 'empty bundle' 'no markdown'

expect 'unavailable bundle' 1 'no such file' "$work/absent"
printf '%b' '---\ntype: Guide\n---\n' > "$work/single.md" || exit 1
expect 'bundle must be a directory' 1 'bundle must be a directory' "$work/single.md"
output=$("$work/checker" one two 2>&1)
status=$?
checks=$((checks + 1))
if [ "$status" -ne 1 ] || ! printf '%s\n' "$output" | grep -Fq 'usage:'; then
	echo 'FAIL extra arguments must print usage and fail'
	failures=$((failures + 1))
else
	echo 'ok extra arguments fail with usage'
fi
if [ "$failures" -ne 0 ]; then
	echo "docs-okf checker tests: $failures of $checks failed"
	exit 1
fi
echo "docs-okf checker tests: $checks passed"
