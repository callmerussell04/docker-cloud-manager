package docker

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
)

var errScanEntryBudgetSpent = errors.New("volume usage scan entry budget spent")

// ScanDirectoryUsage continues scan below root in filepath.WalkDir order,
// counting at most maxEntries new entries (no limit when maxEntries <= 0).
// Entries at or before scan.Cursor were counted by earlier slices and are
// skipped. The returned scan is Complete only when the walk reached its end;
// on error it still carries every entry counted before the failure.
func ScanDirectoryUsage(ctx context.Context, root string, scan model.VolumeUsageScan, maxEntries int) (model.VolumeUsageScan, error) {
	root = filepath.Clean(root)
	prefix := root
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	// Entries up to resumeAfter were counted by earlier slices. Walk order is
	// total, so once one entry follows it every later entry is new.
	resumeAfter := scan.Cursor
	counted := 0
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, walkErr error) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if walkErr != nil {
			return walkErr
		}
		if path == root {
			return nil
		}
		rel := filepath.ToSlash(strings.TrimPrefix(path, prefix))
		if resumeAfter != "" {
			if walkOrderCompare(rel, resumeAfter) <= 0 {
				// Descend into the cursor and its ancestors; everything else at
				// or before the cursor, including whole earlier subtrees, is counted.
				if d.IsDir() && (rel == resumeAfter || strings.HasPrefix(resumeAfter, rel+"/")) {
					return nil
				}
				if d.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}
			resumeAfter = ""
		}
		if maxEntries > 0 && counted >= maxEntries {
			return errScanEntryBudgetSpent
		}
		if !d.IsDir() {
			info, err := d.Info()
			if err != nil {
				return err
			}
			scan.Bytes += info.Size()
		}
		scan.Cursor = rel
		scan.Entries++
		counted++
		return nil
	})
	if errors.Is(err, errScanEntryBudgetSpent) {
		return scan, nil
	}
	if err != nil {
		return scan, fmt.Errorf("scan directory usage: %w", err)
	}
	scan.Complete = true
	return scan, nil
}

// walkOrderCompare orders slash-separated relative paths the way
// filepath.WalkDir visits them: component by component, a directory before
// its descendants.
func walkOrderCompare(a, b string) int {
	for {
		aHead, aRest, aMore := strings.Cut(a, "/")
		bHead, bRest, bMore := strings.Cut(b, "/")
		if c := strings.Compare(aHead, bHead); c != 0 {
			return c
		}
		switch {
		case !aMore && !bMore:
			return 0
		case !aMore:
			return -1
		case !bMore:
			return 1
		}
		a, b = aRest, bRest
	}
}

// ScanVolumeUsage continues a usage scan of a Docker volume's mountpoint. See
// ScanDirectoryUsage for the slice and resume contract.
func (a *Adapter) ScanVolumeUsage(ctx context.Context, volumeName string, scan model.VolumeUsageScan, maxEntries int) (model.VolumeUsageScan, error) {
	vol, err := a.cli.VolumeInspect(ctx, volumeName)
	if err != nil {
		return scan, fmt.Errorf("inspect volume for usage scan: %w", err)
	}
	return ScanDirectoryUsage(ctx, a.volumeMountpointPath(vol.Mountpoint), scan, maxEntries)
}
