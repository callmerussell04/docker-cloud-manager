package gitsource

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"io"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
	"unicode"

	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

type RepoInfo struct {
	URL  string
	Host string
	Path string
	Ref  string
}

type CloneRequest struct {
	RepoURL            string
	Ref                string
	DestDir            string
	AllowedHosts       []string
	Timeout            time.Duration
	MaxRepositoryBytes int64
}

type ArchiveLimits struct {
	MaxRepositoryBytes int64
	MaxArchiveBytes    int64
}

type ArchiveStats struct {
	RepositoryBytes int64
	ArchiveBytes    int64
}

func ValidateRepoURL(raw string, allowedHosts []string) (RepoInfo, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return RepoInfo{}, apperrors.New(apperrors.ErrBadRequest, "git repository url is required")
	}

	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" {
		return RepoInfo{}, apperrors.New(apperrors.ErrBadRequest, "git repository url must use https")
	}
	if u.User != nil {
		return RepoInfo{}, apperrors.New(apperrors.ErrBadRequest, "git repository url must not contain credentials")
	}
	if u.RawQuery != "" || u.Fragment != "" {
		return RepoInfo{}, apperrors.New(apperrors.ErrBadRequest, "git repository url must not contain query or fragment")
	}

	host := strings.ToLower(u.Hostname())
	if host == "" {
		return RepoInfo{}, apperrors.New(apperrors.ErrBadRequest, "git repository host is required")
	}
	if port := u.Port(); port != "" && port != "443" {
		return RepoInfo{}, apperrors.New(apperrors.ErrBadRequest, "git repository url port is not allowed")
	}
	if !hostAllowed(host, allowedHosts) {
		return RepoInfo{}, apperrors.New(apperrors.ErrBadRequest, "git repository host is not allowed")
	}

	repoPath, err := url.PathUnescape(u.EscapedPath())
	if err != nil {
		return RepoInfo{}, apperrors.New(apperrors.ErrBadRequest, "git repository path is invalid")
	}
	repoPath = strings.TrimPrefix(repoPath, "/")
	if err := validateRepoPath(repoPath); err != nil {
		return RepoInfo{}, err
	}

	normalizedHost := host
	if port := u.Port(); port == "443" {
		normalizedHost += ":443"
	}
	normalizedURL := "https://" + normalizedHost + u.EscapedPath()
	return RepoInfo{
		URL:  normalizedURL,
		Host: host,
		Path: repoPath,
	}, nil
}

func ValidateRef(ref string) error {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return nil
	}
	if len(ref) > 256 {
		return apperrors.New(apperrors.ErrBadRequest, "git ref is too long")
	}
	if strings.HasPrefix(ref, "-") || strings.Contains(ref, "..") || strings.Contains(ref, "@{") || strings.HasSuffix(ref, ".lock") {
		return apperrors.New(apperrors.ErrBadRequest, "git ref is invalid")
	}
	if ref == "@" || strings.ContainsAny(ref, "\\:?*[") {
		return apperrors.New(apperrors.ErrBadRequest, "git ref is invalid")
	}
	for _, r := range ref {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return apperrors.New(apperrors.ErrBadRequest, "git ref is invalid")
		}
	}
	return nil
}

func ValidateRelativePath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" || path == "." {
		return nil
	}
	if filepath.IsAbs(path) {
		return apperrors.New(apperrors.ErrBadRequest, "absolute paths are not allowed")
	}
	if strings.Contains(path, "\\") {
		return apperrors.New(apperrors.ErrBadRequest, "backslash paths are not allowed")
	}
	for _, r := range path {
		if unicode.IsControl(r) {
			return apperrors.New(apperrors.ErrBadRequest, "path contains control characters")
		}
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return apperrors.New(apperrors.ErrBadRequest, "parent directory traversal is not allowed")
	}
	return nil
}

