package buildinputs_test

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// dockerfileDirs lists the Dockerfile directories whose build inputs must be
// reproducible, each with the build context its Compose services use.
// The convention is documented in docs/deployment-single-vm.md.
var dockerfileDirs = []struct {
	dir          string
	buildContext string
}{
	{dir: "backend/dockerfiles", buildContext: "backend"},
	{dir: "deployment/registry", buildContext: "."},
}

var (
	digestPattern    = regexp.MustCompile(`@sha256:[0-9a-f]{64}$`)
	lockEntryPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._+-]*=[0-9][0-9A-Za-z._+~-]*-r[0-9]+$`)
	lockedApkPattern = regexp.MustCompile(`^xargs apk add --no-cache < (\S+)$`)
	// apkChangePattern matches apk add or upgrade with flags before the subcommand.
	apkChangePattern = regexp.MustCompile(`\bapk( -\S+)* (add|upgrade)\b`)
)

func TestDockerfilesPinBuildInputs(t *testing.T) {
	t.Parallel()

	root, err := filepath.Abs(filepath.Join("..", "..", ".."))
	require.NoError(t, err)

	for _, dockerfiles := range dockerfileDirs {
		paths, err := filepath.Glob(filepath.Join(root, dockerfiles.dir, "*.Dockerfile"))
		require.NoError(t, err)
		require.NotEmpty(t, paths, "no Dockerfiles found under %s", dockerfiles.dir)
		for _, path := range paths {
			for _, v := range checkDockerfile(t, path, filepath.Join(root, dockerfiles.buildContext)) {
				rel, _ := filepath.Rel(root, path)
				t.Errorf("%s: %s", rel, v)
			}
		}
	}
	if t.Failed() {
		t.Log("Pin base images by digest and install APK packages from a committed name=version lock; see docs/deployment-single-vm.md.")
	}
}

func TestCheckDockerfileReportsFloatingInputs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		dockerfile string
		lock       string
		want       []string
	}{
		{
			name: "pinned image and locked packages",
			dockerfile: "FROM alpine:3.24@sha256:" + strings.Repeat("a", 64) + " AS base\n" +
				"FROM base\n" +
				"COPY image/packages.lock /tmp/packages.lock\n" +
				"RUN xargs apk add --no-cache < /tmp/packages.lock \\\n    && rm /tmp/packages.lock\n",
			lock: "busybox=1.37.0-r31\nsocat=1.8.0.3-r0\n",
		},
		{
			name:       "floating tag",
			dockerfile: "FROM alpine:3.22\n",
			want:       []string{`base image "alpine:3.22" is not pinned by sha256 digest`},
		},
		{
			name:       "unversioned inline package",
			dockerfile: "FROM alpine@sha256:" + strings.Repeat("a", 64) + "\nRUN apk add --no-cache socat\n",
			want:       []string{`"apk add --no-cache socat" must install from a COPYed name=version lock`},
		},
		{
			name: "lock with an unversioned entry",
			dockerfile: "FROM alpine@sha256:" + strings.Repeat("a", 64) + "\n" +
				"COPY image/packages.lock /tmp/packages.lock\n" +
				"RUN xargs apk add --no-cache < /tmp/packages.lock\n",
			lock: "squid\n",
			want: []string{`image/packages.lock: entry "squid" is not name=version`},
		},
		{
			name:       "flag before add",
			dockerfile: "FROM scratch\nRUN apk --no-cache add socat\n",
			want:       []string{`"apk --no-cache add socat" must install from a COPYed name=version lock`},
		},
		{
			name:       "upgrade past the lock",
			dockerfile: "FROM scratch\nRUN apk -U upgrade\n",
			want:       []string{`"apk -U upgrade" must install from a COPYed name=version lock`},
		},
		{
			name:       "exec form",
			dockerfile: "FROM scratch\nRUN [\"apk\", \"add\", \"socat\"]\n",
			want:       []string{`"apk add socat" must install from a COPYed name=version lock`},
		},
		{
			name:       "blank line inside a continuation",
			dockerfile: "FROM scratch\nRUN true \\\n\n    && apk add socat\n",
			want:       []string{`"apk add socat" must install from a COPYed name=version lock`},
		},
		{
			name: "lock copied in another stage",
			dockerfile: "FROM scratch AS Locks\nCOPY image/packages.lock /tmp/packages.lock\n" +
				"FROM locks\nFROM scratch\nRUN xargs apk add --no-cache < /tmp/packages.lock\n",
			lock: "socat=1.8.1.3-r0\n",
			want: []string{`"xargs apk add --no-cache < /tmp/packages.lock" must install from a COPYed name=version lock`},
		},
		{
			name: "missing lock",
			dockerfile: "FROM scratch\nCOPY image/missing.lock /tmp/packages.lock\n" +
				"RUN xargs apk add --no-cache < /tmp/packages.lock\n",
			want: []string{"image/missing.lock: lock is not readable"},
		},
		{
			name: "copy flags",
			dockerfile: "FROM scratch\nCOPY --chown=0:0 image/packages.lock /tmp/packages.lock\n" +
				"RUN xargs apk add --no-cache < /tmp/packages.lock\n",
			lock: "socat=1.8.1.3-r0\n",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			buildContext := t.TempDir()
			path := filepath.Join(buildContext, "image", "test.Dockerfile")
			require.NoError(t, os.MkdirAll(filepath.Dir(path), 0o755))
			require.NoError(t, os.WriteFile(path, []byte(tt.dockerfile), 0o600))
			if tt.lock != "" {
				require.NoError(t, os.WriteFile(filepath.Join(buildContext, "image", "packages.lock"), []byte(tt.lock), 0o600))
			}

			require.Equal(t, tt.want, checkDockerfile(t, path, buildContext))
		})
	}
}

// checkDockerfile returns every floating build input in the Dockerfile at path.
// COPY sources resolve against buildContext, the image's build context. Every
// instruction other than FROM and COPY is scanned for APK installs, so heredoc
// bodies and exec-form RUN arguments are not skipped.
func checkDockerfile(t *testing.T, path, buildContext string) []string {
	t.Helper()

	var violations []string
	stages := map[string]bool{}
	copies := map[string]string{}
	for _, instruction := range readInstructions(t, path) {
		keyword, args, _ := strings.Cut(instruction, " ")
		fields := strings.Fields(args)
		switch strings.ToUpper(keyword) {
		case "FROM":
			ref := fromImage(fields)
			if ref != "scratch" && !stages[strings.ToLower(ref)] && !digestPattern.MatchString(ref) {
				violations = append(violations, fmt.Sprintf("base image %q is not pinned by sha256 digest", ref))
			}
			if n := len(fields); n >= 3 && strings.EqualFold(fields[n-2], "AS") {
				stages[strings.ToLower(fields[n-1])] = true
			}
			// A lock copied in an earlier stage is not on this stage's filesystem.
			copies = map[string]string{}
		case "COPY":
			if sources := withoutFlags(fields); len(sources) == 2 {
				copies[sources[1]] = sources[0]
			}
		default:
			violations = append(violations, checkApkInstalls(args, copies, buildContext)...)
		}
	}
	return violations
}

func checkApkInstalls(run string, copies map[string]string, buildContext string) []string {
	// Exec-form JSON arguments read the same as their shell form once unquoted.
	run = strings.NewReplacer(`[`, " ", `]`, " ", `"`, " ", `,`, " ").Replace(run)
	var violations []string
	for _, command := range strings.Split(run, "&&") {
		command = strings.Join(strings.Fields(command), " ")
		if !apkChangePattern.MatchString(command) {
			continue
		}
		match := lockedApkPattern.FindStringSubmatch(command)
		source, copied := "", false
		if match != nil {
			source, copied = copies[match[1]]
		}
		if !copied {
			violations = append(violations, fmt.Sprintf("%q must install from a COPYed name=version lock", command))
			continue
		}
		violations = append(violations, checkLock(filepath.Join(buildContext, source), source)...)
	}
	return violations
}

