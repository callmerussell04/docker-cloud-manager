package validation_test

import (
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
	"github.com/stretchr/testify/require"
)

func TestForbiddenDomainPrefix(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		prefix   string
		reserved []string
		patterns []string
		want     bool
	}{
		{name: "empty prefix allowed", prefix: "", reserved: []string{"admin"}, patterns: []string{"preview-[0-9]+"}},
		{name: "exact reserved match", prefix: "admin", reserved: []string{"admin"}, want: true},
		{name: "regex full match", prefix: "preview-42", patterns: []string{"preview-[0-9]+"}, want: true},
		{name: "regex is auto anchored", prefix: "my-preview-42", patterns: []string{"preview-[0-9]+"}},
		{name: "non match", prefix: "app", reserved: []string{"admin"}, patterns: []string{"preview-[0-9]+"}},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			require.Equal(t, tt.want, validation.ForbiddenDomainPrefix(tt.prefix, tt.reserved, tt.patterns))
		})
	}
}
