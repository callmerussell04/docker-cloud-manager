package compose

import (
	"archive/zip"
	"bytes"
	"context"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/config"
	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/accessscope"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
)

type ProjectRepository interface {
	Save(ctx context.Context, p model.Project) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, errMsg *string) error
	SaveServiceGraph(ctx context.Context, projectID uuid.UUID, services []model.ProjectServiceNode) error
}

type BuildRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (model.Build, error)
}

type VolumeService interface {
	Create(ctx context.Context, params model.VolumeCreateParams) (uuid.UUID, error)
	Delete(ctx context.Context, volumeID uuid.UUID) error
}

type ContainerService interface {
	Create(ctx context.Context, params model.ContainerCreateParams) (uuid.UUID, error)
	Start(ctx context.Context, containerID uuid.UUID) error
	Delete(ctx context.Context, containerID uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Container, error)
}

type ComposeDockerAPI interface {
	InspectContainer(ctx context.Context, dockerID string) (model.ContainerInspection, error)
}

type ConfigProvider interface {
	Get() config.SystemConfig
}

type BuilderClient interface {
	TriggerBuild(ctx context.Context, srv model.ComposeService, archiveBytes []byte) (uuid.UUID, error)
	CancelBuild(ctx context.Context, buildID uuid.UUID) error
}

type Orchestrator struct {
	parentCtx     context.Context
	parser        *Parser
	projectRepo   ProjectRepository
	buildRepo     BuildRepository
	volumeService VolumeService
	contService   ContainerService
	dockerAPI     ComposeDockerAPI
	cfg           ConfigProvider
	builderClient BuilderClient
	logger        *slog.Logger
}

const imageBuildsUnavailableMessage = "Image builds are currently unavailable. Use Docker Hub images."

func NewOrchestrator(
	parentCtx context.Context,
	projectRepo ProjectRepository,
	buildRepo BuildRepository,
	volumeService VolumeService,
	contService ContainerService,
	dockerAPI ComposeDockerAPI,
	cfg ConfigProvider,
	objectStore ObjectStorage,
	builds BuildJobCreator,
	logger *slog.Logger,
) *Orchestrator {
	if parentCtx == nil {
		parentCtx = context.Background()
	}
	return &Orchestrator{
		parentCtx:     parentCtx,
		parser:        NewParser(),
		projectRepo:   projectRepo,
		buildRepo:     buildRepo,
		volumeService: volumeService,
		contService:   contService,
		dockerAPI:     dockerAPI,
		cfg:           cfg,
		builderClient: NewLocalBuilderClient(objectStore, builds),
		logger:        logging.WithComponent(logger, "compose_orchestrator"),
	}
}

// StartDeployment - Точка входа. Создает проект и запускает горутину оркестрации
func (o *Orchestrator) StartDeployment(ctx context.Context, projectName string, archiveBytes []byte) (uuid.UUID, error) {
	scope, err := accessscope.RequireScope(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	ownerID, err := accessscope.RequireUserOwner(ctx)
	if err != nil {
		return uuid.Nil, err
	}
	cfg := o.cfg.Get()
	if !cfg.ImageBuildsEnabled {
		parsedProject, err := o.parseComposeProject(ctx, projectName, archiveBytes, cfg.ReservedDomainPrefixes)
		if err != nil {
			return uuid.Nil, err
		}
		if composeRequiresBuild(parsedProject) {
			return uuid.Nil, apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage)
		}
	}
	projectID := uuid.New()
	p := model.Project{
		ID:      projectID,
		OwnerID: ownerID,
		Name:    projectName,
		Status:  model.ProjectStatusBuilding,
	}

	if err := o.projectRepo.Save(ctx, p); err != nil {
		return uuid.Nil, err
	}

	// Запускаем асинхронный процесс (Этап 3 и 4)
	requestID := logging.RequestIDFromContext(ctx)
	go o.runPipeline(projectID, scope, projectName, archiveBytes, requestID)

	return projectID, nil
}

