// Package docsokf checks documentation bundle metadata and index structure.
// Keep the package name aligned with its directory: gremlins v0.6.0 uses that
// match to select per-mutant package tests. The process entrypoint is in cmd.
package docsokf

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// Run checks the requested bundle and writes diagnostics, returning a CLI exit status.
func Run(args []string, stdout, stderr io.Writer) int {
	root := "docs"
	if len(args) > 1 {
		fmt.Fprintln(stderr, "usage: docsokf [bundle-directory]")
		return 1
	}
	if len(args) == 1 {
		root = args[0]
	}
	count, err := checkBundle(root, stderr)
	if err != nil {
		fmt.Fprintln(stderr, "docs-okf:", err)
		return 1
	}
	fmt.Fprintf(stdout, "docs-okf: ok — %d markdown files checked\n", count)
	return 0
}

// Check OKF's required concept metadata and §8 index structure, not a closed
// type/schema vocabulary. Title/description are repository writing rules;
// optional metadata is not a conformance requirement (OKF §11).
func checkBundle(root string, warnings io.Writer) (int, error) {
	if root == "" {
		return 0, errors.New("empty bundle directory")
	}
	root = filepath.Clean(root)
	info, err := os.Stat(root)
	if err != nil {
		return 0, fmt.Errorf("stat bundle: %w", err)
	}
	if !info.IsDir() {
		return 0, errors.New("bundle must be a directory")
	}
	return checkTree(os.DirFS(root), warnings)
}

const indexName = "index.md"

func checkTree(tree fs.FS, warnings io.Writer) (int, error) {
	count := 0
	err := fs.WalkDir(tree, ".", func(name string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.Type()&fs.ModeSymlink != 0 {
			target, err := fs.Stat(tree, name)
			if err != nil {
				return fmt.Errorf("stat %s: %w", name, err)
			}
			if target.IsDir() {
				fmt.Fprintf(warnings, "docs-okf: %s: symlink directory not inspected\n", name)
				return nil
			}
		}
		if entry.IsDir() || path.Ext(name) != ".md" {
			return nil
		}
		count++
		data, err := fs.ReadFile(tree, name)
		if err != nil {
			return fmt.Errorf("read %s: %w", name, err)
		}
		if entry.Name() == "log.md" {
			return nil
		} // Reserved history is outside this metadata/index checker.
		meta, body, err := frontmatter(string(data))
		if err == nil && entry.Name() == indexName {
			if meta != nil {
				if path.Dir(name) != "." {
					err = errors.New("only bundle-root index may have frontmatter")
				} else if len(meta) != 1 || meta["okf_version"] == nil {
					err = errors.New("index frontmatter permits only okf_version")
				}
			}
			if err == nil {
				err = checkIndex(body)
			}
		} else if err == nil {
			kind, ok := meta["type"].(string)
			if meta == nil {
				err = errors.New("missing frontmatter")
			} else if !ok || strings.TrimSpace(kind) == "" {
				err = errors.New("concept needs a non-empty string type")
			}
		}
		if err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
		return nil
	})
	if err == nil && count == 0 {
		err = errors.New("no markdown files — check did not run")
	}
	return count, err
}

func frontmatter(text string) (map[string]any, string, error) {
	text = strings.ReplaceAll(text, "\r\n", "\n")
	lines := strings.Split(text, "\n")
	if lines[0] != "---" {
		return nil, text, nil
	}
	for i := 1; i < len(lines); i++ {
		if lines[i] != "---" {
			continue
		}
		meta := map[string]any{}
		decoder := yaml.NewDecoder(strings.NewReader(strings.Join(lines[1:i], "\n")))
		if err := decoder.Decode(&meta); err != nil {
			return nil, "", fmt.Errorf("YAML frontmatter: %w", err)
		}
		if meta == nil {
			return nil, "", errors.New("YAML frontmatter must be a mapping")
		}
		var extra any
		if err := decoder.Decode(&extra); err != io.EOF {
			return nil, "", fmt.Errorf("YAML frontmatter must be one mapping (extra document or error: %v)", err)
		}
		return meta, strings.Join(lines[i+1:], "\n"), nil
	}
	return nil, "", errors.New("unclosed frontmatter")
}

var (
	heading          = regexp.MustCompile(`^#{1,6}\s+\S`)
	entryLink        = regexp.MustCompile(`^\s*[-*+]\s+\[[^]]+\]\([^)]+\)`)
	entryDescription = regexp.MustCompile(`^\s*[-*+]\s+\[[^]]+\]\([^)]+\)\s+-\s+\S`)
	fence            = regexp.MustCompile("^ {0,3}(`{3,}|~{3,})")
)

func checkIndex(body string) error {
	hasHeading, entries := false, 0
	marker := ""
	for i, line := range strings.Split(body, "\n") {
		if match := fence.FindStringSubmatch(line); match != nil {
			if marker == "" {
				marker = match[1]
			} else if match[1][0] == marker[0] && len(match[1]) >= len(marker) && strings.TrimSpace(line) == match[1] {
				marker = ""
			}
			continue
		}
		if marker != "" {
			continue
		}
		if heading.MatchString(line) {
			hasHeading = true
		}
		if !entryLink.MatchString(line) {
			continue
		}
		if !hasHeading {
			return fmt.Errorf("line %d: index entry before a heading", i+1)
		}
		if !entryDescription.MatchString(line) {
			return fmt.Errorf("line %d: index entry needs a description after the link", i+1)
		}
		entries++
	}
	if !hasHeading {
		return errors.New("index needs a section heading")
	}
	if entries == 0 {
		return errors.New("index needs a described list entry")
	}
	return nil
}
