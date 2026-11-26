package compose

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/validation"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
)

type Parser struct{}

var supportedComposeServiceKeys = map[string]struct{}{
	"build":           {},
	"command":         {},
	"depends_on":      {},
	"entrypoint":      {},
	"environment":     {},
	"healthcheck":     {},
	"image":           {},
	"deploy":          {},
	"labels":          {},
	"mem_reservation": {},
	"name":            {},
	"network_mode":    {},
	"pid":             {},
	"privileged":      {},
	"profiles":        {},
	"restart":         {},
	"volumes":         {},
}

var supportedComposeBuildKeys = map[string]struct{}{
	"args":       {},
	"context":    {},
	"dockerfile": {},
}

// supportedComposeDeployKeys keeps only the reservations of deploy: DCM reads
// a service's memory and CPU reservation and derives its limits itself.
var supportedComposeDeployKeys = map[string]struct{}{"resources": {}}

var supportedComposeResourcesKeys = map[string]struct{}{"reservations": {}}

var supportedComposeReservationKeys = map[string]struct{}{
	"cpus":   {},
	"memory": {},
}

var supportedComposeVolumeKeys = map[string]struct{}{
	"external": {},
	"name":     {},
}

func NewParser() *Parser {
	return &Parser{}
}

func (p *Parser) ParseAndValidateWithBaseAndBlocked(ctx context.Context, projectName string, yamlContent []byte, reservedDomainPrefixes []string, blockedDomainPrefixPatterns []string, baseDir string) (*model.ComposeProject, error) {
	if err := validation.ProjectName(projectName); err != nil {
		return nil, fmt.Errorf("%w: %v", apperrors.ErrBadRequest, err)
	}
	baseDir, err := cleanComposeBaseDir(baseDir)
	if err != nil {
		return nil, fmt.Errorf("%w: invalid compose file path: %v", apperrors.ErrBadRequest, err)
	}

	project, err := p.loadComposeProject(ctx, projectName, yamlContent)
	if err != nil {
		return nil, composeLoadError(err)
	}

	// Строгая проверка безопасности
	if err := p.validateSecurity(project); err != nil {
		return nil, err
	}

	// Трансляция во внутренние структуры
	return p.translateToDomain(projectName, project, reservedDomainPrefixes, blockedDomainPrefixPatterns, baseDir)
}

func (p *Parser) loadComposeProject(ctx context.Context, projectName string, yamlContent []byte) (*types.Project, error) {
	config := types.ConfigDetails{
		WorkingDir: ".",
		ConfigFiles: []types.ConfigFile{
			{
				Filename: "docker-compose.yml", // Фейковое имя для логов/ошибок
				Content:  yamlContent,          // Передаем байты напрямую
			},
		},
		Environment: map[string]string{
			"COMPOSE_PROJECT_NAME": projectName,
		}, // Игнорируем внешние переменные окружения хоста
	}
	options := []func(*loader.Options){
		func(opts *loader.Options) {
			opts.SkipValidation = true
			opts.SkipExtends = true
			opts.SkipInclude = true
			opts.SetProjectName(projectName, true)
		},
	}

	composeModel, err := recoverComposeGo(func() (map[string]any, error) {
		return loader.LoadModelWithContext(ctx, config, options...)
	})
	if err != nil {
		return nil, err
	}
	sanitizeComposeModel(composeModel)

	return recoverComposeGo(func() (*types.Project, error) {
		return loader.ModelToProject(composeModel, loader.ToOptions(&config, options), config)
	})
}

// composeLoadError replaces a compose-go load failure with a fixed bad-request
// message. compose-go errors quote the uploaded file, including interpolated
// environment values, and the message reaches the uploader, the stored
// deployment error and the job log, so the cause is dropped, not wrapped.
//
// A YAML decoding failure (an error in the chain whose message starts with
// "yaml: ") keeps its position when the message carries a reliable one. The
// YAML library does not export its syntax error types, and its exported
// LoadError holds only the document's line for a nested construct error, so
// the position is read from the library's own message. The patterns are
// anchored to the library's fixed wording and capture digits only, so they
// cannot carry file content.
func composeLoadError(err error) error {
	for ; err != nil; err = errors.Unwrap(err) {
		message := err.Error()
		if !strings.HasPrefix(message, "yaml: ") {
			continue
		}
		if position, ok := yamlErrorPosition(message); ok {
			return fmt.Errorf("%w: compose file is not valid YAML (%s)", apperrors.ErrBadRequest, position)
		}
		return fmt.Errorf("%w: compose file is not valid YAML", apperrors.ErrBadRequest)
	}
	return fmt.Errorf("%w: compose file is not a valid Compose project", apperrors.ErrBadRequest)
}

