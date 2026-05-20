package compose_test

import . "github.com/callmerussell04/docker-cloud-manager/internal/core/service/compose"

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
)

func TestParserRestartPolicyValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		restart     string
		wantErrPart string
	}{
		{name: "no", restart: `"no"`},
		{name: "on failure", restart: "on-failure"},
		{name: "always rejected", restart: "always", wantErrPart: "restart policy always is not allowed"},
		{name: "unless stopped rejected", restart: "unless-stopped", wantErrPart: "restart policy unless-stopped is not allowed"},
		{name: "max retries rejected", restart: "on-failure:3", wantErrPart: "on-failure max retries are not supported"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			yaml := []byte(`
services:
  web:
    image: nginx:latest
    restart: ` + tt.restart + `
`)

			project, err := NewParser().ParseAndValidate(context.Background(), "proj", yaml, nil)
			if tt.wantErrPart == "" {
				if err != nil {
					t.Fatalf("ParseAndValidate() error = %v", err)
				}
				if len(project.Services) != 1 {
					t.Fatalf("services count = %d, want 1", len(project.Services))
				}
				if got := project.Services[0].Restart; got != strings.Trim(tt.restart, `"`) {
					t.Fatalf("restart = %q, want %q", got, strings.Trim(tt.restart, `"`))
				}
				return
			}

			if err == nil {
				t.Fatalf("ParseAndValidate() error = nil, want error containing %q", tt.wantErrPart)
			}
			if !strings.Contains(err.Error(), tt.wantErrPart) {
				t.Fatalf("ParseAndValidate() error = %q, want containing %q", err.Error(), tt.wantErrPart)
			}
		})
	}
}

func TestParserDependsOnRequiredFalseIsPreserved(t *testing.T) {
	t.Parallel()

	yaml := []byte(`
services:
  web:
    image: nginx:latest
    depends_on:
      db:
        condition: service_healthy
        required: false
  db:
    image: postgres:16
`)

	project, err := NewParser().ParseAndValidate(context.Background(), "proj", yaml, nil)
	if err != nil {
		t.Fatalf("ParseAndValidate() error = %v", err)
	}

	web := findComposeService(t, project.Services, "web")
	if len(web.DependsOn) != 1 {
		t.Fatalf("depends_on count = %d, want 1", len(web.DependsOn))
	}
	dep := web.DependsOn[0]
	if dep.ServiceName != "db" {
		t.Fatalf("dependency service = %q, want db", dep.ServiceName)
	}
	if dep.Condition != "service_healthy" {
		t.Fatalf("dependency condition = %q, want service_healthy", dep.Condition)
	}
	if !dep.Optional {
		t.Fatalf("dependency optional = false, want true")
	}
}

func TestParserRejectsDuplicateDomainPrefixes(t *testing.T) {
	t.Parallel()

	yaml := []byte(`
services:
  web:
    image: nginx:latest
    labels:
      dcm.domain_prefix: app
      dcm.internal_port: "8080"
  api:
    image: nginx:latest
    labels:
      dcm.domain_prefix: app
      dcm.internal_port: "8081"
`)

	_, err := NewParser().ParseAndValidate(context.Background(), "proj", yaml, nil)
	if !errors.Is(err, apperrors.ErrAlreadyExists) {
		t.Fatalf("ParseAndValidate() error = %v, want already exists", err)
	}
	if got := apperrors.SafeMessage(err); !strings.Contains(got, "subdomain app is used by both services") {
		t.Fatalf("SafeMessage() = %q, want duplicate subdomain message", got)
	}
}

func TestParserDependsOnDisabledOptionalServicePasses(t *testing.T) {
	t.Parallel()

	yaml := []byte(`
services:
  web:
    image: nginx:latest
    depends_on:
      db:
        condition: service_started
        required: false
  db:
    image: postgres:16
    profiles:
      - debug
`)

	project, err := NewParser().ParseAndValidate(context.Background(), "proj", yaml, nil)
	if err != nil {
		t.Fatalf("ParseAndValidate() error = %v", err)
	}

	web := findComposeService(t, project.Services, "web")
	if len(web.DependsOn) != 1 {
		t.Fatalf("depends_on count = %d, want 1", len(web.DependsOn))
	}
	if !web.DependsOn[0].Optional {
		t.Fatalf("dependency optional = false, want true")
	}
}

