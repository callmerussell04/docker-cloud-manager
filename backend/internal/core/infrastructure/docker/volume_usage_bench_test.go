package docker_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/infrastructure/docker"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/testutil/testenv"
)

// Synthetic volume profiles for measuring usage scans. Set
// DCM_VOLUME_BENCH_DIR to a directory on the filesystem under test (Docker
// volumes normally live on the host's disk, not tmpfs); missing profiles are
// created there and reused by later runs.
var volumeBenchProfiles = []struct {
	name  string
	build func(b *testing.B, dir string)
}{
	// 8 sparse files of 4 GiB: cost follows entries, not bytes.
	{name: "large", build: func(b *testing.B, dir string) {
		for i := range 8 {
			benchFile(b, filepath.Join(dir, fmt.Sprintf("blob-%d.bin", i)), 4<<30)
		}
	}},
	// Binary tree of depth 12 with a file per directory, plus a 256-level chain.
	{name: "deep", build: func(b *testing.B, dir string) {
		var tree func(dir string, depth int)
		tree = func(dir string, depth int) {
			benchFile(b, filepath.Join(dir, "f.dat"), 4096)
			if depth > 0 {
				tree(filepath.Join(dir, "l"), depth-1)
				tree(filepath.Join(dir, "r"), depth-1)
			}
		}
		tree(filepath.Join(dir, "tree"), 11)
		chain := filepath.Join(dir, "chain")
		for i := range 256 {
			chain = filepath.Join(chain, fmt.Sprintf("d%03d", i))
		}
		benchFile(b, filepath.Join(chain, "leaf.dat"), 1024)
	}},
	// 200 directories of 1000 small files.
	{name: "many_small", build: func(b *testing.B, dir string) {
		for d := range 200 {
			for f := range 1000 {
				benchFile(b, filepath.Join(dir, fmt.Sprintf("dir-%03d", d), fmt.Sprintf("f-%04d", f)), 32)
			}
		}
	}},
	// 100000 files in one directory: every resumed slice re-lists it.
	{name: "flat", build: func(b *testing.B, dir string) {
		for f := range 100000 {
			benchFile(b, filepath.Join(dir, fmt.Sprintf("f-%06d", f)), 32)
		}
	}},
}

func benchFile(b *testing.B, path string, size int64) {
	b.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		b.Fatal(err)
	}
	f, err := os.Create(path)
	if err != nil {
		b.Fatal(err)
	}
	if err := f.Truncate(size); err != nil {
		b.Fatal(err)
	}
	if err := f.Close(); err != nil {
		b.Fatal(err)
	}
}

func volumeBenchProfile(b *testing.B, name string, build func(*testing.B, string)) string {
	b.Helper()
	root := testenv.Lookup(b, "DCM_VOLUME_BENCH_DIR")
	dir := filepath.Join(root, name)
	done := filepath.Join(root, name+".ready")
	if _, err := os.Stat(done); err != nil {
		if err := os.RemoveAll(dir); err != nil {
			b.Fatal(err)
		}
		build(b, dir)
		benchFile(b, done, 0)
	}
	return dir
}

// BenchmarkScanDirectoryUsage measures one complete measurement of each
// profile, unsliced and as resumed slices at the default entry budget.
func BenchmarkScanDirectoryUsage(b *testing.B) {
	const defaultEntryBudget = 50000
	for _, p := range volumeBenchProfiles {
		for _, budget := range []int{0, defaultEntryBudget} {
			b.Run(fmt.Sprintf("%s/budget=%d", p.name, budget), func(b *testing.B) {
				dir := volumeBenchProfile(b, p.name, p.build)
				var scan model.VolumeUsageScan
				var slices int
				var slowest time.Duration
				for b.Loop() {
					scan, slices = model.VolumeUsageScan{}, 0
					for !scan.Complete {
						start := time.Now()
						var err error
						scan, err = docker.ScanDirectoryUsage(context.Background(), dir, scan, budget)
						if err != nil {
							b.Fatal(err)
						}
						slowest = max(slowest, time.Since(start))
						slices++
					}
				}
				b.ReportMetric(float64(scan.Entries), "entries")
				b.ReportMetric(float64(slices), "slices")
				b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/float64(scan.Entries), "ns/entry")
				b.ReportMetric(float64(slowest.Microseconds())/1000, "max-slice-ms")
			})
		}
	}
}