var (
	// yamlSyntaxPosition matches a go.yaml.in/yaml parser or scanner error.
	// An optional lowercase context names the enclosing construct, and a
	// second mark, when present, is the problem itself:
	//   "yaml: line 5, column 22: ..."
	//   "yaml: while scanning for the next token at line 5, column 22: ..."
	//   "yaml: while scanning a quoted scalar at line 5, column 14: line 6: ..."
	yamlSyntaxPosition = regexp.MustCompile(`^yaml: (?:[a-z ]+ at )?line (\d+)(?:, column (\d+))?: (?:line (\d+)(?:, column (\d+))?: )?`)
	// yamlConstructPosition matches a go.yaml.in/yaml construct error with
	// nested line prefixes: "yaml: construct errors:\n  line 1: line 4: ...".
	// The outermost is the document and the innermost the offending node. A
	// single prefix is only the document's line, so it gives no position.
	yamlConstructPosition = regexp.MustCompile(`^yaml: construct errors:\n  line \d+: (?:line \d+: )*line (\d+): `)
)

func yamlErrorPosition(message string) (string, bool) {
	if match := yamlConstructPosition.FindStringSubmatch(message); match != nil {
		return "line " + match[1], true
	}
	match := yamlSyntaxPosition.FindStringSubmatch(message)
	if match == nil {
		return "", false
	}
	line, column := match[1], match[2]
	if match[3] != "" {
		line, column = match[3], match[4]
	}
	if column == "" {
		return "line " + line, true
	}
	return "line " + line + ", column " + column, true
}

// recoverComposeGo runs one compose-go call. compose-go type-asserts sections
// it expects to be maps and panics on untrusted input such as `services:` with
// no value; that is reported as an invalid Compose project (see
// composeLoadError) instead of crashing the request or deployment worker.
// Panics in DCM's own code are not recovered.
func recoverComposeGo[T any](call func() (T, error)) (result T, err error) {
	defer func() {
		if recover() != nil {
			var zero T
			result, err = zero, errors.New("compose file is malformed")
		}
	}()
	return call()
}

func sanitizeComposeModel(composeModel map[string]any) {
	for key := range composeModel {
		switch key {
		case "name", "services", "volumes":
		default:
			delete(composeModel, key)
		}
	}

	services, ok := asStringMap(composeModel["services"])
	if ok {
		sanitizeComposeServices(services)
	}

	volumes, ok := asStringMap(composeModel["volumes"])
	if ok {
		sanitizeComposeVolumes(volumes)
	}
}

func sanitizeComposeServices(services map[string]any) {
	for _, rawService := range services {
		service, ok := asStringMap(rawService)
		if !ok {
			continue
		}
		pruneStringMap(service, supportedComposeServiceKeys)
		sanitizeComposeServiceBuild(service)
		sanitizeComposeServiceDependsOn(service)
		sanitizeComposeServiceDeploy(service)
	}
}

// sanitizeComposeServiceDeploy prunes deploy down to
// resources.reservations.memory and resources.reservations.cpus. It then
// settles the memory precedence that compose-go would otherwise refuse as
// distinct values: a deploy memory reservation drops mem_reservation, and
// without one mem_reservation moves into the deploy reservations.
func sanitizeComposeServiceDeploy(service map[string]any) {
	deploy, ok := asStringMap(service["deploy"])
	if !ok {
		return
	}
	pruneStringMap(deploy, supportedComposeDeployKeys)
	resources, ok := asStringMap(deploy["resources"])
	if !ok {
		return
	}
	pruneStringMap(resources, supportedComposeResourcesKeys)
	reservations, ok := asStringMap(resources["reservations"])
	if !ok {
		return
	}
	pruneStringMap(reservations, supportedComposeReservationKeys)
	if memReservation, ok := service["mem_reservation"]; ok {
		if memory, ok := reservations["memory"]; !ok || memory == nil {
			reservations["memory"] = memReservation
		}
		delete(service, "mem_reservation")
	}
}

