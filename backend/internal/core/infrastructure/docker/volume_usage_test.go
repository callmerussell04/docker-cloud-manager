package docker_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/stretchr/testify/require"
)

func writeSizedFile(t *testing.T, root, rel string, size int64) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(rel))
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
	f, err := os.Create(path)
	require.NoError(t, err)
	require.NoError(t, f.Truncate(size))
	require.NoError(t, f.Close())
}

func TestScanDirectoryUsageWithoutEntryLimitMeasuresApparentFileSizes(t *testing.T) {
	root := t.TempDir()
	writeSizedFile(t, root, "a.txt", 10)
	writeSizedFile(t, root, "nested/deeper/b.bin", 3<<30) // sparse: apparent size is accounted
	writeSizedFile(t, root, "nested/c", 0)
	require.NoError(t, os.Mkdir(filepath.Join(root, "empty"), 0o755))

	scan, err := docker.ScanDirectoryUsage(context.Background(), root, model.VolumeUsageScan{}, 0)

	require.NoError(t, err)
	require.True(t, scan.Complete)
	require.EqualValues(t, 10+3<<30, scan.Bytes)
	require.EqualValues(t, 6, scan.Entries, "files and directories below the root")
}

// writeOrderingTrapTree builds names whose byte order differs from walk order
// ("b-c" and "b.d" sort before "b/x" as strings but are walked after b/).
// Each file has a distinct power-of-two size, so a missed or double-counted
// file changes the total.
func writeOrderingTrapTree(t *testing.T) (root string, wantBytes int64, wantEntries int64) {
	t.Helper()
	root = t.TempDir()
	files := []string{"a", "b/x", "b/y/z", "b/y/zz", "b-c", "b.d/e", "c/d/e/f", "c/g"}
	for i, rel := range files {
		writeSizedFile(t, root, rel, 1<<i)
	}
	require.NoError(t, os.MkdirAll(filepath.Join(root, "b", "empty"), 0o755))
	// Files: 8. Directories: b, b/y, b/empty, b.d, c, c/d, c/d/e.
	return root, 1<<len(files) - 1, 15
}

func scanToCompletion(t *testing.T, root string, maxEntries int) model.VolumeUsageScan {
	t.Helper()
	var scan model.VolumeUsageScan
	for slices := 1; ; slices++ {
		require.LessOrEqual(t, slices, 100, "scan must finish")
		prev := scan
		next, err := docker.ScanDirectoryUsage(context.Background(), root, scan, maxEntries)
		require.NoError(t, err)
		if next.Complete {
			require.LessOrEqual(t, next.Entries-prev.Entries, int64(maxEntries))
			return next
		}
		require.Equal(t, int64(maxEntries), next.Entries-prev.Entries, "an incomplete slice uses its whole entry budget")
		require.NotEqual(t, prev.Cursor, next.Cursor)
		scan = next
	}
}

func TestScanDirectoryUsageResumesFromCursorWithinEntryBudget(t *testing.T) {
	root, wantBytes, wantEntries := writeOrderingTrapTree(t)
	for _, budget := range []int{1, 2, 3, 4, 7, 14, 15} {
		t.Run(fmt.Sprintf("budget=%d", budget), func(t *testing.T) {
			scan := scanToCompletion(t, root, budget)
			require.Equal(t, wantBytes, scan.Bytes)
			require.Equal(t, wantEntries, scan.Entries)
		})
	}
}

func TestScanDirectoryUsagePartialSliceIsNotComplete(t *testing.T) {
	root, wantBytes, _ := writeOrderingTrapTree(t)

	scan, err := docker.ScanDirectoryUsage(context.Background(), root, model.VolumeUsageScan{}, 3)

	require.NoError(t, err)
	require.False(t, scan.Complete)
	require.EqualValues(t, 3, scan.Entries)
	require.Less(t, scan.Bytes, wantBytes)
	require.Equal(t, "b/empty", scan.Cursor, "walk order: a, b, b/empty")
}

func TestScanDirectoryUsageStopsOnCancellationAndKeepsProgress(t *testing.T) {
	root, wantBytes, wantEntries := writeOrderingTrapTree(t)
	partial, err := docker.ScanDirectoryUsage(context.Background(), root, model.VolumeUsageScan{}, 4)
	require.NoError(t, err)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	stopped, err := docker.ScanDirectoryUsage(ctx, root, partial, 0)

	require.ErrorIs(t, err, context.Canceled)
	require.False(t, stopped.Complete)
	require.Equal(t, partial, stopped, "no entry is counted after cancellation and earlier progress is kept")

	resumed, err := docker.ScanDirectoryUsage(context.Background(), root, stopped, 0)
	require.NoError(t, err)
	require.True(t, resumed.Complete)
	require.Equal(t, wantBytes, resumed.Bytes)
	require.Equal(t, wantEntries, resumed.Entries)
}

func TestScanDirectoryUsageResumesAfterCursorEntryWasRemoved(t *testing.T) {
	root, _, _ := writeOrderingTrapTree(t)
	partial, err := docker.ScanDirectoryUsage(context.Background(), root, model.VolumeUsageScan{}, 3)
	require.NoError(t, err)
	require.Equal(t, "b/empty", partial.Cursor)
	require.NoError(t, os.RemoveAll(filepath.Join(root, "b")))

	resumed, err := docker.ScanDirectoryUsage(context.Background(), root, partial, 0)

	require.NoError(t, err)
	require.True(t, resumed.Complete)
	// After the removed cursor: b-c (1<<4), b.d, b.d/e (1<<5), c, c/d, c/d/e, c/d/e/f (1<<6), c/g (1<<7).
	require.Equal(t, partial.Bytes+1<<4+1<<5+1<<6+1<<7, resumed.Bytes)
	require.Equal(t, partial.Entries+8, resumed.Entries)
}

func TestScanDirectoryUsageAcceptsUncleanRoot(t *testing.T) {
	root, wantBytes, wantEntries := writeOrderingTrapTree(t)
	partial, err := docker.ScanDirectoryUsage(context.Background(), root+"/", model.VolumeUsageScan{}, 3)
	require.NoError(t, err)
	require.Equal(t, "b/empty", partial.Cursor)

	scan, err := docker.ScanDirectoryUsage(context.Background(), root+"/./", partial, 0)

	require.NoError(t, err)
	require.True(t, scan.Complete)
	require.Equal(t, wantBytes, scan.Bytes)
	require.Equal(t, wantEntries, scan.Entries)
}
