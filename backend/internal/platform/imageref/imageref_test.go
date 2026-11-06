package imageref_test

import (
	"regexp"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/imageref"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/validation"
	"github.com/distribution/reference"
	"github.com/stretchr/testify/require"
)

var anchoredTag = regexp.MustCompile(`^` + reference.TagRegexp.String() + `$`)

// FuzzParseTag pins why ParseTag may split on the first colon: validation
// rejects every registry host:port prefix before a tag reaches parsing, so the
// first colon of an accepted tag always separates the name from its version.
func FuzzParseTag(f *testing.F) {
	for _, seed := range []string{
		"nginx", "nginx:alpine", "nginx:", "docker.io/library/nginx:1.27", "my-app:v1",
		"registry:5000/repo:tag", "localhost:5000/app", "127.0.0.1:5000/app:v1",
		"nginx@sha256:0000000000000000000000000000000000000000000000000000000000000000",
		"a:b:c", ":tag", "",
	} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, tag string) {
		baseName, version := imageref.ParseTag(tag)
		normalized := imageref.NormalizeTag(tag)
		require.Equal(t, baseName+":"+version, normalized)

		if validation.ImageTag(tag) != nil {
			return
		}
		if first, _, ok := strings.Cut(tag, "/"); ok {
			require.NotContains(t, first, ":", "a registry host:port prefix must be rejected before tag parsing")
		}
		require.NotContains(t, baseName, ":")
		require.Regexp(t, anchoredTag, version)

		// Normalisation round-trips: the normalized tag is still valid and parses back to the same parts.
		require.NoError(t, validation.ImageTag(normalized))
		gotBase, gotVersion := imageref.ParseTag(normalized)
		require.Equal(t, baseName, gotBase)
		require.Equal(t, version, gotVersion)
		require.Equal(t, normalized, imageref.NormalizeTag(normalized))
	})
}
