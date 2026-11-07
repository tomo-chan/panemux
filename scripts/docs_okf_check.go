//go:build ignore

// The OKF documentation gate. Run through make check-docs-okf; the explicit
// file list keeps this standalone tool out of the product's package graph.
package main

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

func main() {
	root := "docs"
	if len(os.Args) > 2 {
		fmt.Fprintln(os.Stderr, "usage: docs_okf_check [bundle-directory]")
		os.Exit(1)
	}
	if len(os.Args) == 2 {
		root = os.Args[1]
	}
	count, err := checkBundle(root)
	if err != nil {
		fmt.Fprintln(os.Stderr, "docs-okf:", err)
		os.Exit(1)
	}
	fmt.Printf("docs-okf: ok — %d markdown files checked\n", count)
}

// Check OKF's required concept metadata and §8 index structure, not a closed
// type/schema vocabulary. Title/description are repository writing rules;
// optional metadata is not a conformance requirement (OKF §11).
func checkBundle(root string) (int, error) {
	info, err := os.Stat(root)
	if err != nil {
		return 0, err
	}
	if !info.IsDir() {
		return 0, errors.New("bundle must be a directory")
	}
	count := 0
	err = filepath.WalkDir(root, func(path string, entry fs.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || filepath.Ext(path) != ".md" {
			return nil
		}
		count++
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		if entry.Name() == "log.md" {
			return nil // Reserved history files are not concepts or indexes.
		}
		meta, body, err := frontmatter(string(data))
		if err == nil && entry.Name() == "index.md" {
			if meta != nil {
				if filepath.Dir(rel) != "." {
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
			return fmt.Errorf("%s: %w", filepath.ToSlash(rel), err)
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
