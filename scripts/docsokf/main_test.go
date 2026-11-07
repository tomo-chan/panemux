package docsokf

import (
	"bytes"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"testing/fstest"
)

var bundleCases = []struct {
	name  string
	files map[string]string
	want  string
}{{"concept without index",
	map[string]string{"example.md": "---\ntype: Specification\ntitle: Example\ndescription: An example c" +
		"ontract.\n---\n# Example\n"},
	""},
	{"nested concepts",
		map[string]string{"topic/example.md": "---\ntype: Specification\ntitle: Example\ndescription: An example c" +
			"ontract.\n---\n# Example\n", "topic/index.md": "# Contracts\n\n* [Example](example.md) - An example contract.\n"},
		""},
	{"root version",
		map[string]string{indexName: "---\nokf_version: \"0.2\"\n---\n# Contracts\n\n* [Example](example.md) " +
			"- An example contract.\n"},
		""},
	{"root without version",
		map[string]string{indexName: "# Contracts\n\n* [Example](example.md) - An example contract.\n"},
		""},
	{"unknown version best effort",
		map[string]string{indexName: "---\nokf_version: \"0.3\"\n---\n# Contracts\n\n* [Example](example.md) " +
			"- An example contract.\n"},
		""},
	{"unknown type and fields",
		map[string]string{"example.md": "---\ntype: Future Type\ncustom: {nested: [one, two]}\n---\n"},
		""},
	{"optional fields absent", map[string]string{"example.md": "---\ntype: Guide\n---\n"}, ""},
	{"CRLF frontmatter",
		map[string]string{"example.md": "---\r\ntype: Specification\r\ntitle: Example\r\ndescription: An exampl" +
			"e contract.\r\n---\r\n# Example\r\n"},
		""},
	{"quoted and multiline YAML",
		map[string]string{"example.md": "---\ntype: 'Security Requirement'\ndescription: >-\n  Two lines\n  o" +
			"f summary.\ntags: [one, two]\n---\n"},
		""},
	{"translation",
		map[string]string{"example.ja.md": "---\ntype: Specification\ntitle: 日本語訳\ndescription: イベントの契約の日本語訳。\n-" +
			"--\n# 契約\n"},
		""},
	{"README is a concept", map[string]string{"README.md": "# Read me\n"}, "README.md: missing frontmatter"},
	{"missing frontmatter", map[string]string{"example.md": "# Example\n"}, "missing frontmatter"},
	{"unclosed frontmatter", map[string]string{"example.md": "---\ntype: Guide\n"}, "unclosed frontmatter"},
	{"invalid YAML", map[string]string{"example.md": "---\ntype: [broken\n---\n"}, "YAML"},
	{"empty YAML", map[string]string{"example.md": "---\n---\n"}, "YAML"},
	{"null root index metadata",
		map[string]string{indexName: "---\nnull\n---\n# Contracts\n* [Example](example.md) - Summary.\n"},
		"YAML"},
	{"null nested index metadata",
		map[string]string{"topic/index.md": "---\nnull\n---\n# Contracts\n* [Example](example.md) - Summary.\n"},
		"YAML"},
	{"duplicate YAML key", map[string]string{"example.md": "---\ntype: Guide\ntype: Reference\n---\n"}, "YAML"},
	{"YAML sequence", map[string]string{"example.md": "---\n- type: Guide\n---\n"}, "YAML"},
	{"explicit YAML end marker", map[string]string{"example.md": "---\ntype: Guide\n...\n---\n"}, ""},
	{"trailing invalid YAML", map[string]string{"example.md": "---\ntype: Guide\n...\ninvalid\n---\n"}, "YAML"},
	{"missing type", map[string]string{"example.md": "---\ntitle: Example\n---\n"}, "non-empty string type"},
	{"empty type", map[string]string{"example.md": "---\ntype: \"  \"\n---\n"}, "non-empty string type"},
	{"null type", map[string]string{"example.md": "---\ntype:\n---\n"}, "non-empty string type"},
	{"numeric type", map[string]string{"example.md": "---\ntype: 7\n---\n"}, "non-empty string type"},
	{"list type", map[string]string{"example.md": "---\ntype: [Guide]\n---\n"}, "non-empty string type"},
	{"nested index metadata",
		map[string]string{"topic/index.md": "---\nokf_version: \"0.2\"\n---\n# Contracts\n\n* [Example](example.md) " +
			"- An example contract.\n"},
		"only bundle-root index"},
	{"root concept metadata",
		map[string]string{indexName: "---\ntype: Guide\n---\n# Contracts\n\n* [Example](example.md) - An ex" +
			"ample contract.\n"},
		"only okf_version"},
	{"index without heading", map[string]string{indexName: "* [Example](example.md) - Summary.\n"}, "heading"},
	{"index without entries", map[string]string{indexName: "# Contracts\n\nIntroductory prose.\n"}, "entry"},
	{"index without headings or links", map[string]string{indexName: "Only prose.\n"}, "heading"},
	{"entry before heading",
		map[string]string{indexName: "* [Example](example.md) - Summary.\n# Contracts\n"},
		"before a heading"},
	{"entry without description",
		map[string]string{indexName: "# Contracts\n* [Example](example.md)\n"},
		"description"},
	{"empty description", map[string]string{indexName: "# Contracts\n* [Example](example.md) - \n"}, "description"},
	{"directory entries", map[string]string{indexName: "# Topics\n- [Topic](topic/) - Topic contracts.\n"}, ""},
	{"prose and writing rules",
		map[string]string{indexName: "# Contracts\n\n* [Example](example.md) - An example contract.\n\n## " +
			"Writing rules\n- Keep contracts focused.\n\n| Topic | Guide |\n|---|" +
			"---|\n| Example | [Example](example.md) |\n"},
		""},
	{"fenced entries ignored",
		map[string]string{indexName: "# Contracts\n```markdown\n* [Example](example.md) - Summary.\n```\n"},
		"entry"},
	{"fenced headings ignored",
		map[string]string{indexName: "~~~md\n# Example\n~~~\n* [Example](example.md) - Summary.\n"},
		"before a heading"},
	{"code examples do not break index",
		map[string]string{indexName: "# Contracts\n\n* [Example](example.md) - An example contract.\n\n```" +
			"md\n* [No description](missing.md)\n```\n"},
		""},
	{"different fence markers stay in code",
		map[string]string{indexName: "# Contracts\n\n* [Example](example.md) - An example contract.\n\n```" +
			"`md\n~~~\n```\n* [No description](missing.md)\n````\n"},
		""},
	{"non-markdown resources",
		map[string]string{"images/sample.png": "png", "models/model.als": "model"},
		"no markdown"},
	{"empty bundle", map[string]string{}, "no markdown"},
	{"version plus extra key",
		map[string]string{indexName: "---\nokf_version: \"0.2\"\ntitle: extra\n---\n# Entries\n* [One](one.md" +
			") - Summary.\n"},
		"only okf_version"},
	{"reserved log without metadata", map[string]string{"log.md": "# History\n\n## 2026-11-08\n* Updated docs.\n"}, ""},
	{"fence character must match",
		map[string]string{indexName: "# Entries\n```md\n~~~\n* [Hidden](hidden.md) - Hidden.\n```\n"},
		"entry"},
	{"info string cannot close fence",
		map[string]string{indexName: "# Entries\n```md\n```md\n* [Hidden](hidden.md) - Hidden.\n```\n"},
		"entry"},
}