func CleanRelativePath(path string) (string, error) {
	path = strings.TrimSpace(path)
	if err := ValidateRelativePath(path); err != nil {
		return "", err
	}
	if path == "" {
		return "", nil
	}
	return filepath.ToSlash(filepath.Clean(path)), nil
}

func Clone(ctx context.Context, req CloneRequest) (RepoInfo, error) {
	info, err := ValidateRepoURL(req.RepoURL, req.AllowedHosts)
	if err != nil {
		return RepoInfo{}, err
	}
	ref := strings.TrimSpace(req.Ref)
	if err := ValidateRef(ref); err != nil {
		return RepoInfo{}, err
	}
	if req.DestDir == "" {
		return RepoInfo{}, apperrors.New(apperrors.ErrBadRequest, "git clone destination is required")
	}

	baseCtx := ctx
	var cancel context.CancelFunc
	if req.Timeout > 0 {
		baseCtx, cancel = context.WithTimeout(ctx, req.Timeout)
		defer cancel()
	}
	cloneCtx, cloneCancel := context.WithCancel(baseCtx)
	defer cloneCancel()

	sizeErrCh := startSizeMonitor(cloneCtx, cloneCancel, req.DestDir, req.MaxRepositoryBytes)

	args := []string{
		"-c", "protocol.allow=never",
		"-c", "protocol.https.allow=always",
		"clone",
		"--depth=1",
		"--no-tags",
		"--single-branch",
	}
	if ref != "" {
		args = append(args, "--branch", ref)
	}
	args = append(args, "--", info.URL, req.DestDir)

	cmd := exec.CommandContext(cloneCtx, "git", args...)
	cmd.Env = append(os.Environ(),
		"GIT_TERMINAL_PROMPT=0",
		"GIT_ASKPASS=",
		"SSH_ASKPASS=",
	)
	if output, err := cmd.CombinedOutput(); err != nil {
		if sizeErr := cloneSizeError(sizeErrCh); sizeErr != nil {
			return RepoInfo{}, sizeErr
		}
		if errors.Is(baseCtx.Err(), context.DeadlineExceeded) {
			return RepoInfo{}, apperrors.New(apperrors.ErrTimeout, "git clone timed out")
		}
		if errors.Is(err, exec.ErrNotFound) {
			return RepoInfo{}, apperrors.New(apperrors.ErrUnavailable, "git executable is unavailable")
		}
		_ = output
		return RepoInfo{}, apperrors.New(apperrors.ErrBadRequest, "failed to clone git repository")
	}
	if sizeErr := cloneSizeError(sizeErrCh); sizeErr != nil {
		return RepoInfo{}, sizeErr
	}
	if _, err := repositorySize(ctx, req.DestDir, req.MaxRepositoryBytes); err != nil {
		return RepoInfo{}, err
	}

	info.Ref = ref
	return info, nil
}

func ArchiveToZipFile(ctx context.Context, sourceDir, archivePath string, limits ArchiveLimits) (ArchiveStats, error) {
	f, err := os.Create(archivePath)
	if err != nil {
		return ArchiveStats{}, err
	}
	defer f.Close()
	return ArchiveToZip(ctx, sourceDir, f, limits)
}