func sanitizeComposeServiceBuild(service map[string]any) {
	build, ok := asStringMap(service["build"])
	if !ok {
		return
	}
	pruneStringMap(build, supportedComposeBuildKeys)
}

func sanitizeComposeServiceDependsOn(service map[string]any) {
	dependencies, ok := asStringMap(service["depends_on"])
	if !ok {
		return
	}
	for _, rawDependency := range dependencies {
		dependency, ok := asStringMap(rawDependency)
		if !ok {
			continue
		}
		delete(dependency, "restart")
		if condition, ok := dependency["condition"].(string); ok && condition != "" && !model.IsValidComposeDependencyCondition(condition) {
			dependency["condition"] = model.ComposeDependencyConditionStarted
		}
	}
}

func sanitizeComposeVolumes(volumes map[string]any) {
	for _, rawVolume := range volumes {
		volume, ok := asStringMap(rawVolume)
		if !ok {
			continue
		}
		pruneStringMap(volume, supportedComposeVolumeKeys)
	}
}

func pruneStringMap(values map[string]any, allowed map[string]struct{}) {
	for key := range values {
		if _, ok := allowed[key]; !ok {
			delete(values, key)
		}
	}
}

func asStringMap(value any) (map[string]any, bool) {
	values, ok := value.(map[string]any)
	return values, ok
}

// validateSecurity блокирует опасные директивы, чтобы защитить хост-систему
func (p *Parser) validateSecurity(project *types.Project) error {
	// Пользователям запрещено создавать свои сети. Они всегда изолированы в рамках одной сети владельца.
	//delete(project.Networks, "default")
	//if len(project.Networks) > 0 {
	//	return fmt.Errorf("%w: custom networks are not allowed in this PaaS", apperrors.ErrBadRequest)
	//}
	// или
	project.Networks = nil

	for _, service := range project.Services {
		// Запрещаем режим хостовой сети (доступ ко всем портам сервера)
		if service.NetworkMode == "host" {
			return fmt.Errorf("%w: network_mode: host is not allowed", apperrors.ErrBadRequest)
		}
		// Запрещаем запуск контейнеров с повышенными привилегиями
		if service.Privileged {
			return fmt.Errorf("%w: privileged mode is not allowed", apperrors.ErrBadRequest)
		}
		// Запрещаем делить пространство процессов с хостом
		if service.Pid == "host" {
			return fmt.Errorf("%w: pid: host is not allowed", apperrors.ErrBadRequest)
		}

		for _, vol := range service.Volumes {
			// Если том монтируется как бинд (путь на хосте)
			if vol.Type == types.VolumeTypeBind {
				// Категорически запрещаем абсолютные пути (например, /var/run/docker.sock или /etc)
				if strings.HasPrefix(vol.Source, "/") {
					return fmt.Errorf("%w: absolute bind mounts are not allowed", apperrors.ErrBadRequest)
				}
			}
		}
	}
	return nil
}