func TestBundle(t *testing.T) {
	for _, tc := range bundleCases {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			for name, content := range tc.files {
				path := filepath.Join(root, name)
				if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, []byte(content), 0600); err != nil {
					t.Fatal(err)
				}
			}
			var warnings bytes.Buffer
			_, err := checkBundle(root, &warnings)
			if tc.want == "" {
				if err != nil {
					t.Fatal(err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("want %q, got %v", tc.want, err)
			}
			if warnings.Len() != 0 {
				t.Fatalf("unexpected warnings: %s", &warnings)
			}
		})
	}
}

type failingFS struct {
	fs.FS
	fail string
}

func (f failingFS) Open(name string) (fs.File, error) {
	if name == f.fail {
		return nil, errors.New("fixture read failure")
	}
	return f.FS.Open(name)
}

func TestTreeReadErrors(t *testing.T) {
	for _, name := range []string{".", "one.md", "sub"} {
		t.Run(name, func(t *testing.T) {
			base := fstest.MapFS{"one.md": {Data: []byte("---\ntype: Guide\n---\n")},
				"sub/two.md": {Data: []byte("---\ntype: Guide\n---\n")}}
			_, err := checkTree(failingFS{base, name}, &bytes.Buffer{})
			if err == nil || !strings.Contains(err.Error(), "fixture read failure") {
				t.Fatalf("expected read failure, got %v", err)
			}
		})
	}
}

