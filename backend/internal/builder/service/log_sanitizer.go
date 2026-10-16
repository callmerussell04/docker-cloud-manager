package service

import (
	"bufio"
	"io"
	"math"
	"regexp"
	"strings"

	"github.com/callmerussell04/docker-cloud-manager/internal/builder/model"
)

const publishNoticeLine = "[SYSTEM] Publishing image to internal registry...\n"

var credentialValuePattern = regexp.MustCompile(`(?i)\b(token|password|passwd|secret|authorization|auth)\b\s*[:=]\s*("[^"\s]*"|'[^'\s]*'|[^\s,;]+)`)
var authorizationLinePattern = regexp.MustCompile(`(?i)\bauthorization\b\s*[:=]\s*[^\r\n]+`)

type buildLogSanitizerOptions struct {
	MaxBytes       int64
	RegistryURL    string
	DestinationTag string
}

type buildLogSanitizer struct {
	reader       *bufio.Reader
	opts         buildLogSanitizerOptions
	imageRef     *regexp.Regexp
	pending      []byte
	remaining    int64
	terminalErr  error
	publishShown bool
}

func newBuildLogSanitizer(source io.Reader, opts buildLogSanitizerOptions) io.Reader {
	limit := max(opts.MaxBytes, 0)
	readLimit := limit
	if readLimit < math.MaxInt64 {
		readLimit++
	}
	s := &buildLogSanitizer{
		remaining: limit,
		reader:    bufio.NewReader(io.LimitReader(source, readLimit)),
		opts:      opts,
	}
	if opts.RegistryURL != "" {
		s.imageRef = regexp.MustCompile(regexp.QuoteMeta(opts.RegistryURL) + `/[^\s,'"<>]+`)
	}
	return s
}

func (s *buildLogSanitizer) Read(p []byte) (int, error) {
	if len(p) == 0 {
		return 0, nil
	}
	for len(s.pending) == 0 {
		if s.terminalErr != nil {
			return 0, s.terminalErr
		}
		// The source is limited before ReadString can assemble a newline-free line.
		line, err := s.reader.ReadString('\n')
		if int64(len(line)) > s.remaining {
			s.terminalErr = model.ErrBuildLogSizeLimitExceeded
			return 0, s.terminalErr
		}
		s.remaining -= int64(len(line))
		s.terminalErr = err
		if len(line) > 0 {
			s.pending = []byte(s.sanitizeLine(line))
		}
	}
	n := copy(p, s.pending)
	s.pending = s.pending[n:]
	return n, nil
}

func (s *buildLogSanitizer) sanitizeLine(line string) string {
	if s.isInternalPublishLine(line) {
		if s.publishShown {
			return ""
		}
		s.publishShown = true
		return publishNoticeLine
	}

	line = authorizationLinePattern.ReplaceAllString(line, "Authorization=[REDACTED]")
	line = credentialValuePattern.ReplaceAllString(line, "${1}=[REDACTED]")
	if s.opts.DestinationTag != "" {
		line = strings.ReplaceAll(line, s.opts.DestinationTag, "[internal-image]")
	}
	if s.imageRef != nil {
		line = s.imageRef.ReplaceAllString(line, "[internal-image]")
	}
	if s.opts.RegistryURL != "" {
		line = strings.ReplaceAll(line, s.opts.RegistryURL, "[internal-registry]")
	}
	return line
}

func (s *buildLogSanitizer) isInternalPublishLine(line string) bool {
	lower := strings.ToLower(line)
	hasPublishDetail := strings.Contains(lower, "push") ||
		strings.Contains(lower, "pushed") ||
		strings.Contains(lower, "digest") ||
		strings.Contains(lower, "sha256:") ||
		strings.Contains(lower, "destination")
	if !hasPublishDetail {
		return false
	}

	return (s.opts.RegistryURL != "" && strings.Contains(line, s.opts.RegistryURL)) ||
		(s.opts.DestinationTag != "" && strings.Contains(line, s.opts.DestinationTag)) ||
		isKanikoLogLine(line)
}

func isKanikoLogLine(line string) bool {
	line = strings.TrimSpace(line)
	return strings.HasPrefix(line, "INFO[") ||
		strings.HasPrefix(line, "WARN[") ||
		strings.HasPrefix(line, "ERRO[") ||
		strings.HasPrefix(line, "DEBU[")
}