func ArchiveToZip(ctx context.Context, sourceDir string, w io.Writer, limits ArchiveLimits) (ArchiveStats, error) {
	root, err := filepath.Abs(sourceDir)
	if err != nil {
		return ArchiveStats{}, err
	}

	counter := &limitWriter{w: w, max: limits.MaxArchiveBytes}
	zw := zip.NewWriter(counter)
	stats := ArchiveStats{}

	walkErr := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if path == root {
			return nil
		}
		if d.IsDir() {
			if d.Name() == ".git" {
				return filepath.SkipDir
			}
			return nil
		}

		info, err := d.Info()
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return apperrors.New(apperrors.ErrBadRequest, "git repository contains unsupported symlink")
		}
		if !info.Mode().IsRegular() {
			return apperrors.New(apperrors.ErrBadRequest, "git repository contains unsupported file type")
		}
		if limits.MaxRepositoryBytes > 0 && stats.RepositoryBytes+info.Size() > limits.MaxRepositoryBytes {
			return apperrors.New(apperrors.ErrBadRequest, "git repository exceeds configured size limit")
		}

		rel, err := filepath.Rel(root, path)
		if err != nil {
			return err
		}
		name := filepath.ToSlash(rel)
		if name == "." || strings.HasPrefix(name, "../") || strings.HasPrefix(name, "/") {
			return fmt.Errorf("illegal repository file path: %s", rel)
		}

		header, err := zip.FileInfoHeader(info)
		if err != nil {
			return err
		}
		header.Name = name
		header.Method = zip.Deflate

		dst, err := zw.CreateHeader(header)
		if err != nil {
			return err
		}
		src, err := os.Open(path)
		if err != nil {
			return err
		}
		written, copyErr := io.Copy(dst, src)
		closeErr := src.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
		stats.RepositoryBytes += written
		return nil
	})
	closeErr := zw.Close()
	stats.ArchiveBytes = counter.written
	if walkErr != nil {
		return stats, walkErr
	}
	if closeErr != nil {
		return stats, closeErr
	}
	return stats, nil
}

func startSizeMonitor(ctx context.Context, cancel context.CancelFunc, dir string, maxBytes int64) <-chan error {
	if maxBytes <= 0 {
		return nil
	}
	errCh := make(chan error, 1)
	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if _, err := repositorySize(ctx, dir, maxBytes); err != nil {
					if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
						return
					}
					select {
					case errCh <- err:
					default:
					}
					cancel()
					return
				}
			}
		}
	}()
	return errCh
}

func cloneSizeError(errCh <-chan error) error {
	if errCh == nil {
		return nil
	}
	select {
	case err := <-errCh:
		return err
	default:
		return nil
	}
}

func repositorySize(ctx context.Context, root string, maxBytes int64) (int64, error) {
	if root == "" {
		return 0, nil
	}
	if _, err := os.Stat(root); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return 0, nil
		}
		return 0, err
	}
	var size int64
	err := filepath.WalkDir(root, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}
		if d.IsDir() {
			return nil
		}
		info, err := d.Info()
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return nil
			}
			return err
		}
		size += info.Size()
		if maxBytes > 0 && size > maxBytes {
			return apperrors.New(apperrors.ErrBadRequest, "git repository exceeds configured size limit")
		}
		return nil
	})
	if err != nil {
		return size, err
	}
	return size, nil
}

func hostAllowed(host string, allowedHosts []string) bool {
	for _, item := range allowedHosts {
		if host == strings.ToLower(strings.TrimSpace(item)) {
			return true
		}
	}
	return false
}

func validateRepoPath(path string) error {
	if path == "" {
		return apperrors.New(apperrors.ErrBadRequest, "git repository path is required")
	}
	if strings.Contains(path, "//") || strings.Contains(path, "\\") {
		return apperrors.New(apperrors.ErrBadRequest, "git repository path is invalid")
	}
	for _, r := range path {
		if unicode.IsSpace(r) || unicode.IsControl(r) {
			return apperrors.New(apperrors.ErrBadRequest, "git repository path is invalid")
		}
	}
	for _, segment := range strings.Split(path, "/") {
		if segment == "" || segment == "." || segment == ".." {
			return apperrors.New(apperrors.ErrBadRequest, "git repository path is invalid")
		}
	}
	return nil
}

type limitWriter struct {
	w       io.Writer
	max     int64
	written int64
}

func (w *limitWriter) Write(p []byte) (int, error) {
	if w.max > 0 && w.written+int64(len(p)) > w.max {
		return 0, apperrors.New(apperrors.ErrBadRequest, "git source archive exceeds configured size limit")
	}
	n, err := w.w.Write(p)
	w.written += int64(n)
	return n, err
}
