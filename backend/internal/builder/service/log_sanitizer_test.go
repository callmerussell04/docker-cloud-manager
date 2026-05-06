package service

import (
	"io"
	"strings"
	"testing"
)

func TestBuildLogSanitizerCollapsesInternalRegistryPublishLines(t *testing.T) {
	raw := strings.Join([]string{
		"INFO[0001] RUN npm test",
		"INFO[0002] Pushing image to registry:5000/owner_app:latest",
		"INFO[0003] Pushed registry:5000/owner_app@sha256:abc123",
		"INFO[0004] Digest: sha256:def456 registry:5000/owner_app:latest",
		"INFO[0005] digest: sha256:nohost",
		"done",
	}, "\n") + "\n"

	got := readSanitizedLog(t, raw)

	if !strings.Contains(got, "RUN npm test") || !strings.Contains(got, "done") {
		t.Fatalf("ordinary build lines were not preserved:\n%s", got)
	}
	if strings.Count(got, publishNoticeLine) != 1 {
		t.Fatalf("publish notice count = %d, want 1; logs:\n%s", strings.Count(got, publishNoticeLine), got)
	}
	for _, sensitive := range []string{"registry:5000", "owner_app", "sha256:abc123", "sha256:def456", "sha256:nohost"} {
		if strings.Contains(got, sensitive) {
			t.Fatalf("sanitized logs contain sensitive value %q:\n%s", sensitive, got)
		}
	}
}

func TestBuildLogSanitizerMasksCredentialsAndInternalImageReferences(t *testing.T) {
	raw := strings.Join([]string{
		"npm WARN token=abc123 password=hunter2 secret='value'",
		"using image registry:5000/owner_app:latest as cache seed",
		"Authorization: Bearer something",
	}, "\n") + "\n"

	got := readSanitizedLog(t, raw)

	for _, preserved := range []string{"npm WARN", "using image", "Authorization=[REDACTED]"} {
		if !strings.Contains(got, preserved) {
			t.Fatalf("expected %q in sanitized logs:\n%s", preserved, got)
		}
	}
	for _, sensitive := range []string{"abc123", "hunter2", "value", "Bearer", "something", "registry:5000", "owner_app"} {
		if strings.Contains(got, sensitive) {
			t.Fatalf("sanitized logs contain sensitive value %q:\n%s", sensitive, got)
		}
	}
}

func readSanitizedLog(t *testing.T, raw string) string {
	t.Helper()
	reader := newBuildLogSanitizer(strings.NewReader(raw), buildLogSanitizerOptions{
		RegistryURL:    "registry:5000",
		DestinationTag: "registry:5000/owner_app:latest",
	})
	data, err := io.ReadAll(reader)
	if err != nil {
		t.Fatalf("failed to read sanitized logs: %v", err)
	}
	return string(data)
}
