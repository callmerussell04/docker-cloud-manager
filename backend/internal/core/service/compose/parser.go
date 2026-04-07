package compose

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/compose-spec/compose-go/v2/loader"
	"github.com/compose-spec/compose-go/v2/types"
)

type Parser struct{}

func NewParser() *Parser {
	return &Parser{}
}

// ParseAndValidate принимает сырой YAML, проверяет его на безопасность и конвертирует в доменную модель
func (p *Parser) ParseAndValidate(ctx context.Context, projectName string, yamlContent []byte) (*domain.ComposeProject, error) {
	header := fmt.Sprintf("name: %s\n", projectName)
	fullContent := append([]byte(header), yamlContent...)
	project, err := loader.LoadWithContext(ctx, types.ConfigDetails{
		WorkingDir: ".",
		ConfigFiles: []types.ConfigFile{
			{
				Filename: "docker-compose.yml", // Фейковое имя для логов/ошибок
				Content:  fullContent,          // Передаем байты напрямую
			},
		},
		Environment: map[string]string{}, // Игнорируем внешние переменные окружения хоста
	})

	if err != nil {
		// Ошибка может возникнуть как на этапе парсинга YAML, так и на этапе валидации схемы Compose
		return nil, fmt.Errorf("%w: failed to parse compose file: %v", apperrors.ErrBadRequest, err)
	}

	// Строгая проверка безопасности
	if err := p.validateSecurity(project); err != nil {
		return nil, err
	}

	// Трансляция во внутренние структуры
	return p.translateToDomain(projectName, project)
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
					return fmt.Errorf("%w: absolute bind mounts are not allowed: %s", apperrors.ErrBadRequest, vol.Source)
				}
			}
		}
	}
	return nil
}

// translateToDomain конвертирует структуру compose-go в нашу бизнес-модель
func (p *Parser) translateToDomain(projectName string, project *types.Project) (*domain.ComposeProject, error) {
	result := &domain.ComposeProject{
		Name: projectName,
	}

	// 1. Собираем именованные тома
	volumeMap := make(map[string]string)
	for volName, volConfig := range project.Volumes {
		driver := "local"
		if volConfig.Driver != "" {
			driver = volConfig.Driver
		}

		result.Volumes = append(result.Volumes, domain.VolumeCreateParams{
			Name:       volName,
			Driver:     driver,
			DriverOpts: volConfig.DriverOpts,
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
		domainSrv := domain.ComposeService{
			Name:      srv.Name,
			EnvVars:   make(map[string]string),
			BuildArgs: make(map[string]string),
			DependsOn: make(map[string]string),
		}

		// Обработка сборки (build)
		if srv.Build != nil {
			domainSrv.BuildContext = srv.Build.Context
			if srv.Build.Dockerfile != "" {
				domainSrv.Dockerfile = srv.Build.Dockerfile
			} else {
				domainSrv.Dockerfile = "Dockerfile"
			}

			domainSrv.ImageTag = srv.Image
			if domainSrv.ImageTag == "" {
				domainSrv.ImageTag = fmt.Sprintf("%s_%s:latest", projectName, srv.Name)
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
			domainSrv.Restart = srv.Restart
		}

		// Healthcheck
		if srv.HealthCheck != nil && !srv.HealthCheck.Disable {
			domainSrv.Healthcheck = &domain.Healthcheck{
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
			if vol.Type == types.VolumeTypeVolume {
				if _, exists := volumeMap[vol.Source]; !exists {
					return fmt.Errorf("%w: service %s references undefined volume %s", apperrors.ErrBadRequest, srv.Name, vol.Source)
				}
				domainSrv.VolumeMounts = append(domainSrv.VolumeMounts, domain.VolumeMountParams{
					VolumeName: vol.Source,
					MountPath:  vol.Target,
					IsReadOnly: vol.ReadOnly,
				})
			} else if vol.Type == types.VolumeTypeBind {
				return fmt.Errorf("%w: bind mounts are temporarily disabled in this PaaS for path: %s", apperrors.ErrBadRequest, vol.Source)
			}
		}

		// Сохраняем зависимости (depends_on condition)
		for depName, depConfig := range srv.DependsOn {
			domainSrv.DependsOn[depName] = depConfig.Condition
		}

		// Парсим кастомные лейблы для экспоуза
		prefixStr, hasPrefix := srv.Labels["dcm.domain_prefix"]
		portStr, hasPort := srv.Labels["dcm.internal_port"]

		if hasPrefix || hasPort {
			// Если указан один лейбл, второй обязателен
			if !hasPrefix || !hasPort {
				return fmt.Errorf("%w: service %s must have BOTH dcm.domain_prefix and dcm.internal_port labels to be exposed", apperrors.ErrBadRequest, srv.Name)
			}
			if len(prefixStr) > 30 {
				return fmt.Errorf("%w: domain prefix for service %s must be 30 characters or less", apperrors.ErrBadRequest, srv.Name)
			}
			portInt, err := strconv.Atoi(portStr)
			if err != nil || portInt <= 0 {
				return fmt.Errorf("%w: invalid dcm.internal_port for service %s", apperrors.ErrBadRequest, srv.Name)
			}
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
