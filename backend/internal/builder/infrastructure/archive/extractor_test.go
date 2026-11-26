package archive_test

import (
	"archive/tar"
	"archive/zip"
	"bytes"
	"compress/gzip"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/builder/infrastructure/archive"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/stretchr/testify/require"
)

func TestExtractorExtractsZipTarAndTarGz(t *testing.T) {
	for _, tt := range []struct {
		name  string
		write func(string, map[string]string) error
		file  string
	}{
		{name: "zip", write: writeZipArchive, file: "source.zip"},
		{name: "tar", write: writeTarArchive, file: "source.tar"},
		{name: "tar.gz", write: writeTarGzArchive, file: "source.tar.gz"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			dir := t.TempDir()
			archivePath := filepath.Join(dir, tt.file)
			destDir := filepath.Join(dir, "out")
			err := tt.write(archivePath, map[string]string{
				"Dockerfile":              "FROM scratch\n",
				"services/api/main.go":    "package main\n",
				"services/api/nested.txt": "hello\n",
			})
			require.NoError(t, err)

			err = archive.NewExtractor().Extract(archivePath, destDir, 1024)
			require.NoError(t, err)
			require.FileExists(t, filepath.Join(destDir, "Dockerfile"))
			require.FileExists(t, filepath.Join(destDir, "services/api/main.go"))
			require.FileExists(t, filepath.Join(destDir, "services/api/nested.txt"))
		})
	}
}

// `tar -czf ctx.tar.gz .` writes the root directory itself as the first entry.
func TestExtractorAcceptsRootDirectoryEntry(t *testing.T) {
	for _, kind := range fuzzArchiveKinds {
		t.Run(kind.file, func(t *testing.T) {
			data, err := kind.build([]string{"./", "./Dockerfile", "./services/", "./services/main.go"})
			require.NoError(t, err)
			requireRootEntryExtracts(t, kind.file, data)
		})
	}

	t.Run("tar directory named dot", func(t *testing.T) {
		var buf bytes.Buffer
		w := tar.NewWriter(&buf)
		require.NoError(t, w.WriteHeader(&tar.Header{Name: ".", Mode: 0o755, Typeflag: tar.TypeDir}))
		require.NoError(t, writeTarEntries(w, map[string]string{"Dockerfile": "x", "services/main.go": "x"}))
		require.NoError(t, w.Close())
		requireRootEntryExtracts(t, "source.tar", buf.Bytes())
	})
}

func requireRootEntryExtracts(t *testing.T, archiveName string, data []byte) {
	t.Helper()
	dir := t.TempDir()
	archivePath := filepath.Join(dir, archiveName)
	require.NoError(t, os.WriteFile(archivePath, data, 0o600))
	destDir := filepath.Join(dir, "out")

	require.NoError(t, archive.NewExtractor().Extract(archivePath, destDir, 1024))
	require.FileExists(t, filepath.Join(destDir, "Dockerfile"))
	require.FileExists(t, filepath.Join(destDir, "services", "main.go"))
}

// A file entry that resolves to the root, or any entry that resolves
// outside it, fails without naming the entry.
func TestExtractorRejectsRootFileAndEscapingEntriesWithoutEchoingTheName(t *testing.T) {
	for _, kind := range fuzzArchiveKinds {
		for _, name := range []string{".", "secret-name/..", "../secret-name"} {
			t.Run(kind.file+"/"+name, func(t *testing.T) {
				data, err := kind.build([]string{name})
				require.NoError(t, err)
				err = requireExtractionContained(t, kind.file, data)
				require.Error(t, err)
				require.NotContains(t, err.Error(), "secret-name")
			})
		}
	}
}

func TestExtractorRejectsEscapingDirectoryEntry(t *testing.T) {
	for _, kind := range fuzzArchiveKinds {
		t.Run(kind.file, func(t *testing.T) {
			data, err := kind.build([]string{"./", "../"})
			require.NoError(t, err)
			require.Error(t, requireExtractionContained(t, kind.file, data))
		})
	}
}