// translateToDomain конвертирует структуру compose-go в нашу бизнес-модель
func (p *Parser) translateToDomain(projectName string, project *types.Project, reservedDomainPrefixes []string, blockedDomainPrefixPatterns []string, baseDir string) (*model.ComposeProject, error) {
	result := &model.ComposeProject{
		Name: projectName,
	}
	domainPrefixServices := make(map[string]string)

	// 1. Собираем именованные тома
	volumeMap := make(map[string]string)
	for volName, volConfig := range project.Volumes {
		if err := validation.ResourceName(volName); err != nil {
			return nil, fmt.Errorf("%w: volume has invalid name: %v", apperrors.ErrBadRequest, err)
		}
		displayName := volName
		if bool(volConfig.External) {
			displayName = volConfig.Name
			if displayName == "" {
				displayName = volName
			}
			if err := validation.ResourceName(displayName); err != nil {
				return nil, fmt.Errorf("%w: external volume %s has invalid name: %v", apperrors.ErrBadRequest, volName, err)
			}
			result.Volumes = append(result.Volumes, model.ComposeVolume{
				Alias:    volName,
				Name:     displayName,
				External: true,
			})
			volumeMap[volName] = volName
			continue
		}

		if volConfig.Driver != "" {
			return nil, fmt.Errorf("%w: custom volume drivers are not allowed", apperrors.ErrBadRequest)
		}
		if len(volConfig.DriverOpts) > 0 {
			return nil, fmt.Errorf("%w: volume driver options are not allowed", apperrors.ErrBadRequest)
		}

		result.Volumes = append(result.Volumes, model.ComposeVolume{
			Alias: volName,
			Name:  fmt.Sprintf("%s_%s", projectName, displayName),
		})
		volumeMap[volName] = volName
	}

	// 2. Обрабатываем каждый сервис, используя встроенный топологический обход compose-go
	// Получаем список всех имен сервисов в проекте
	allServiceNames := make([]string, 0, len(project.Services))
	for name := range project.Services {
		allServiceNames = append(allServiceNames, name)
	}

	// ForEachService гарантирует вызов колбэка в правильном порядке (с учетом depends_on)
	err := project.ForEachService(allServiceNames, func(serviceName string, srv *types.ServiceConfig) error {
		if err := validation.ResourceName(srv.Name); err != nil {
			return fmt.Errorf("%w: service has invalid name: %v", apperrors.ErrBadRequest, err)
		}

		domainSrv := model.ComposeService{
			Name:      srv.Name,
			EnvVars:   make(map[string]string),
			BuildArgs: make(map[string]string),
		}

		// Обработка сборки (build)
		if srv.Build != nil {
			buildContext, err := normalizeComposeBuildContext(baseDir, srv.Build.Context)
			if err != nil {
				return fmt.Errorf("%w: invalid build context for service %s: %v", apperrors.ErrBadRequest, srv.Name, err)
			}

			domainSrv.BuildContext = buildContext
			if srv.Build.Dockerfile != "" {
				if err := validateRelativeComposePath(srv.Build.Dockerfile); err != nil {
					return fmt.Errorf("%w: invalid dockerfile path for service %s: %v", apperrors.ErrBadRequest, srv.Name, err)
				}
				domainSrv.Dockerfile = srv.Build.Dockerfile
			} else {
				domainSrv.Dockerfile = "Dockerfile"
			}

			domainSrv.ImageTag = srv.Image
			if domainSrv.ImageTag == "" {
				domainSrv.ImageTag = strings.ToLower(fmt.Sprintf("%s_%s:latest", projectName, srv.Name))
			}
			// Сохраняем Build Args
			for k, v := range srv.Build.Args {
				if v != nil {
					domainSrv.BuildArgs[k] = *v
				}
			}
		} else {
			if srv.Image == "" {
				return fmt.Errorf("%w: service %s must have either image or build", apperrors.ErrBadRequest, srv.Name)
			}
			domainSrv.ImageTag = srv.Image
		}
		if err := validation.ImageTag(domainSrv.ImageTag); err != nil {
			return fmt.Errorf("%w: invalid image tag for service %s: %v", apperrors.ErrBadRequest, srv.Name, err)
		}

		memoryMB, millicores, err := serviceReservation(srv)
		if err != nil {
			return err
		}
		domainSrv.MemoryReservationMB, domainSrv.CPUReservationMillicores = memoryMB, millicores

		// Копируем переменные окружения
		for key, val := range srv.Environment {
			if val != nil {
				domainSrv.EnvVars[key] = *val
			}
		}

		// Команды и перезапуск
		if len(srv.Command) > 0 {
			domainSrv.Command = srv.Command
		}
		if len(srv.Entrypoint) > 0 {
			domainSrv.Entrypoint = srv.Entrypoint
		}
		if srv.Restart != "" {
			if isSupportedRestartPolicy(srv.Restart) {
				domainSrv.Restart = srv.Restart
			}
		}

		// Healthcheck
		if srv.HealthCheck != nil && !srv.HealthCheck.Disable {
			domainSrv.Healthcheck = &model.Healthcheck{
				Test: srv.HealthCheck.Test,
			}

			if srv.HealthCheck.Retries != nil {
				domainSrv.Healthcheck.Retries = int(*srv.HealthCheck.Retries)
			}

			if srv.HealthCheck.Interval != nil {
				domainSrv.Healthcheck.Interval = time.Duration(*srv.HealthCheck.Interval)
				if domainSrv.Healthcheck.Interval < 5*time.Second {
					return fmt.Errorf("%w: healthcheck interval must be at least 5s", apperrors.ErrBadRequest)
				}
			}

			if srv.HealthCheck.Timeout != nil {
				domainSrv.Healthcheck.Timeout = time.Duration(*srv.HealthCheck.Timeout)
				if domainSrv.Healthcheck.Timeout < 2*time.Second {
					return fmt.Errorf("%w: healthcheck timeout must be at least 2s", apperrors.ErrBadRequest)
				}
			}

			if srv.HealthCheck.StartPeriod != nil {
				domainSrv.Healthcheck.StartPeriod = time.Duration(*srv.HealthCheck.StartPeriod)
			}
		}

		// Копируем маунты томов
		for _, vol := range srv.Volumes {
			switch vol.Type {
			case types.VolumeTypeVolume:
				if _, exists := volumeMap[vol.Source]; !exists {
					return fmt.Errorf("%w: service %s references an undefined volume", apperrors.ErrBadRequest, srv.Name)
				}
				if err := validation.MountPath(vol.Target); err != nil {
					return fmt.Errorf("%w: invalid mount path for service %s: %v", apperrors.ErrBadRequest, srv.Name, err)
				}
				domainSrv.VolumeMounts = append(domainSrv.VolumeMounts, model.VolumeMountParams{
					VolumeName: vol.Source,
					MountPath:  vol.Target,
					IsReadOnly: vol.ReadOnly,
				})
			case types.VolumeTypeBind:
				return fmt.Errorf("%w: bind mounts are temporarily disabled in this PaaS", apperrors.ErrBadRequest)
			}
		}

		// Сохраняем зависимости (depends_on condition)
		for depName, depConfig := range srv.DependsOn {
			condition := depConfig.Condition
			if condition == "" || !model.IsValidComposeDependencyCondition(condition) {
				condition = model.ComposeDependencyConditionStarted
			}
			domainSrv.DependsOn = append(domainSrv.DependsOn, model.ComposeDependency{
				ServiceName: depName,
				Condition:   condition,
				Optional:    !depConfig.Required,
			})
		}
		sort.Slice(domainSrv.DependsOn, func(i, j int) bool {
			return domainSrv.DependsOn[i].ServiceName < domainSrv.DependsOn[j].ServiceName
		})

		// Парсим кастомные лейблы для экспоуза
		prefixStr, hasPrefix := srv.Labels["dcm.domain_prefix"]
		portStr, hasPort := srv.Labels["dcm.internal_port"]

		if hasPrefix || hasPort {
			// Если указан один лейбл, второй обязателен
			if !hasPrefix || !hasPort {
				return fmt.Errorf("%w: service %s must have BOTH dcm.domain_prefix and dcm.internal_port labels to be exposed", apperrors.ErrBadRequest, srv.Name)
			}
			if err := validation.DomainPrefix(prefixStr); err != nil {
				return fmt.Errorf("%w: invalid domain prefix for service %s: %v", apperrors.ErrBadRequest, srv.Name, err)
			}
			if validation.ForbiddenDomainPrefix(prefixStr, reservedDomainPrefixes, blockedDomainPrefixPatterns) {
				return apperrors.New(apperrors.ErrBadRequest, fmt.Sprintf("domain prefix %q is forbidden", prefixStr))
			}
			portInt, err := strconv.Atoi(portStr)
			if err != nil || portInt <= 0 || portInt > 65535 {
				return fmt.Errorf("%w: invalid dcm.internal_port for service %s", apperrors.ErrBadRequest, srv.Name)
			}
			if existingService, exists := domainPrefixServices[prefixStr]; exists {
				return apperrors.New(apperrors.ErrAlreadyExists, fmt.Sprintf("subdomain %s is used by both services %s and %s", prefixStr, existingService, srv.Name))
			}
			domainPrefixServices[prefixStr] = srv.Name
			domainSrv.DomainPrefix = prefixStr
			domainSrv.InternalPort = portInt
		}

		result.Services = append(result.Services, domainSrv)
		return nil
	})

	if err != nil {
		return nil, err
	}

	return result, nil
}

