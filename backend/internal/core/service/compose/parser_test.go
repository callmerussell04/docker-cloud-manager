package compose_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/service/compose"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/validation"
)

func TestDocumentedComposeExamplesRemainValid(t *testing.T) {
	t.Parallel()

	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("failed to locate parser test file")
	}
	examplesDir := filepath.Join(filepath.Dir(filename), "../../../../../docs/examples")
	tests := []struct {
		name     string
		path     string
		basePath string
	}{
		{name: "public image", path: "compose-public-image.yml"},
		{name: "external volume", path: "compose-external-volume.yml"},
		{name: "source build", path: "compose-build/docker-compose.yml", basePath: "compose-build"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			content, err := os.ReadFile(filepath.Join(examplesDir, tt.path))
			if err != nil {
				t.Fatalf("read documented compose example: %v", err)
			}
			if _, err := compose.NewParser().ParseAndValidateWithBase(context.Background(), "docs-example", content, nil, tt.basePath); err != nil {
				t.Fatalf("ParseAndValidateWithBase() error = %v", err)
			}
		})
	}
}

func TestDocumentedUnsafeComposeDirectivesRemainRejected(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		service string
		want    string
	}{
		{name: "host network", service: "network_mode: host", want: "network_mode: host is not allowed"},
		{name: "host pid", service: "pid: host", want: "pid: host is not allowed"},
		{name: "privileged", service: "privileged: true", want: "privileged mode is not allowed"},
		{name: "absolute bind mount", service: "volumes:\n      - /var/run/docker.sock:/var/run/docker.sock", want: "absolute bind mounts are not allowed"},
		{name: "relative bind mount", service: "volumes:\n      - ./data:/data", want: "bind mounts are temporarily disabled"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			yaml := []byte("services:\n  web:\n    image: nginx:alpine\n    " + tt.service + "\n")
			_, err := compose.NewParser().ParseAndValidateWithBase(context.Background(), "docs-example", yaml, nil, "")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("ParseAndValidateWithBase() error = %v, want containing %q", err, tt.want)
			}
		})
	}
}

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

			project, err := compose.NewParser().ParseAndValidateWithBase(context.Background(), "proj", yaml, nil, "")
			if err != nil {
				t.Fatalf("ParseAndValidateWithBase() error = %v", err)
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

	project, err := compose.NewParser().ParseAndValidateWithBase(context.Background(), "proj", yaml, nil, "")
	if err != nil {
		t.Fatalf("ParseAndValidateWithBase() error = %v", err)
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

	_, err := compose.NewParser().ParseAndValidateWithBase(context.Background(), "proj", yaml, nil, "")
	if !errors.Is(err, apperrors.ErrAlreadyExists) {
		t.Fatalf("ParseAndValidateWithBase() error = %v, want already exists", err)
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

	project, err := compose.NewParser().ParseAndValidateWithBase(context.Background(), "project_a", yaml, nil, "")
	if err != nil {
		t.Fatalf("ParseAndValidateWithBase() error = %v", err)
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

	_, err := compose.NewParser().ParseAndValidateWithBaseAndBlocked(context.Background(), "proj", yaml, nil, []string{"preview-[0-9]+"}, "")
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

	project, err := compose.NewParser().ParseAndValidateWithBase(context.Background(), "proj", yaml, nil, "")
	if err != nil {
		t.Fatalf("ParseAndValidateWithBase() error = %v", err)
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
  db:
    image: postgres:16
    profiles:
      - debug
`)

			_, err := compose.NewParser().ParseAndValidateWithBase(context.Background(), "proj", yaml, nil, "")
			requireRejection(t, err, apperrors.ErrBadRequest, invalidComposeProjectMessage)
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

			_, err := compose.NewParser().ParseAndValidateWithBase(context.Background(), "proj", yaml, nil, "")
			requireRejection(t, err, apperrors.ErrBadRequest, invalidComposeProjectMessage)
		})
	}
}

// invalidComposeProjectMessage is the fixed message for a file compose-go
// rejects. compose-go's own message names the undefined dependency, which is
// rejected input, so it is not passed on.
const invalidComposeProjectMessage = "bad request: compose file is not a valid Compose project"

func requireRejection(t *testing.T, err, kind error, want string) {
	t.Helper()
	if !errors.Is(err, kind) {
		t.Fatalf("ParseAndValidateWithBase() error = %v, want %v", err, kind)
	}
	if got := apperrors.SafeMessage(err); got != want {
		t.Fatalf("SafeMessage() = %q, want %q", got, want)
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

	project, err := compose.NewParser().ParseAndValidateWithBase(context.Background(), "proj", yaml, nil, "")
	if err != nil {
		t.Fatalf("ParseAndValidateWithBase() error = %v", err)
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

	project, err := compose.NewParser().ParseAndValidateWithBase(context.Background(), "proj", yaml, nil, "deploy")
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

	project, err := compose.NewParser().ParseAndValidateWithBase(context.Background(), "proj", yaml, nil, "")
	if err != nil {
		t.Fatalf("ParseAndValidateWithBase() error = %v", err)
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

	_, err := compose.NewParser().ParseAndValidateWithBase(context.Background(), "proj", yaml, nil, "deploy")
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

// FuzzParser checks that untrusted Compose YAML never panics the parser, fails
// only as a client error, and that an accepted project keeps every build path
// inside the repository and every image tag valid.
func FuzzParser(f *testing.F) {
	for _, seed := range []struct{ yaml, baseDir string }{
		{"services:\n  web:\n    image: nginx:alpine\n", ""},
		{"services:\n  web:\n    build:\n      context: ./app\n      dockerfile: Dockerfile\n    image: web:latest\n", "deploy"},
		{"services:\n  web:\n    build: ../outside\n", ""},
		{"services:\n  web:\n    build:\n      context: app\n      dockerfile: ../../Dockerfile\n", "deploy"},
		{"services:\n  web:\n    build: /abs\n", "../up"},
		{"services:\n  web:\n    image: registry:5000/app\n    privileged: true\n", ""},
		{"services:\n  a:\n    image: nginx\n    depends_on:\n      b:\n        condition: service_healthy\n  b:\n    image: redis\n", ""},
		{"volumes:\n  data:\n    external: true\nservices:\n  db:\n    image: postgres\n    volumes:\n      - data:/var/lib/postgresql/data\n", ""},
		{"x: &a [1, *a]\n", ""},
		{"", "."},
	} {
		f.Add([]byte(seed.yaml), seed.baseDir)
	}
	f.Fuzz(func(t *testing.T, yaml []byte, baseDir string) {
		project, err := compose.NewParser().ParseAndValidateWithBase(context.Background(), "proj", yaml, nil, baseDir)
		if err != nil {
			if !errors.Is(err, apperrors.ErrBadRequest) && !errors.Is(err, apperrors.ErrAlreadyExists) {
				t.Fatalf("parser error is not a client error: %v", err)
			}
			return
		}
		for _, service := range project.Services {
			if err := validation.ImageTag(service.ImageTag); err != nil {
				t.Fatalf("service %q accepted with invalid image tag %q", service.Name, service.ImageTag)
			}
			for _, path := range []string{service.BuildContext, service.Dockerfile} {
				if path == "" {
					continue
				}
				clean := filepath.ToSlash(filepath.Clean(path))
				if filepath.IsAbs(path) || clean == ".." || strings.HasPrefix(clean, "../") {
					t.Fatalf("service %q accepted with build path %q outside the repository", service.Name, path)
				}
			}
		}
	})
}

// compose-go assumes some top-level sections are maps; malformed sections must
// fail as client errors instead of panicking the Core process.
func TestParserRejectsMalformedTopLevelSections(t *testing.T) {
	t.Parallel()
	for _, yaml := range []string{
		"services:\n",
		"services: web\n",
		"networks: 1\nservices:\n  web:\n    image: nginx\n",
		"services:\n  web:\n",
		"volumes: [data]\nservices:\n  web:\n    image: nginx\n",
	} {
		t.Run(yaml, func(t *testing.T) {
			t.Parallel()
			_, err := compose.NewParser().ParseAndValidateWithBase(context.Background(), "proj", []byte(yaml), nil, "")
			if !errors.Is(err, apperrors.ErrBadRequest) {
				t.Fatalf("ParseAndValidateWithBase() error = %v, want bad request", err)
			}
		})
	}
}

// echoMarker stands for user input a bad-request message must not repeat.
const echoMarker = "zzmarker"

const resourceNameRule = "name must start with a letter or digit and contain only letters, digits, underscore, dot or hyphen"

// Bad-request messages reach the uploader and the stored deployment error, and
// failed jobs log them. Each reachable rejecting branch gets a row whose input
// carries echoMarker. Messages name only identifiers that passed validation
// (echoAllowed marks the rows where that identifier is the marker) and never
// repeat a rejected value or a raw compose-go error.
//
// Not listed: the custom volume driver checks, which sanitizeComposeModel makes
// unreachable, and the parser's own undefined-volume and image-or-build checks,
// which compose-go pre-empts (see their compose-go rows).
func TestParserRejectionsNeverEchoRejectedInput(t *testing.T) {
	t.Parallel()

	const web = "services:\n  web:\n    image: nginx\n"
	tests := []struct {
		name        string
		project     string
		baseDir     string
		reserved    []string
		yaml        string
		want        string
		echoAllowed bool
	}{
		// Request arguments.
		{
			name:    "invalid project name",
			project: "-" + echoMarker,
			yaml:    web,
			want:    "bad request: " + resourceNameRule,
		},
		{
			name:    "compose file path outside the repository",
			baseDir: "../" + echoMarker,
			yaml:    web,
			want:    "bad request: invalid compose file path: parent directory traversal is not allowed",
		},
		// compose-go load failures.
		{
			name: "compose-go interpolation error",
			yaml: web + "    environment:\n      TOKEN: \"${MISSING:?" + echoMarker + "}\"\n",
			want: invalidComposeProjectMessage,
		},
		{
			name: "undefined volume is rejected by compose-go",
			yaml: web + "    volumes:\n      - type: volume\n        source: " + echoMarker + "\n        target: /data\n",
			want: invalidComposeProjectMessage,
		},
		{
			name: "neither image nor build is rejected by compose-go",
			yaml: "services:\n  web:\n    environment:\n      TOKEN: " + echoMarker + "\n",
			want: invalidComposeProjectMessage,
		},
		{
			name: "compose-go panic on a malformed section",
			yaml: "services: " + echoMarker + "\n",
			want: invalidComposeProjectMessage,
		},
		{
			name: "YAML syntax error keeps only its position",
			yaml: web + "    environment:\n      TOKEN: " + echoMarker + ": bad\n",
			want: "bad request: compose file is not valid YAML (line 5, column 22)",
		},
		{
			name: "YAML scanner error at a line start",
			yaml: "services:\n\t" + echoMarker + ": nginx\n",
			want: "bad request: compose file is not valid YAML (line 2)",
		},
		{
			name: "YAML error reports the problem, not its enclosing scalar",
			yaml: web + "    environment:\n      TOKEN: \"" + echoMarker + "\n",
			want: "bad request: compose file is not valid YAML (line 6)",
		},
		{
			name: "YAML construct error keeps only the offending line",
			yaml: "services:\n  " + echoMarker + ":\n    image: nginx\n  " + echoMarker + ":\n    image: nginx\n",
			want: "bad request: compose file is not valid YAML (line 4)",
		},
		{
			name: "YAML construct error with only the document line has no position",
			yaml: web + "    healthcheck:\n      retries: !!int " + echoMarker + "\n",
			want: "bad request: compose file is not valid YAML",
		},
		// Security checks, which run before names are validated.
		{
			name: "host network",
			yaml: "services:\n  " + echoMarker + ":\n    image: nginx\n    network_mode: host\n",
			want: "bad request: network_mode: host is not allowed",
		},
		{
			name: "privileged",
			yaml: "services:\n  " + echoMarker + ":\n    image: nginx\n    privileged: true\n",
			want: "bad request: privileged mode is not allowed",
		},
		{
			name: "host pid",
			yaml: "services:\n  " + echoMarker + ":\n    image: nginx\n    pid: host\n",
			want: "bad request: pid: host is not allowed",
		},
		{
			name: "absolute bind mount host path",
			yaml: web + "    volumes:\n      - /" + echoMarker + ":/data\n",
			want: "bad request: absolute bind mounts are not allowed",
		},
		// Volumes.
		{
			name: "invalid volume name",
			yaml: "volumes:\n  -" + echoMarker + ": {}\n" + web,
			want: "bad request: volume has invalid name: " + resourceNameRule,
		},
		{
			name: "invalid external volume name names only the validated alias",
			yaml: "volumes:\n  data:\n    external: true\n    name: -" + echoMarker + "\n" + web,
			want: "bad request: external volume data has invalid name: " + resourceNameRule,
		},
		// Services, whose name is validated first.
		{
			name: "invalid service name",
			yaml: "services:\n  -" + echoMarker + ":\n    image: nginx\n",
			want: "bad request: service has invalid name: " + resourceNameRule,
		},
		{
			name: "build context outside the repository",
			yaml: "services:\n  web:\n    build: ../" + echoMarker + "\n",
			want: "bad request: invalid build context for service web: parent directory traversal is not allowed",
		},
		{
			name: "dockerfile outside the repository",
			yaml: "services:\n  web:\n    build:\n      context: .\n      dockerfile: ../" + echoMarker + "\n",
			want: "bad request: invalid dockerfile path for service web: parent directory traversal is not allowed",
		},
		{
			name: "invalid image tag",
			yaml: "services:\n  web:\n    image: registry.example:5000/" + echoMarker + "\n",
			want: "bad request: invalid image tag for service web: image must be a Docker Hub name (optionally prefixed with docker.io/) or an owned DCM image tag",
		},
		{
			name: "healthcheck interval",
			yaml: web + "    healthcheck:\n      test: [CMD, " + echoMarker + "]\n      interval: 1s\n",
			want: "bad request: healthcheck interval must be at least 5s",
		},
		{
			name: "healthcheck timeout",
			yaml: web + "    healthcheck:\n      test: [CMD, " + echoMarker + "]\n      timeout: 1s\n",
			want: "bad request: healthcheck timeout must be at least 2s",
		},
		{
			name: "invalid mount path",
			yaml: "volumes:\n  data: {}\n" + web + "    volumes:\n      - data:/proc/" + echoMarker + "\n",
			want: "bad request: invalid mount path for service web: mounting a volume to /proc is not allowed",
		},
		{
			name: "relative bind mount host path",
			yaml: web + "    volumes:\n      - ./" + echoMarker + ":/data\n",
			want: "bad request: bind mounts are temporarily disabled in this PaaS",
		},
		// Exposure labels.
		{
			name: "domain prefix without internal port",
			yaml: web + "    labels:\n      dcm.domain_prefix: " + echoMarker + "\n",
			want: "bad request: service web must have BOTH dcm.domain_prefix and dcm.internal_port labels to be exposed",
		},
		{
			name: "invalid domain prefix",
			yaml: web + "    labels:\n      dcm.domain_prefix: " + echoMarker + "_x\n      dcm.internal_port: \"8080\"\n",
			want: "bad request: invalid domain prefix for service web: domain prefix must be a DNS label: lowercase letters, digits and hyphen only",
		},
		{
			name: "invalid internal port",
			yaml: web + "    labels:\n      dcm.domain_prefix: app\n      dcm.internal_port: " + echoMarker + "\n",
			want: "bad request: invalid dcm.internal_port for service web",
		},
		{
			name:        "forbidden domain prefix names the validated prefix",
			reserved:    []string{echoMarker},
			yaml:        web + "    labels:\n      dcm.domain_prefix: " + echoMarker + "\n      dcm.internal_port: \"8080\"\n",
			want:        `domain prefix "` + echoMarker + `" is forbidden`,
			echoAllowed: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			project := tt.project
			if project == "" {
				project = "proj"
			}
			_, err := compose.NewParser().ParseAndValidateWithBase(context.Background(), project, []byte(tt.yaml), tt.reserved, tt.baseDir)
			if got := apperrors.SafeMessage(err); !tt.echoAllowed && strings.Contains(got, echoMarker) {
				t.Fatalf("SafeMessage() = %q echoes rejected input %q", got, echoMarker)
			}
			requireRejection(t, err, apperrors.ErrBadRequest, tt.want)
		})
	}
}