func TestExtractorReplacesRepeatedTarEntry(t *testing.T) {
	dir := t.TempDir()
	var buf bytes.Buffer
	w := tar.NewWriter(&buf)
	for _, body := range []string{"FROM busybox\nRUN echo stale\n", "FROM scratch\n"} {
		require.NoError(t, w.WriteHeader(&tar.Header{Name: "Dockerfile", Mode: 0644, Size: int64(len(body))}))
		_, err := w.Write([]byte(body))
		require.NoError(t, err)
	}
	require.NoError(t, w.Close())
	archivePath := filepath.Join(dir, "repeated.tar")
	require.NoError(t, os.WriteFile(archivePath, buf.Bytes(), 0644))

	destDir := filepath.Join(dir, "out")
	require.NoError(t, archive.NewExtractor().Extract(archivePath, destDir, 1024))

	got, err := os.ReadFile(filepath.Join(destDir, "Dockerfile"))
	require.NoError(t, err)
	require.Equal(t, "FROM scratch\n", string(got))
}

func TestExtractorRejectsUnsupportedTraversalOversizeAndCorruptArchives(t *testing.T) {
	dir := t.TempDir()
	extractor := archive.NewExtractor()

	err := extractor.Extract(filepath.Join(dir, "source.rar"), filepath.Join(dir, "out"), 1024)
	require.ErrorIs(t, err, apperrors.ErrInvalidFileFormat)

	traversalZip := filepath.Join(dir, "traversal.zip")
	require.NoError(t, writeZipArchive(traversalZip, map[string]string{"../evil.txt": "owned"}))
	err = extractor.Extract(traversalZip, filepath.Join(dir, "zip-out"), 1024)
	require.Error(t, err)
	require.NoFileExists(t, filepath.Join(dir, "evil.txt"))

	oversizeTar := filepath.Join(dir, "oversize.tar")
	require.NoError(t, writeTarArchive(oversizeTar, map[string]string{"big.txt": "12345"}))
	err = extractor.Extract(oversizeTar, filepath.Join(dir, "oversize-out"), 4)
	require.ErrorIs(t, err, apperrors.ErrResourceExhausted)

	corruptZip := filepath.Join(dir, "corrupt.zip")
	require.NoError(t, os.WriteFile(corruptZip, []byte("not a zip"), 0644))
	err = extractor.Extract(corruptZip, filepath.Join(dir, "corrupt-out"), 1024)
	require.Error(t, err)
	require.False(t, errors.Is(err, apperrors.ErrInvalidFileFormat))
}

// FuzzExtractEntryNames builds well-formed archives from fuzzed entry names,
// so the fuzzer explores path handling rather than archive framing.
func FuzzExtractEntryNames(f *testing.F) {
	for _, seed := range []struct {
		format uint8
		names  string
	}{
		{0, "Dockerfile\nsrc/main.go"}, {1, "../escape"}, {2, "a/../../escape"}, {0, "/abs/path"},
		{1, "dir/\ndir/file"}, {2, "./x\n."}, {0, "a/b/../../../c"}, {1, ".."}, {2, "a\\..\\..\\b"},
		{0, "./\n./Dockerfile"}, {1, "./\n./Dockerfile"}, {2, "./\n../escape"},
	} {
		f.Add(seed.format, seed.names)
	}
	f.Fuzz(func(t *testing.T, format uint8, names string) {
		kind := fuzzArchiveKinds[int(format)%len(fuzzArchiveKinds)]
		entries := strings.Split(names, "\n")
		data, err := kind.build(entries)
		if err != nil {
			t.Skip("the archive writer rejects these names")
		}
		extractErr := requireExtractionContained(t, kind.file, data)
		// The walk only sees escapes that stay inside the test's temporary
		// tree; the names show an escape of any depth.
		if slices.ContainsFunc(entries, escapesRoot) {
			require.Error(t, extractErr, "an archive with an escaping entry must be rejected")
		}
	})
}

// FuzzExtractRawArchive feeds arbitrary bytes to the archive readers.
func FuzzExtractRawArchive(f *testing.F) {
	for i, kind := range fuzzArchiveKinds {
		for _, names := range [][]string{{"Dockerfile"}, {"../escape"}, {"dir/", "dir/file"}, {"./", "./Dockerfile"}} {
			data, err := kind.build(names)
			require.NoError(f, err)
			f.Add(uint8(i), data)
		}
	}
	f.Fuzz(func(t *testing.T, format uint8, data []byte) {
		_ = requireExtractionContained(t, fuzzArchiveKinds[int(format)%len(fuzzArchiveKinds)].file, data)
	})
}

