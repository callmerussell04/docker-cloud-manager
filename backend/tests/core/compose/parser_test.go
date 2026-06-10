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
		wantRestart string
	}{
		{name: "no", restart: `"no"`, wantRestart: "no"},
		{name: "on failure", restart: "on-failure", wantRestart: "on-failure"},
		{name: "always ignored", restart: "always"},
		{name: "unless stopped ignored", restart: "unless-stopped"},
		{name: "max retries ignored", restart: "on-failure:3"},
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
			if err != nil {
				t.Fatalf("ParseAndValidate() error = %v", err)
			}
			if len(project.Services) != 1 {
				t.Fatalf("services count = %d, want 1", len(project.Services))
			}
			if got := project.Services[0].Restart; got != tt.wantRestart {
				t.Fatalf("restart = %q, want %q", got, tt.wantRestart)
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

func TestParserPrefixesManagedVolumesButKeepsExternalNames(t *testing.T) {
	t.Parallel()

	yaml := []byte(`
services:
  db:
    image: postgres:16
    volumes:
      - postgres_data:/var/lib/postgresql/data
      - shared_data:/shared:ro
volumes:
  postgres_data: {}
  shared_data:
    external: true
    name: project_a_postgres_data
`)

	project, err := NewParser().ParseAndValidate(context.Background(), "project_a", yaml, nil)
	if err != nil {
		t.Fatalf("ParseAndValidate() error = %v", err)
	}
	if len(project.Volumes) != 2 {
		t.Fatalf("volumes count = %d, want 2", len(project.Volumes))
	}

	managed := findComposeVolume(t, project.Volumes, "postgres_data")
	if managed.Name != "project_a_postgres_data" {
		t.Fatalf("managed volume name = %q, want project_a_postgres_data", managed.Name)
	}
	if managed.External {
		t.Fatalf("managed volume external = true, want false")
	}

	external := findComposeVolume(t, project.Volumes, "shared_data")
	if external.Name != "project_a_postgres_data" {
		t.Fatalf("external volume name = %q, want project_a_postgres_data", external.Name)
	}
	if !external.External {
		t.Fatalf("external volume external = false, want true")
	}
}

func TestParserRejectsBlockedDomainPrefixPattern(t *testing.T) {
	t.Parallel()

	yaml := []byte(`
services:
  web:
    image: nginx:latest
    labels:
      dcm.domain_prefix: preview-42
      dcm.internal_port: "8080"
`)

	_, err := NewParser().ParseAndValidateWithBaseAndBlocked(context.Background(), "proj", yaml, nil, []string{"preview-[0-9]+"}, "")
	if !errors.Is(err, apperrors.ErrBadRequest) {
		t.Fatalf("ParseAndValidateWithBaseAndBlocked() error = %v, want bad request", err)
	}
	if got := apperrors.SafeMessage(err); got != `domain prefix "preview-42" is forbidden` {
		t.Fatalf("SafeMessage() = %q, want forbidden domain prefix message", got)
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

func TestParserDependsOnRestartIsIgnored(t *testing.T) {
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

	project, err := NewParser().ParseAndValidate(context.Background(), "proj", yaml, nil)
	if err != nil {
		t.Fatalf("ParseAndValidate() error = %v", err)
	}
	web := findComposeService(t, project.Services, "web")
	if len(web.DependsOn) != 1 {
		t.Fatalf("depends_on count = %d, want 1", len(web.DependsOn))
	}
	dep := web.DependsOn[0]
	if dep.ServiceName != "db" || dep.Condition != model.ComposeDependencyConditionStarted || dep.Optional {
		t.Fatalf("dependency = %+v, want required db service_started dependency", dep)
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

func TestParserPreservesManagedAndExternalVolumeAliases(t *testing.T) {
	t.Parallel()

	yaml := []byte(`
services:
  web:
    image: nginx:latest
    volumes:
      - data:/var/lib/app
      - cache:/cache:ro
      - custom:/custom
volumes:
  data: {}
  cache:
    external: true
  custom:
    name: shared-data
    external: true
`)

	project, err := NewParser().ParseAndValidate(context.Background(), "proj", yaml, nil)
	if err != nil {
		t.Fatalf("ParseAndValidate() error = %v", err)
	}

	data := findComposeVolume(t, project.Volumes, "data")
	if data.Name != "proj_data" || data.External {
		t.Fatalf("data volume = %+v, want managed alias data/name proj_data", data)
	}
	cache := findComposeVolume(t, project.Volumes, "cache")
	if cache.Name != "cache" || !cache.External {
		t.Fatalf("cache volume = %+v, want external alias cache/name cache", cache)
	}
	custom := findComposeVolume(t, project.Volumes, "custom")
	if custom.Name != "shared-data" || !custom.External {
		t.Fatalf("custom volume = %+v, want external alias custom/name shared-data", custom)
	}

	web := findComposeService(t, project.Services, "web")
	if len(web.VolumeMounts) != 3 {
		t.Fatalf("volume mounts count = %d, want 3", len(web.VolumeMounts))
	}
	if web.VolumeMounts[1].VolumeName != "cache" || !web.VolumeMounts[1].IsReadOnly {
		t.Fatalf("second mount = %+v, want readonly cache alias", web.VolumeMounts[1])
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

func findComposeVolume(t *testing.T, volumes []model.ComposeVolume, alias string) model.ComposeVolume {
	t.Helper()
	for _, volume := range volumes {
		if volume.Alias == alias {
			return volume
		}
	}
	t.Fatalf("volume alias %q not found", alias)
	return model.ComposeVolume{}
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
