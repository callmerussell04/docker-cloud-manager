package gitsource

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

func TestValidateRepoURL(t *testing.T) {
	t.Parallel()

	allowed := []string{"github.com", "gitlab.com"}
	info, err := ValidateRepoURL("https://github.com/acme/app.git", allowed)
	if err != nil {
		t.Fatalf("ValidateRepoURL() error = %v", err)
	}
	if info.Host != "github.com" || info.Path != "acme/app.git" {
		t.Fatalf("repo info = %+v, want github.com acme/app.git", info)
	}

	tests := []string{
		"http://github.com/acme/app.git",
		"https://token@github.com/acme/app.git",
		"https://github.com:8443/acme/app.git",
		"https://example.com/acme/app.git",
		"https://github.com/acme/../app.git",
		"https://github.com/acme/app.git?ref=main",
	}
	for _, raw := range tests {
		raw := raw
		t.Run(raw, func(t *testing.T) {
			t.Parallel()
			if _, err := ValidateRepoURL(raw, allowed); err == nil {
				t.Fatalf("ValidateRepoURL(%q) error = nil, want error", raw)
			}
		})
	}
}

func TestValidateRefAndRelativePath(t *testing.T) {
	t.Parallel()

	if err := ValidateRef("feature/add-git"); err != nil {
		t.Fatalf("ValidateRef() error = %v", err)
	}
	for _, ref := range []string{"-main", "feature..bad", "bad@{ref", "release.lock", "bad ref"} {
		if err := ValidateRef(ref); err == nil {
			t.Fatalf("ValidateRef(%q) error = nil, want error", ref)
		}
	}

	if err := ValidateRelativePath("services/api/Dockerfile"); err != nil {
		t.Fatalf("ValidateRelativePath() error = %v", err)
	}
	for _, path := range []string{"/etc/passwd", "../app", "app/../../secret", `app\Dockerfile`} {
		if err := ValidateRelativePath(path); err == nil {
			t.Fatalf("ValidateRelativePath(%q) error = nil, want error", path)
		}
	}
}

func TestArchiveToZipExcludesGitAndAppliesLimits(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".git", "config"), []byte("secret"))
	mustWriteFile(t, filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"))
	mustWriteFile(t, filepath.Join(dir, "app", "main.go"), []byte("package main\n"))

	var buf bytes.Buffer
	stats, err := ArchiveToZip(context.Background(), dir, &buf, ArchiveLimits{
		MaxRepositoryBytes: 1024 * 1024,
		MaxArchiveBytes:    1024 * 1024,
	})
	if err != nil {
		t.Fatalf("ArchiveToZip() error = %v", err)
	}
	if stats.RepositoryBytes == 0 || stats.ArchiveBytes == 0 {
		t.Fatalf("archive stats = %+v, want non-zero values", stats)
	}

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	if err != nil {
		t.Fatalf("zip.NewReader() error = %v", err)
	}
	seen := map[string]bool{}
	for _, f := range zr.File {
		seen[f.Name] = true
		if f.Name == ".git/config" {
			t.Fatalf(".git file was included in archive")
		}
	}
	if !seen["Dockerfile"] || !seen["app/main.go"] {
		t.Fatalf("archive entries = %v, want Dockerfile and app/main.go", seen)
	}

	var limited bytes.Buffer
	_, err = ArchiveToZip(context.Background(), dir, &limited, ArchiveLimits{
		MaxRepositoryBytes: 1,
		MaxArchiveBytes:    1024 * 1024,
	})
	if !errors.Is(err, apperrors.ErrBadRequest) {
		t.Fatalf("ArchiveToZip() error = %v, want ErrBadRequest for repository limit", err)
	}
}

func TestRepositorySizeIncludesGitDirectory(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".git", "objects", "pack"), bytes.Repeat([]byte("a"), 8))
	mustWriteFile(t, filepath.Join(dir, "app.txt"), []byte("ok"))

	size, err := repositorySize(context.Background(), dir, 1024)
	if err != nil {
		t.Fatalf("repositorySize() error = %v", err)
	}
	if size != 10 {
		t.Fatalf("repositorySize() = %d, want 10", size)
	}

	_, err = repositorySize(context.Background(), dir, 9)
	if !errors.Is(err, apperrors.ErrBadRequest) {
		t.Fatalf("repositorySize() error = %v, want ErrBadRequest", err)
	}
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.WriteFile(path, data, 0644); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}
}
