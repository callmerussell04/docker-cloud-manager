package validation_test

import (
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/validation"
)

// echoMarker is embedded in every rejected value; it must never reach a message.
const echoMarker = "echomarker"

// Services wrap validation errors as fmt.Errorf("%w: %v", ErrBadRequest, err),
// and SafeMessage passes bad-request messages through to clients. That is safe
// only while no validation message echoes the value it rejected.
func TestValidationErrorsNeverEchoTheirInput(t *testing.T) {
	for _, tc := range []struct {
		name     string
		validate func(string) error
		input    string
	}{
		{name: "domain prefix", validate: validation.DomainPrefix, input: "Bad_" + echoMarker},
		{name: "resource name", validate: validation.ResourceName, input: "-" + echoMarker},
		{name: "project name", validate: validation.ProjectName, input: echoMarker + "!"},
		{name: "image tag with registry host", validate: validation.ImageTag, input: "registry." + echoMarker + ".io/app:v1"},
		{name: "image tag with invalid reference", validate: validation.ImageTag, input: "UPPER/" + echoMarker + ":v1"},
		{name: "image tag with digest", validate: validation.ImageTag, input: echoMarker + "@sha256:" + "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		{name: "relative mount path", validate: validation.MountPath, input: echoMarker},
		{name: "mount path under proc", validate: validation.MountPath, input: "/proc/" + echoMarker},
		{name: "docker size", validate: validation.DockerSize, input: echoMarker},
		{name: "positive number", validate: validation.PositiveNumberString, input: echoMarker},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.validate(tc.input)
			require.Error(t, err)

			wrapped := fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
			require.NotContains(t, err.Error(), echoMarker)
			require.NotContains(t, apperrors.SafeMessage(wrapped), echoMarker)
			require.Contains(t, apperrors.SafeMessage(wrapped), err.Error(), "bad-request messages pass through")
		})
	}
}
