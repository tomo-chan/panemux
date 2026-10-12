package taskhook

import (
	"context"
	"path/filepath"
	"strings"
)

type heldFile struct {
	path       string
	id         string
	executable bool
}

func (h *NativeHost) Files(ctx context.Context, pid int, root, executableID string) (NativeFiles, error) {
	if _, err := h.Process(ctx, pid); err != nil {
		return NativeFiles{}, err
	}
	files, err := h.files(ctx, pid)
	if err != nil || len(files) > MaxHeldFiles {
		return NativeFiles{}, ErrUnsupported
	}
	var result NativeFiles
	seen := map[string]bool{}
	for _, f := range files {
		if f.executable && f.id == executableID {
			result.Native = true
		}
		if !strings.HasPrefix(f.path, root+"/") || !strings.HasPrefix(filepath.Base(f.path), "rollout-") ||
			!strings.HasSuffix(f.path, ".jsonl") || seen[f.id] {

			continue
		}
		candidate, readErr := heldMetadata(root, f.path, f.id, h.uid)
		if readErr != nil {
			return NativeFiles{}, ErrUnsupported
		}
		seen[f.id] = true
		result.Sessions = append(result.Sessions, candidate)
	}
	if ctx.Err() != nil {
		return NativeFiles{}, ErrUnsupported
	}
	return result, nil
}