var fuzzArchiveKinds = []struct {
	file  string
	build func(names []string) ([]byte, error)
}{
	{"source.zip", buildZip},
	{"source.tar", func(names []string) ([]byte, error) { return buildTar(names, false) }},
	{"source.tar.gz", func(names []string) ([]byte, error) { return buildTar(names, true) }},
}

func buildZip(names []string) ([]byte, error) {
	var buf bytes.Buffer
	w := zip.NewWriter(&buf)
	for _, name := range names {
		entry, err := w.Create(name)
		if err != nil {
			return nil, err
		}
		if !strings.HasSuffix(name, "/") {
			if _, err := entry.Write([]byte("x")); err != nil {
				return nil, err
			}
		}
	}
	err := w.Close()
	return buf.Bytes(), err
}

func buildTar(names []string, gzipped bool) ([]byte, error) {
	var buf bytes.Buffer
	var gz *gzip.Writer
	w := tar.NewWriter(&buf)
	if gzipped {
		gz = gzip.NewWriter(&buf)
		w = tar.NewWriter(gz)
	}
	for _, name := range names {
		header := &tar.Header{Name: name, Mode: 0o644, Typeflag: tar.TypeReg, Size: 1}
		if strings.HasSuffix(name, "/") {
			header = &tar.Header{Name: name, Mode: 0o755, Typeflag: tar.TypeDir}
		}
		if err := w.WriteHeader(header); err != nil {
			return nil, err
		}
		if header.Size > 0 {
			if _, err := w.Write([]byte("x")); err != nil {
				return nil, err
			}
		}
	}
	err := w.Close()
	if gz != nil {
		err = errors.Join(err, gz.Close())
	}
	return buf.Bytes(), err
}

// escapesRoot reports whether an entry name resolves outside the extraction root.
func escapesRoot(name string) bool {
	rel := filepath.Clean(filepath.Join("root", name))
	return rel != "root" && !strings.HasPrefix(rel, "root"+string(filepath.Separator))
}

// requireExtractionContained extracts below a deeply nested root and fails if
// anything appears beside it or a symlink appears inside it, whether or not
// extraction succeeded. It returns the extraction error.
func requireExtractionContained(t *testing.T, archiveName string, data []byte) error {
	t.Helper()
	base := t.TempDir()
	archivePath := filepath.Join(base, archiveName)
	require.NoError(t, os.WriteFile(archivePath, data, 0o600))
	root := filepath.Join(append([]string{base}, slices.Repeat([]string{"outer"}, 8)...)...)
	root = filepath.Join(root, "root")
	require.NoError(t, os.MkdirAll(root, 0o755))

	extractErr := archive.NewExtractor().Extract(archivePath, root, 1<<20)

	require.NoError(t, filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if path == archivePath || (path != root && strings.HasPrefix(root, path+string(filepath.Separator))) {
			return nil // the archive itself and the directories above the root
		}
		rel, err := filepath.Rel(root, path)
		require.NoError(t, err)
		require.False(t, rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)), "%s escaped the extraction root", path)
		require.Zero(t, entry.Type()&fs.ModeSymlink, "%s is a symlink", path)
		return nil
	}))
	return extractErr
}

func writeZipArchive(path string, files map[string]string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	w := zip.NewWriter(f)
	for name, body := range files {
		entry, err := w.Create(name)
		if err != nil {
			_ = w.Close()
			return err
		}
		if _, err := entry.Write([]byte(body)); err != nil {
			_ = w.Close()
			return err
		}
	}
	return w.Close()
}

func writeTarArchive(path string, files map[string]string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	w := tar.NewWriter(f)
	if err := writeTarEntries(w, files); err != nil {
		_ = w.Close()
		return err
	}
	return w.Close()
}

func writeTarGzArchive(path string, files map[string]string) (err error) {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, f.Close()) }()
	gw := gzip.NewWriter(f)
	defer func() { err = errors.Join(err, gw.Close()) }()
	tw := tar.NewWriter(gw)
	if err := writeTarEntries(tw, files); err != nil {
		_ = tw.Close()
		return err
	}
	return tw.Close()
}

func writeTarEntries(w *tar.Writer, files map[string]string) error {
	for name, body := range files {
		data := []byte(body)
		if err := w.WriteHeader(&tar.Header{Name: name, Mode: 0644, Size: int64(len(data))}); err != nil {
			return err
		}
		if _, err := w.Write(data); err != nil {
			return err
		}
	}
	return nil
}