func (o *Orchestrator) parseComposeProject(ctx context.Context, projectName string, archiveBytes []byte, reservedDomainPrefixes []string) (*model.ComposeProject, error) {
	composeYaml, err := extractComposeFile(archiveBytes)
	if err != nil {
		return nil, err
	}
	return o.parser.ParseAndValidate(ctx, projectName, composeYaml, reservedDomainPrefixes)
}

func composeRequiresBuild(project *model.ComposeProject) bool {
	if project == nil {
		return false
	}
	for _, srv := range project.Services {
		if srv.BuildContext != "" {
			return true
		}
	}
	return false
}

func (o *Orchestrator) runPipeline(projectID uuid.UUID, scope accessscope.Scope, projectName string, archiveBytes []byte, requestID string) {
	baseCtx, cancel := context.WithTimeout(o.parentCtx, time.Duration(o.cfg.Get().ComposePipelineTimeoutMinutes)*time.Minute)
	defer cancel()
	ctx := logging.ContextWithRequestID(baseCtx, requestID)
	ctx = accessscope.WithScope(ctx, scope)
	ownerID := scope.UserID
	logger := o.logger.With("project_id", projectID, "owner_id", ownerID)
	logger.InfoContext(ctx, "compose deployment pipeline started", "project_name", projectName)

	// Вспомогательная функция для обновления статуса при ошибке
	failProject := func(err error) {
		errMsg := apperrors.SafeMessage(err)
		logger.ErrorContext(ctx, "compose deployment failed", "error", err)
		_ = o.projectRepo.UpdateStatus(ctx, projectID, model.ProjectStatusFailed, &errMsg)
	}

	// 1. Извлечение, парсинг и валидация YAML
	parsedProject, err := o.parseComposeProject(ctx, projectName, archiveBytes, o.cfg.Get().ReservedDomainPrefixes)
	if err != nil {
		failProject(err)
		return
	}
	// 2. Оркестрация сборок (Kaniko)
	var buildIDs []uuid.UUID
	for _, srv := range parsedProject.Services {
		if srv.BuildContext != "" { // Нужна сборка
			if !o.cfg.Get().ImageBuildsEnabled {
				o.cancelBuilds(ctx, buildIDs)
				failProject(apperrors.New(apperrors.ErrUnavailable, imageBuildsUnavailableMessage))
				return
			}
			if status, ok := o.failedBuildStatus(ctx, buildIDs); ok {
				o.cancelBuilds(ctx, buildIDs)
				failProject(apperrors.New(apperrors.ErrConflict, fmt.Sprintf("build failed with status: %s", status)))
				return
			}

			buildID, err := o.builderClient.TriggerBuild(ctx, srv, archiveBytes)
			if err != nil {
				o.cancelBuilds(ctx, buildIDs)
				failProject(fmt.Errorf("failed to trigger build for service %s: %w", srv.Name, err))
				return
			}
			buildIDs = append(buildIDs, buildID)
		}
	}

	// Ожидание завершения всех сборок
	if len(buildIDs) > 0 {
		err = o.waitForBuilds(ctx, buildIDs)
		if err != nil {
			failProject(fmt.Errorf("build phase failed: %w", err))
			return
		}
	}

	// Развертывание контейнеров
	_ = o.projectRepo.UpdateStatus(ctx, projectID, model.ProjectStatusDeploying, nil)
	logger.InfoContext(ctx, "compose builds completed; starting deployment")

	// Списки для Rollback
	var createdVolumes []uuid.UUID
	var createdContainers []uuid.UUID

	// Функция отката (Rollback). Если что-то упало — удаляем уже созданное.
	rollback := func(deployErr error) {
		logger.WarnContext(ctx, "compose deployment failed; rolling back", "error", deployErr)
		// Сначала контейнеры (они зависят от томов)
		for _, cid := range createdContainers {
			_ = o.contService.Delete(ctx, cid)
		}
		// Затем тома
		for _, vid := range createdVolumes {
			_ = o.volumeService.Delete(ctx, vid)
		}
		failProject(deployErr)
	}

	// Создаем Именованные Тома (Volumes)
	// Маппинг: имя тома из YAML -> реальный UUID в базе
	volumeNameMap := make(map[string]uuid.UUID)

	for _, volParams := range parsedProject.Volumes {
		volParams.ProjectID = &projectID

		volID, err := o.volumeService.Create(ctx, volParams)
		if err != nil {
			rollback(fmt.Errorf("failed to create volume %s: %w", volParams.Name, err))
			return
		}
		createdVolumes = append(createdVolumes, volID)
		volumeNameMap[volParams.Name] = volID
	}

	serviceToContainerID := make(map[string]uuid.UUID)

	// Создаем Контейнеры (Services)
	for _, srv := range parsedProject.Services {
		// Подготавливаем Mounts (меняем строковое имя из YAML на сгенерированный UUID тома)
		var resolvedMounts []model.VolumeMountParams
		for _, m := range srv.VolumeMounts {
			if vid, ok := volumeNameMap[m.VolumeName]; ok {
				resolvedMounts = append(resolvedMounts, model.VolumeMountParams{
					VolumeID:   vid,
					MountPath:  m.MountPath,
					IsReadOnly: m.IsReadOnly,
				})
			}
		}

		// Формируем параметры
		createParams := model.ContainerCreateParams{
			ProjectID:    &projectID,
			Name:         fmt.Sprintf("%s_%s", projectName, srv.Name), // Визуальное имя для юзера
			NetworkAlias: srv.Name,                                    // DNS алиас из compose
			ImageTag:     srv.ImageTag,
			InternalPort: srv.InternalPort,
			DomainPrefix: srv.DomainPrefix,
			EnvVars:      srv.EnvVars,
			VolumeMounts: resolvedMounts,
			Command:      srv.Command,
			Entrypoint:   srv.Entrypoint,
			Restart:      srv.Restart,
			Healthcheck:  srv.Healthcheck,
		}

		contID, err := o.contService.Create(ctx, createParams)
		if err != nil {
			// Если нет квоты или Docker упал -> откатываем весь проект
			rollback(fmt.Errorf("failed to create service %s: %w", srv.Name, err))
			return
		}
		createdContainers = append(createdContainers, contID)
		serviceToContainerID[srv.Name] = contID
	}

	serviceGraph, err := projectServiceGraph(projectID, parsedProject.Services, serviceToContainerID)
	if err != nil {
		rollback(err)
		return
	}
	if err := o.projectRepo.SaveServiceGraph(ctx, projectID, serviceGraph); err != nil {
		rollback(fmt.Errorf("failed to save compose service graph: %w", err))
		return
	}

	// 3.3 Запуск контейнеров (Авто-старт после создания)
	for _, srv := range parsedProject.Services {
		contID := serviceToContainerID[srv.Name]

		// 1. Ждем выполнения условий зависимостей
		for _, dep := range srv.DependsOn {
			depContID, ok := serviceToContainerID[dep.ServiceName]
			if !ok {
				err := fmt.Errorf("dependency %s not found for service %s", dep.ServiceName, srv.Name)
				if dep.Optional {
					logger.WarnContext(ctx, "optional compose dependency unavailable; continuing deployment",
						"project_id", projectID,
						"service_name", srv.Name,
						"dependency_service_name", dep.ServiceName,
						"condition", dep.Condition,
						"error", err,
					)
					continue
				}
				rollback(err)
				return
			}

			// Получаем DockerID зависимости из БД
			depContInfo, err := o.contService.GetByID(ctx, depContID)
			if err != nil {
				if dep.Optional {
					logger.WarnContext(ctx, "optional compose dependency unavailable; continuing deployment",
						"project_id", projectID,
						"service_name", srv.Name,
						"dependency_service_name", dep.ServiceName,
						"condition", dep.Condition,
						"error", err,
					)
					continue
				}
				rollback(err)
				return
			}

			// Блокирующий поллинг состояния
			err = o.waitForCondition(ctx, depContInfo.DockerID, dep.Condition)
			if err != nil {
				if dep.Optional {
					logger.WarnContext(ctx, "optional compose dependency failed; continuing deployment",
						"project_id", projectID,
						"service_name", srv.Name,
						"dependency_service_name", dep.ServiceName,
						"condition", dep.Condition,
						"error", err,
					)
					continue
				}
				rollback(fmt.Errorf("dependency %s failed condition %s: %w", dep.ServiceName, dep.Condition, err))
				return
			}
		}

		// 2. Все зависимости готовы, запускаем сам сервис
		err := o.contService.Start(ctx, contID)
		if err != nil {
			rollback(fmt.Errorf("failed to start service %s: %w", srv.Name, err))
			return
		}
	}

	// 4. Финал
	_ = o.projectRepo.UpdateStatus(ctx, projectID, model.ProjectStatusRunning, nil)
	logger.InfoContext(ctx, "compose deployment completed")
}