func checkLock(path, name string) []string {
	data, err := os.ReadFile(path)
	if err != nil {
		return []string{name + ": lock is not readable"}
	}
	var violations []string
	entries := 0
	for _, entry := range strings.Split(string(data), "\n") {
		entry = strings.TrimSpace(entry)
		if entry == "" {
			continue
		}
		entries++
		if !lockEntryPattern.MatchString(entry) {
			violations = append(violations, fmt.Sprintf("%s: entry %q is not name=version", name, entry))
		}
	}
	if entries == 0 {
		violations = append(violations, name+": lock is empty")
	}
	return violations
}

// fromImage returns the image reference of a FROM instruction, skipping flags.
func fromImage(fields []string) string {
	if refs := withoutFlags(fields); len(refs) > 0 {
		return refs[0]
	}
	return ""
}

func withoutFlags(fields []string) []string {
	var args []string
	for _, field := range fields {
		if !strings.HasPrefix(field, "--") {
			args = append(args, field)
		}
	}
	return args
}

// readInstructions joins backslash continuations and drops comments. Like
// Docker, it keeps a continuation open across blank and comment lines.
func readInstructions(t *testing.T, path string) []string {
	t.Helper()

	file, err := os.Open(path)
	require.NoError(t, err)
	defer func() { _ = file.Close() }() // read-only close

	var instructions []string
	var current strings.Builder
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if body, continued := strings.CutSuffix(line, "\\"); continued {
			current.WriteString(body + " ")
			continue
		}
		current.WriteString(line)
		instructions = append(instructions, strings.Join(strings.Fields(current.String()), " "))
		current.Reset()
	}
	require.NoError(t, scanner.Err())
	return instructions
}
