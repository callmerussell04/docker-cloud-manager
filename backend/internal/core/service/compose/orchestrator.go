package compose

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/logging"
	"github.com/google/uuid"
)

type ProjectRepository interface {
	Save(ctx context.Context, p model.Project) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, errMsg *string) error
}

type BuildRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (model.Build, error)
}

type VolumeService interface {
	Create(ctx context.Context, ownerID uuid.UUID, params model.VolumeCreateParams) (uuid.UUID, error)
	Delete(ctx context.Context, ownerID, volumeID uuid.UUID) error
}

type ContainerService interface {
	Create(ctx context.Context, ownerID uuid.UUID, params model.ContainerCreateParams) (uuid.UUID, error)
	Start(ctx context.Context, ownerID, containerID uuid.UUID) error
	Delete(ctx context.Context, ownerID, containerID uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (model.Container, error)
}

type ComposeDockerAPI interface {
	InspectContainer(ctx context.Context, dockerID string) (model.ContainerInspection, error)
}

type Orchestrator struct {
	parser         *Parser
	projectRepo    ProjectRepository
	buildRepo      BuildRepository
	volumeService  VolumeService
	contService    ContainerService
	dockerAPI      ComposeDockerAPI
	builderHTTPUrl string
	httpClient     *http.Client
	internalToken  string
	logger         *slog.Logger
}

func NewOrchestrator(
	projectRepo ProjectRepository,
	buildRepo BuildRepository,
	volumeService VolumeService,
	contService ContainerService,
	dockerAPI ComposeDockerAPI,
	builderHTTPUrl string,
	internalToken string,
	logger *slog.Logger,
) *Orchestrator {
	return &Orchestrator{
		parser:         NewParser(),
		projectRepo:    projectRepo,
		buildRepo:      buildRepo,
		volumeService:  volumeService,
		contService:    contService,
		dockerAPI:      dockerAPI,
		builderHTTPUrl: builderHTTPUrl,
		httpClient:     &http.Client{Timeout: 30 * time.Second},
		internalToken:  internalToken,
		logger:         logging.WithComponent(logger, "compose_orchestrator"),
	}
}

// StartDeployment - Точка входа. Создает проект и запускает горутину оркестрации
func (o *Orchestrator) StartDeployment(ctx context.Context, ownerID uuid.UUID, projectName string, archiveBytes []byte) (uuid.UUID, error) {
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
	go o.runPipeline(projectID, ownerID, projectName, archiveBytes, requestID)

	return projectID, nil
}

func (o *Orchestrator) runPipeline(projectID, ownerID uuid.UUID, projectName string, archiveBytes []byte, requestID string) {
	ctx := logging.ContextWithRequestID(context.Background(), requestID)
	logger := o.logger.With("project_id", projectID, "owner_id", ownerID)
	logger.InfoContext(ctx, "compose deployment pipeline started", "project_name", projectName)

	// Вспомогательная функция для обновления статуса при ошибке
	failProject := func(err error) {
		errMsg := apperrors.SafeMessage(err)
		logger.ErrorContext(ctx, "compose deployment failed", "error", err)
		_ = o.projectRepo.UpdateStatus(ctx, projectID, model.ProjectStatusFailed, &errMsg)
	}

	// 1. Извлечение и парсинг YAML
	composeYaml, err := extractComposeFile(archiveBytes)
	if err != nil {
		failProject(err)
		return
	}

	// Парсинг и валидация
	parsedProject, err := o.parser.ParseAndValidate(ctx, projectName, composeYaml)
	if err != nil {
		failProject(err)
		return
	}

	// 2. Оркестрация сборок (Kaniko)
	var buildIDs []uuid.UUID
	for _, srv := range parsedProject.Services {
		if srv.BuildContext != "" { // Нужна сборка
			buildID, err := o.triggerBuild(ctx, ownerID, srv, archiveBytes)
			if err != nil {
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
			_ = o.contService.Delete(ctx, ownerID, cid)
		}
		// Затем тома
		for _, vid := range createdVolumes {
			_ = o.volumeService.Delete(ctx, ownerID, vid)
		}
		failProject(deployErr)
	}

	// Создаем Именованные Тома (Volumes)
	// Маппинг: имя тома из YAML -> реальный UUID в базе
	volumeNameMap := make(map[string]uuid.UUID)

	for _, volParams := range parsedProject.Volumes {
		volParams.ProjectID = &projectID

		volID, err := o.volumeService.Create(ctx, ownerID, volParams)
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

		contID, err := o.contService.Create(ctx, ownerID, createParams)
		if err != nil {
			// Если нет квоты или Docker упал -> откатываем весь проект
			rollback(fmt.Errorf("failed to create service %s: %w", srv.Name, err))
			return
		}
		createdContainers = append(createdContainers, contID)
		serviceToContainerID[srv.Name] = contID
	}

	// 3.3 Запуск контейнеров (Авто-старт после создания)
	for _, srv := range parsedProject.Services {
		contID := serviceToContainerID[srv.Name]

		// 1. Ждем выполнения условий зависимостей
		for depName, condition := range srv.DependsOn {
			depContID, ok := serviceToContainerID[depName]
			if !ok {
				rollback(fmt.Errorf("dependency %s not found for service %s", depName, srv.Name))
				return
			}

			// Получаем DockerID зависимости из БД
			depContInfo, err := o.contService.GetByID(ctx, depContID)
			if err != nil {
				rollback(err)
				return
			}

			// Блокирующий поллинг состояния
			err = o.waitForCondition(ctx, depContInfo.DockerID, condition)
			if err != nil {
				rollback(fmt.Errorf("dependency %s failed condition %s: %w", depName, condition, err))
				return
			}
		}

		// 2. Все зависимости готовы, запускаем сам сервис
		err := o.contService.Start(ctx, ownerID, contID)
		if err != nil {
			rollback(fmt.Errorf("failed to start service %s: %w", srv.Name, err))
			return
		}
	}

	// 4. Финал
	_ = o.projectRepo.UpdateStatus(ctx, projectID, model.ProjectStatusRunning, nil)
	logger.InfoContext(ctx, "compose deployment completed")
}

func (o *Orchestrator) triggerBuild(ctx context.Context, ownerID uuid.UUID, srv model.ComposeService, archiveBytes []byte) (uuid.UUID, error) {
	body := &bytes.Buffer{}
	writer := multipart.NewWriter(body)

	_ = writer.WriteField("tag", srv.ImageTag)
	_ = writer.WriteField("context", srv.BuildContext)
	_ = writer.WriteField("dockerfile", srv.Dockerfile)
	if len(srv.BuildArgs) > 0 {
		argsJSON, _ := json.Marshal(srv.BuildArgs)
		_ = writer.WriteField("build_args", string(argsJSON))
	}

	part, err := writer.CreateFormFile("archive", "compose.zip")
	if err == nil {
		_, _ = part.Write(archiveBytes)
	}
	writer.Close()

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, o.builderHTTPUrl+"/api/v1/images/build", body)
	if err != nil {
		return uuid.Nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-Id", ownerID.String())
	req.Header.Set("X-Internal-Token", o.internalToken)
	if requestID := logging.RequestIDFromContext(ctx); requestID != "" {
		req.Header.Set(logging.RequestIDHeader, requestID)
	}

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return uuid.Nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return uuid.Nil, builderHTTPError(resp)
	}

	var result struct {
		BuildID string `json:"build_id"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return uuid.Nil, err
	}

	return uuid.Parse(result.BuildID)
}

func (o *Orchestrator) waitForBuilds(ctx context.Context, buildIDs []uuid.UUID) error {
	ticker := time.NewTicker(3 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
			allSuccess := true
			for _, bid := range buildIDs {
				b, err := o.buildRepo.GetByID(ctx, bid)
				if err != nil {
					continue // Ждем, пока запись появится
				}
				if model.IsBuildFailedStatus(b.Status) {
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

func builderHTTPError(resp *http.Response) error {
	message := fmt.Sprintf("builder returned status %d", resp.StatusCode)

	var body struct {
		Error string `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&body); err == nil && body.Error != "" {
		message = body.Error
	}

	switch resp.StatusCode {
	case http.StatusBadRequest:
		return apperrors.New(apperrors.ErrBadRequest, message)
	case http.StatusUnauthorized:
		return apperrors.New(apperrors.ErrUnauthorized, message)
	case http.StatusForbidden:
		return apperrors.New(apperrors.ErrForbidden, message)
	case http.StatusNotFound:
		return apperrors.New(apperrors.ErrNotFound, message)
	case http.StatusConflict:
		return apperrors.New(apperrors.ErrConflict, message)
	default:
		return apperrors.New(apperrors.ErrInternal, apperrors.ErrInternal.Error())
	}
}

func (o *Orchestrator) waitForCondition(ctx context.Context, dockerID string, condition string) error {
	timeout := time.After(5 * time.Minute) // Максимальное время ожидания поднятия зависимости
	ticker := time.NewTicker(2 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timeout:
			return apperrors.New(apperrors.ErrConflict, "timeout waiting for dependency state")
		case <-ticker.C:
			inspect, err := o.dockerAPI.InspectContainer(ctx, dockerID)
			if err != nil {
				continue // Контейнер мог еще не успеть появиться, ждем дальше
			}

			state := inspect.State
			switch condition {
			case "service_healthy":
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
			case "service_completed_successfully":
				if !state.Running {
					if state.ExitCode == 0 {
						return nil // Успешно завершил работу (идеально для migrate)
					}
					return apperrors.New(apperrors.ErrConflict, "dependency exited with non-zero code")
				}
			case "service_started":
				if state.Running {
					return nil // Просто запустился
				}
				if !state.Running && state.ExitCode != 0 {
					return apperrors.New(apperrors.ErrConflict, "dependency failed to start")
				}
			default:
				// По дефолту (если condition пустой) ведем себя как service_started
				if state.Running {
					return nil
				}
			}
		}
	}
}