func TestParserDependsOnDisabledRequiredServiceRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		depConfig  string
		wantErrMsg string
	}{
		{
			name: "default required",
			depConfig: `
      db:
        condition: service_started`,
			wantErrMsg: `service "web" depends on undefined service "db"`,
		},
		{
			name: "explicit required",
			depConfig: `
      db:
        condition: service_started
        required: true`,
			wantErrMsg: `service "web" depends on undefined service "db"`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			yaml := []byte(`
services:
  web:
    image: nginx:latest
    depends_on:` + tt.depConfig + `
  db:
    image: postgres:16
    profiles:
      - debug
`)

			_, err := NewParser().ParseAndValidate(context.Background(), "proj", yaml, nil)
			if err == nil {
				t.Fatalf("ParseAndValidate() error = nil, want disabled required dependency error")
			}
			if !strings.Contains(err.Error(), tt.wantErrMsg) {
				t.Fatalf("ParseAndValidate() error = %q, want containing %q", err.Error(), tt.wantErrMsg)
			}
		})
	}
}

func TestParserDependsOnUnknownRequiredServiceRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		depConfig string
	}{
		{
			name: "default required",
			depConfig: `
      db:
        condition: service_started`,
		},
		{
			name: "explicit required",
			depConfig: `
      db:
        condition: service_started
        required: true`,
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			yaml := []byte(`
services:
  web:
    image: nginx:latest
    depends_on:` + tt.depConfig + `
`)

			_, err := NewParser().ParseAndValidate(context.Background(), "proj", yaml, nil)
			if err == nil {
				t.Fatalf("ParseAndValidate() error = nil, want unknown required dependency error")
			}
			wantErrMsg := `service "web" depends on undefined service "db"`
			if !strings.Contains(err.Error(), wantErrMsg) {
				t.Fatalf("ParseAndValidate() error = %q, want containing %q", err.Error(), wantErrMsg)
			}
		})
	}
}

func TestParserDependsOnRestartStillRejected(t *testing.T) {
	t.Parallel()

	yaml := []byte(`
services:
  web:
    image: nginx:latest
    depends_on:
      db:
        condition: service_started
        restart: true
  db:
    image: postgres:16
`)

	_, err := NewParser().ParseAndValidate(context.Background(), "proj", yaml, nil)
	if err == nil {
		t.Fatalf("ParseAndValidate() error = nil, want depends_on.restart error")
	}
	if !strings.Contains(err.Error(), "depends_on.restart is not supported") {
		t.Fatalf("ParseAndValidate() error = %q, want depends_on.restart error", err.Error())
	}
}

func TestParserNormalizesBuildContextFromComposeBaseDir(t *testing.T) {
	t.Parallel()

	yaml := []byte(`
services:
  web:
    build:
      context: ./app
      dockerfile: Dockerfile
    image: web:latest
`)

	project, err := NewParser().ParseAndValidateWithBase(context.Background(), "proj", yaml, nil, "deploy")
	if err != nil {
		t.Fatalf("ParseAndValidateWithBase() error = %v", err)
	}

	web := findComposeService(t, project.Services, "web")
	if web.BuildContext != "deploy/app" {
		t.Fatalf("build context = %q, want deploy/app", web.BuildContext)
	}
	if web.Dockerfile != "Dockerfile" {
		t.Fatalf("dockerfile = %q, want Dockerfile", web.Dockerfile)
	}
}

func TestParserRejectsBuildContextEscapingComposeBaseDir(t *testing.T) {
	t.Parallel()

	yaml := []byte(`
services:
  web:
    build:
      context: ../app
    image: web:latest
`)

	_, err := NewParser().ParseAndValidateWithBase(context.Background(), "proj", yaml, nil, "deploy")
	if err == nil {
		t.Fatalf("ParseAndValidateWithBase() error = nil, want invalid build context")
	}
	if !strings.Contains(err.Error(), "parent directory traversal is not allowed") {
		t.Fatalf("ParseAndValidateWithBase() error = %q, want traversal error", err.Error())
	}
}

func findComposeService(t *testing.T, services []model.ComposeService, name string) model.ComposeService {
	t.Helper()
	for _, service := range services {
		if service.Name == name {
			return service
		}
	}
	t.Fatalf("service %q not found", name)
	return model.ComposeService{}
}