func projectServiceGraph(projectID uuid.UUID, services []model.ComposeService, serviceToContainerID map[string]uuid.UUID) ([]model.ProjectServiceNode, error) {
	graph := make([]model.ProjectServiceNode, 0, len(services))
	serviceOrder := make(map[string]int, len(services))
	for i, srv := range services {
		serviceOrder[srv.Name] = i
	}
	for i, srv := range services {
		containerID, ok := serviceToContainerID[srv.Name]
		if !ok {
			return nil, fmt.Errorf("container mapping not found for service %s", srv.Name)
		}
		node := model.ProjectServiceNode{
			ProjectID:   projectID,
			ContainerID: containerID,
			ServiceName: srv.Name,
			StartOrder:  i,
		}
		for _, dep := range srv.DependsOn {
			depOrder, ok := serviceOrder[dep.ServiceName]
			if !ok {
				if dep.Optional {
					continue
				}
				return nil, fmt.Errorf("dependency %s not found for service %s", dep.ServiceName, srv.Name)
			}
			if depOrder >= i {
				return nil, fmt.Errorf("dependency %s must be ordered before service %s", dep.ServiceName, srv.Name)
			}
			depContainerID, ok := serviceToContainerID[dep.ServiceName]
			if !ok {
				if dep.Optional {
					continue
				}
				return nil, fmt.Errorf("container mapping not found for dependency %s", dep.ServiceName)
			}
			node.Dependencies = append(node.Dependencies, model.ProjectServiceDependency{
				ProjectID:            projectID,
				ContainerID:          containerID,
				DependsOnContainerID: depContainerID,
				DependsOnServiceName: dep.ServiceName,
				Condition:            dep.Condition,
				Optional:             dep.Optional,
			})
		}
		graph = append(graph, node)
	}
	return graph, nil
}

