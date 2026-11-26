package gitsource_test

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"maps"
	"net/url"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/gitsource"
	"github.com/stretchr/testify/require"
)

func TestValidateRepoURL(t *testing.T) {
	t.Parallel()

	allowed := []string{"github.com", "gitlab.com"}
	info, err := gitsource.ValidateRepoURL("https://github.com/acme/app.git", allowed)
	require.NoError(t, err)
	require.Equal(t, "github.com", info.Host)
	require.Equal(t, "acme/app.git", info.Path)

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
			_, err := gitsource.ValidateRepoURL(raw, allowed)
			require.Error(t, err)
		})
	}
}

// FuzzValidateRepoURL checks that an accepted URL is an allowed https host
// with a clean path and no credentials, query or fragment, and that its
// normalized form validates to the same repository.
func FuzzValidateRepoURL(f *testing.F) {
	allowed := []string{"github.com", "gitlab.com"}
	for _, seed := range []string{
		"https://github.com/acme/app.git", "https://GitHub.com:443/acme/app", "https://github.com:22/acme/app",
		"https://user:pass@github.com/acme/app", "http://github.com/acme/app", "https://evil.example/acme/app",
		"https://github.com/acme/../app", "https://github.com/acme/%2e%2e/app", "https://github.com/acme//app",
		"https://github.com/acme/app?x=1", "https://github.com/acme/app#frag", "https://github.com/a%2Fb",
		"https://github.com/acme/a%20b", "https://github.com", " https://github.com/acme/app ", "",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		info, err := gitsource.ValidateRepoURL(raw, allowed)
		if err != nil {
			require.ErrorIs(t, err, apperrors.ErrBadRequest)
			return
		}
		require.Contains(t, allowed, info.Host)
		parsed, err := url.Parse(info.URL)
		require.NoError(t, err)
		require.Equal(t, "https", parsed.Scheme)
		require.Equal(t, info.Host, parsed.Hostname())
		require.Nil(t, parsed.User)
		require.Empty(t, parsed.RawQuery)
		require.Empty(t, parsed.Fragment)
		require.False(t, slices.ContainsFunc(strings.Split(info.Path, "/"), func(segment string) bool {
			return segment == "" || segment == "." || segment == ".."
		}), "path %q must not contain empty, current or parent segments", info.Path)

		again, err := gitsource.ValidateRepoURL(info.URL, allowed)
		require.NoError(t, err)
		require.Equal(t, info, again)
	})
}

func TestValidateRefAndRelativePath(t *testing.T) {
	t.Parallel()

	require.NoError(t, gitsource.ValidateRef("feature/add-git"))
	for _, ref := range []string{"-main", "feature..bad", "bad@{ref", "release.lock", "bad ref"} {
		require.Error(t, gitsource.ValidateRef(ref))
	}

	require.NoError(t, gitsource.ValidateRelativePath("services/api/Dockerfile"))
	for _, path := range []string{"/etc/passwd", "../app", "app/../../secret", `app\Dockerfile`} {
		require.Error(t, gitsource.ValidateRelativePath(path))
	}
}

func TestArchiveToZipExcludesGitAndAppliesLimits(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	mustWriteFile(t, filepath.Join(dir, ".git", "config"), []byte("secret"))
	mustWriteFile(t, filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"))
	mustWriteFile(t, filepath.Join(dir, "app", "main.go"), []byte("package main\n"))

	var buf bytes.Buffer
	stats, err := gitsource.ArchiveToZip(context.Background(), dir, &buf, gitsource.ArchiveLimits{
		MaxRepositoryBytes: 1024 * 1024,
		MaxArchiveBytes:    1024 * 1024,
	})
	require.NoError(t, err)
	require.NotZero(t, stats.RepositoryBytes)
	require.NotZero(t, stats.ArchiveBytes)

	zr, err := zip.NewReader(bytes.NewReader(buf.Bytes()), int64(buf.Len()))
	require.NoError(t, err)
	seen := map[string]bool{}
	for _, f := range zr.File {
		seen[f.Name] = true
		require.NotEqual(t, ".git/config", f.Name)
	}
	require.True(t, seen["Dockerfile"])
	require.True(t, seen["app/main.go"])

	var limited bytes.Buffer
	_, err = gitsource.ArchiveToZip(context.Background(), dir, &limited, gitsource.ArchiveLimits{
		MaxRepositoryBytes: 1,
		MaxArchiveBytes:    1024 * 1024,
	})
	require.True(t, errors.Is(err, apperrors.ErrBadRequest))
}

func mustWriteFile(t *testing.T, path string, data []byte) {
	t.Helper()
	require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
	require.NoError(t, os.WriteFile(path, data, 0644))
}

func TestValidationErrorsNeverEchoTheirInput(t *testing.T) {
	const marker = "echomarker"
	validateURL := func(raw string) func() error {
		return func() error {
			_, err := gitsource.ValidateRepoURL(raw, []string{"github.com"})
			return err
		}
	}
	for _, tc := range []struct {
		name     string
		validate func() error
	}{
		{name: "non-https URL", validate: validateURL("http://github.com/" + marker + "/repo")},
		{name: "URL with credentials", validate: validateURL("https://user:" + marker + "@github.com/acme/repo")},
		{name: "URL with query", validate: validateURL("https://github.com/acme/repo?" + marker)},
		{name: "URL with disallowed host", validate: validateURL("https://" + marker + ".example/acme/repo")},
		{name: "URL with invalid path", validate: validateURL("https://github.com/acme/../" + marker)},
		{name: "ref", validate: func() error { return gitsource.ValidateRef(marker + "..main") }},
		{name: "absolute path", validate: func() error { return gitsource.ValidateRelativePath("/" + marker) }},
		{name: "parent traversal", validate: func() error { return gitsource.ValidateRelativePath("../" + marker) }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.validate()
			require.ErrorIs(t, err, apperrors.ErrBadRequest)
			require.NotContains(t, err.Error(), marker)
			require.NotContains(t, apperrors.SafeMessage(err), marker)
		})
	}
}