func TestSymlinkDirectoryIsReported(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "invalid.md"), []byte("# No metadata"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "valid.md"), []byte("---\ntype: Guide\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(outside, filepath.Join(root, "linked")); err != nil {
		t.Fatal(err)
	}
	var warnings bytes.Buffer
	count, err := checkBundle(root, &warnings)
	if err != nil || count != 1 {
		t.Fatalf("count %d, err %v", count, err)
	}
	if !strings.Contains(warnings.String(), "linked") || !strings.Contains(warnings.String(), "not inspected") {
		t.Fatalf("missing skipped-directory diagnostic: %s", &warnings)
	}
}

func TestSymlinkFiles(t *testing.T) {
	for _, broken := range []bool{false, true} {
		t.Run(map[bool]string{false: "file", true: "broken"}[broken], func(t *testing.T) {
			root := t.TempDir()
			target := filepath.Join(t.TempDir(), "one.md")
			if !broken {
				if err := os.WriteFile(target, []byte("---\ntype: Guide\n---\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			if err := os.Symlink(target, filepath.Join(root, "link.md")); err != nil {
				t.Fatal(err)
			}
			var warnings bytes.Buffer
			count, err := checkBundle(root, &warnings)
			if broken {
				if err == nil {
					t.Fatal("unreadable target must not pass")
				}
			} else if err != nil || count != 1 {
				t.Fatalf("count %d, error %v", count, err)
			}
			if warnings.Len() != 0 {
				t.Fatalf("file symlink must not be reported as a directory: %s", &warnings)
			}
		})
	}
}

func TestCommand(t *testing.T) {
	root := t.TempDir()
	valid := filepath.Join(root, "docs")
	t.Chdir(root)
	if err := os.Mkdir(valid, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(valid, "one.md"), []byte("---\ntype: Guide\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(root, "file.md")
	if err := os.WriteFile(file, []byte("---\ntype: Guide\n---\n"), 0600); err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name   string
		needle string
		args   []string
		status int
	}{
		{name: "success", args: []string{valid}, status: 0, needle: "1 markdown files checked"},
		{name: "default bundle", args: nil, status: 0, needle: "1 markdown files checked"},
		{name: "missing", args: []string{filepath.Join(root, "absent")}, status: 1, needle: "no such file"},
		{name: "file", args: []string{file}, status: 1, needle: "bundle must be a directory"},
		{name: "usage", args: []string{"one", "two"}, status: 1, needle: "usage:"},
		{name: "empty directory", args: []string{""}, status: 1, needle: "empty bundle directory"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var out, stderr bytes.Buffer
			got := Run(tc.args, &out, &stderr)
			if got != tc.status || !strings.Contains(out.String()+stderr.String(), tc.needle) {
				t.Fatalf("status %d, out %s, stderr %s", got, &out, &stderr)
			}
		})
	}
}