func (o *Orchestrator) waitForBuilds(ctx context.Context, buildIDs []uuid.UUID) error {
	ticker := time.NewTicker(time.Duration(o.cfg.Get().ComposeBuildPollIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			ticker.Reset(time.Duration(o.cfg.Get().ComposeBuildPollIntervalSeconds) * time.Second)
			allSuccess := true
			for _, bid := range buildIDs {
				b, err := o.buildRepo.GetByID(ctx, bid)
				if err != nil {
					continue // Ждем, пока запись появится
				}
				if model.IsBuildFailedStatus(b.Status) {
					o.cancelBuilds(ctx, buildIDs)
					return apperrors.New(apperrors.ErrConflict, fmt.Sprintf("build failed with status: %s", b.Status))
				}
				if b.Status != model.BuildStatusSuccess {
					allSuccess = false
				}
			}
			if allSuccess {
				return nil
			}
		}
	}
}

func (o *Orchestrator) failedBuildStatus(ctx context.Context, buildIDs []uuid.UUID) (string, bool) {
	for _, buildID := range buildIDs {
		b, err := o.buildRepo.GetByID(ctx, buildID)
		if err != nil {
			continue
		}
		if model.IsBuildFailedStatus(b.Status) {
			return b.Status, true
		}
	}
	return "", false
}