// serviceReservation returns a service's memory reservation in whole MB,
// rounded up, and its CPU reservation in millicores, rounded to the nearest.
// Memory comes from deploy.resources.reservations.memory, else
// mem_reservation; CPU from deploy.resources.reservations.cpus. Zero means no
// figure. A figure outside the container reservation bounds is a bad request
// that names the service and not the figure.
func serviceReservation(srv *types.ServiceConfig) (memoryMB, millicores int64, err error) {
	memoryBytes := int64(srv.MemReservation)
	var cores float64
	if srv.Deploy != nil && srv.Deploy.Resources.Reservations != nil {
		reservations := srv.Deploy.Resources.Reservations
		if reservations.MemoryBytes != 0 {
			memoryBytes = int64(reservations.MemoryBytes)
		}
		cores = float64(reservations.NanoCPUs)
	}
	if memoryBytes != 0 {
		if memoryBytes < config.MinMemoryReservationBytes || memoryBytes > config.MaxMemoryReservationBytes {
			return 0, 0, fmt.Errorf("%w: memory reservation for service %s must be %s", apperrors.ErrBadRequest, srv.Name, config.MemoryReservationRange())
		}
		memoryMB = model.BytesToMBRoundedUp(memoryBytes)
	}
	if cores != 0 {
		// compose-go keeps cpus as float32, so 0.01 arrives as 9.9999998
		// millicores: snap to a thousandth of a millicore first. The bounds
		// then hold before rounding to whole millicores, so a figure just
		// below the minimum is not rounded into it. The comparison is false
		// for NaN, so NaN is rejected too.
		scaled := math.Round(cores*1000*1000) / 1000
		if !(scaled >= float64(config.MinCPUReservationMillicores) && scaled <= float64(config.MaxCPUReservationMillicores)) {
			return 0, 0, fmt.Errorf("%w: cpu reservation for service %s must be %s", apperrors.ErrBadRequest, srv.Name, config.CPUReservationRangeInCores())
		}
		millicores = int64(math.Round(scaled))
	}
	return memoryMB, millicores, nil
}