// TestCloneGivesGitOnlyTheEnvironmentItNeeds runs Clone against a stand-in git
// executable that records its environment. Core clones on a request path, so
// its bootstrap secrets must not reach a process that talks to an untrusted
// remote.
func TestCloneGivesGitOnlyTheEnvironmentItNeeds(t *testing.T) {
	binDir := t.TempDir()
	envFile := filepath.Join(t.TempDir(), "git.env")
	// The last argument is the clone destination; env is a shell-independent
	// way to list the environment the child received.
	script := "#!/bin/sh\nenv > '" + envFile + "'\nfor last; do :; done\nmkdir -p \"$last\"\n"
	require.NoError(t, os.WriteFile(filepath.Join(binDir, "git"), []byte(script), 0o700))

	t.Setenv("PATH", binDir+string(os.PathListSeparator)+os.Getenv("PATH"))
	t.Setenv("HOME", t.TempDir())
	t.Setenv("HTTPS_PROXY", "http://proxy.internal:3128")
	t.Setenv("https_proxy", "http://proxy.internal:3128")
	t.Setenv("NO_PROXY", "localhost")
	t.Setenv("SSL_CERT_FILE", "/etc/ssl/certs/ca-certificates.crt")
	t.Setenv("DATABASE_URL", "postgres://user:database-secret@db/core")
	t.Setenv("CORE_SERVICE_TOKEN", "internal-secret")
	t.Setenv("OBJECT_STORAGE_SECRET_KEY", "storage-secret")
	t.Setenv("GIT_SSH_COMMAND", "ssh -o ProxyCommand=evil")
	t.Setenv("GIT_ASKPASS", "/usr/bin/leak-credentials")

	_, err := gitsource.Clone(context.Background(), gitsource.CloneRequest{
		RepoURL:      "https://github.com/owner/repo.git",
		DestDir:      filepath.Join(t.TempDir(), "repo"),
		AllowedHosts: []string{"github.com"},
	})
	require.NoError(t, err)

	raw, err := os.ReadFile(envFile)
	require.NoError(t, err)
	got := map[string]string{}
	for line := range strings.SplitSeq(strings.TrimSpace(string(raw)), "\n") {
		name, value, _ := strings.Cut(line, "=")
		got[name] = value
	}

	names := slices.Sorted(maps.Keys(got))
	for _, name := range []string{"DATABASE_URL", "CORE_SERVICE_TOKEN", "OBJECT_STORAGE_SECRET_KEY", "GIT_SSH_COMMAND"} {
		require.NotContains(t, names, name)
	}
	require.False(t, strings.Contains(string(raw), "secret"), "a secret value reached git: variables %v", names)
	require.Equal(t, os.Getenv("PATH"), got["PATH"])
	require.Equal(t, os.Getenv("HOME"), got["HOME"])
	require.Equal(t, "http://proxy.internal:3128", got["HTTPS_PROXY"])
	require.Equal(t, "http://proxy.internal:3128", got["https_proxy"])
	require.Equal(t, "localhost", got["NO_PROXY"])
	require.Equal(t, "/etc/ssl/certs/ca-certificates.crt", got["SSL_CERT_FILE"])
	require.Equal(t, "0", got["GIT_TERMINAL_PROMPT"])
	require.Empty(t, got["GIT_ASKPASS"])
	require.Empty(t, got["SSH_ASKPASS"])
}