func (o *Orchestrator) cancelBuilds(ctx context.Context, buildIDs []uuid.UUID) {
	for _, buildID := range buildIDs {
		if err := o.builderClient.CancelBuild(ctx, buildID); err != nil {
			o.logger.WarnContext(ctx, "failed to cancel build", "build_id", buildID, "error", err)
		}
	}
}

func extractComposeFile(archiveBytes []byte) ([]byte, error) {
	// 1. Попытка прочитать как ZIP архив
	r, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err == nil {
		for _, f := range r.File {
			if f.Name == "docker-compose.yml" || f.Name == "docker-compose.yaml" {
				rc, err := f.Open()
				if err != nil {
					return nil, err
				}
				defer rc.Close()
				return io.ReadAll(rc)
			}
		}
		return nil, apperrors.New(apperrors.ErrBadRequest, "docker-compose.yml not found in zip archive")
	}

	// 2. Если это не ZIP, проверяем, не является ли это просто сырым YAML файлом
	// (Например, если файл содержит строку 'version:' или 'services:')
	contentStr := string(archiveBytes)
	if strings.Contains(contentStr, "services:") {
		return archiveBytes, nil
	}

	return nil, apperrors.New(apperrors.ErrBadRequest, "invalid file format: expected zip archive or raw docker-compose.yml")
}

func (o *Orchestrator) waitForCondition(ctx context.Context, dockerID string, condition string) error {
	timeout := time.After(time.Duration(o.cfg.Get().ComposeDependencyWaitTimeoutMinutes) * time.Minute)
	ticker := time.NewTicker(time.Duration(o.cfg.Get().ComposeDependencyPollIntervalSeconds) * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return apperrors.New(apperrors.ErrConflict, "timeout waiting for dependency state")
		case <-ticker.C:
			ticker.Reset(time.Duration(o.cfg.Get().ComposeDependencyPollIntervalSeconds) * time.Second)
			inspect, err := o.dockerAPI.InspectContainer(ctx, dockerID)
			if err != nil {
				continue // Контейнер мог еще не успеть появиться, ждем дальше
			}

			state := inspect.State
			switch condition {
			case model.ComposeDependencyConditionHealthy:
				if state.HealthStatus == nil {
					return apperrors.New(apperrors.ErrBadRequest, "service_healthy requested, but no healthcheck defined for container")
				}
				if *state.HealthStatus == "healthy" {
					return nil // Зависимость здорова, идем дальше!
				}
				if *state.HealthStatus == "unhealthy" {
					return apperrors.New(apperrors.ErrConflict, "dependency became unhealthy")
				}
				if !state.Running && state.ExitCode != 0 {
					return apperrors.New(apperrors.ErrConflict, "dependency exited before becoming healthy")
				}
			case model.ComposeDependencyConditionCompletedSuccessfully:
				if !state.Running {
					if state.ExitCode == 0 {
						return nil // Успешно завершил работу (идеально для migrate)
					}
					return apperrors.New(apperrors.ErrConflict, "dependency exited with non-zero code")
				}
			case model.ComposeDependencyConditionStarted:
				if state.Running {
					return nil // Просто запустился
				}
				if !state.Running && state.ExitCode != 0 {
					return apperrors.New(apperrors.ErrConflict, "dependency failed to start")
				}
			default:
				return apperrors.New(apperrors.ErrBadRequest, "unsupported dependency condition")
			}
		}
	}
}