func isSupportedRestartPolicy(policy string) bool {
	switch policy {
	case "no", "on-failure":
		return true
	}
	return false
}

func validateRelativeComposePath(path string) error {
	if path == "" || path == "." {
		return nil
	}
	if filepath.IsAbs(path) {
		return fmt.Errorf("absolute paths are not allowed")
	}
	clean := filepath.Clean(path)
	if clean == ".." || strings.HasPrefix(clean, "../") || strings.Contains(clean, "\x00") {
		return fmt.Errorf("parent directory traversal is not allowed")
	}
	return nil
}

func cleanComposeBaseDir(path string) (string, error) {
	if path == "" || path == "." {
		return "", nil
	}
	if err := validateRelativeComposePath(path); err != nil {
		return "", err
	}
	clean := filepath.ToSlash(filepath.Clean(path))
	if clean == "." {
		return "", nil
	}
	return clean, nil
}

func normalizeComposeBuildContext(baseDir, contextPath string) (string, error) {
	if err := validateRelativeComposePath(contextPath); err != nil {
		return "", err
	}
	if baseDir == "" {
		if contextPath == "" {
			return ".", nil
		}
		return filepath.ToSlash(filepath.Clean(contextPath)), nil
	}
	if contextPath == "" || contextPath == "." {
		return baseDir, nil
	}
	joined := filepath.ToSlash(filepath.Clean(filepath.Join(baseDir, contextPath)))
	if joined == ".." || strings.HasPrefix(joined, "../") {
		return "", fmt.Errorf("build context escapes repository root")
	}
	return joined, nil
}
