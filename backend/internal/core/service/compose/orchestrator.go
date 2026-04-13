package compose

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"mime/multipart"
	"net/http"
	"strings"
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
	"github.com/docker/docker/api/types/container"
	"github.com/google/uuid"
)

type ProjectRepository interface {
	Save(ctx context.Context, p domain.Project) error
	UpdateStatus(ctx context.Context, id uuid.UUID, status string, errMsg *string) error
}

type BuildRepository interface {
	GetByID(ctx context.Context, id uuid.UUID) (domain.Build, error)
}

type VolumeService interface {
	Create(ctx context.Context, ownerID uuid.UUID, params domain.VolumeCreateParams) (uuid.UUID, error)
	Delete(ctx context.Context, ownerID, volumeID uuid.UUID) error
}

type ContainerService interface {
	Create(ctx context.Context, ownerID uuid.UUID, params domain.ContainerCreateParams) (uuid.UUID, error)
	Start(ctx context.Context, ownerID, containerID uuid.UUID) error
	Delete(ctx context.Context, ownerID, containerID uuid.UUID) error
	GetByID(ctx context.Context, id uuid.UUID) (domain.Container, error)
}

type ComposeDockerAPI interface {
	InspectContainer(ctx context.Context, dockerID string) (*container.InspectResponse, error)
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
}

func NewOrchestrator(
	projectRepo ProjectRepository,
	buildRepo BuildRepository,
	volumeService VolumeService,
	contService ContainerService,
	dockerAPI ComposeDockerAPI,
	builderHTTPUrl string,
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
	}
}

// StartDeployment - Точка входа. Создает проект и запускает горутину оркестрации
func (o *Orchestrator) StartDeployment(ctx context.Context, ownerID uuid.UUID, projectName string, archiveBytes []byte) (uuid.UUID, error) {
	projectID := uuid.New()
	p := domain.Project{
		ID:      projectID,
		OwnerID: ownerID,
		Name:    projectName,
		Status:  domain.ProjectStatusBuilding,
	}

	if err := o.projectRepo.Save(ctx, p); err != nil {
		return uuid.Nil, err
	}

	// Запускаем асинхронный процесс (Этап 3 и 4)
	go o.runPipeline(projectID, ownerID, projectName, archiveBytes)

	return projectID, nil
}

func (o *Orchestrator) runPipeline(projectID, ownerID uuid.UUID, projectName string, archiveBytes []byte) {
	ctx := context.Background()

	// Вспомогательная функция для обновления статуса при ошибке
	failProject := func(err error) {
		errMsg := err.Error()
		log.Printf("[Orchestrator] Project %s failed: %v", projectID, err)
		_ = o.projectRepo.UpdateStatus(ctx, projectID, domain.ProjectStatusFailed, &errMsg)
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
			buildID, err := o.triggerBuild(ownerID, srv, archiveBytes)
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
	_ = o.projectRepo.UpdateStatus(ctx, projectID, domain.ProjectStatusDeploying, nil)
	log.Printf("[Orchestrator] Project %s builds completed. Starting deployment...", projectID)

	// Списки для Rollback
	var createdVolumes []uuid.UUID
	var createdContainers []uuid.UUID

	// Функция отката (Rollback). Если что-то упало — удаляем уже созданное.
	rollback := func(deployErr error) {
		log.Printf("[Orchestrator] Deployment failed. Rolling back Project %s...", projectID)
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
		var resolvedMounts []domain.VolumeMountParams
		for _, m := range srv.VolumeMounts {
			if vid, ok := volumeNameMap[m.VolumeName]; ok {
				resolvedMounts = append(resolvedMounts, domain.VolumeMountParams{
					VolumeID:   vid,
					MountPath:  m.MountPath,
					IsReadOnly: m.IsReadOnly,
				})
			}
		}

		// Формируем параметры
		createParams := domain.ContainerCreateParams{
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
	_ = o.projectRepo.UpdateStatus(ctx, projectID, domain.ProjectStatusRunning, nil)
	log.Printf("[Orchestrator] Project %s deployed and started successfully!", projectID)
}

func (o *Orchestrator) triggerBuild(ownerID uuid.UUID, srv domain.ComposeService, archiveBytes []byte) (uuid.UUID, error) {
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

	req, err := http.NewRequest("POST", o.builderHTTPUrl+"/api/v1/images/build", body)
	if err != nil {
		return uuid.Nil, err
	}
	req.Header.Set("Content-Type", writer.FormDataContentType())
	req.Header.Set("X-User-Id", ownerID.String())

	resp, err := o.httpClient.Do(req)
	if err != nil {
		return uuid.Nil, err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusAccepted {
		return uuid.Nil, fmt.Errorf("builder returned status %d", resp.StatusCode)
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
				if b.Status == domain.BuildStatusFailed || b.Status == "failed_timeout" || b.Status == "failed_quota_exceeded" {
					return fmt.Errorf("build %s failed with status: %s", bid, b.Status)
				}
				if b.Status != domain.BuildStatusSuccess {
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
		return nil, fmt.Errorf("docker-compose.yml not found in zip archive")
	}

	// 2. Если это не ZIP, проверяем, не является ли это просто сырым YAML файлом
	// (Например, если файл содержит строку 'version:' или 'services:')
	contentStr := string(archiveBytes)
	if strings.Contains(contentStr, "services:") {
		return archiveBytes, nil
	}

	return nil, fmt.Errorf("invalid file format: not a valid zip archive or raw docker-compose.yml")
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
			return fmt.Errorf("timeout waiting for dependency state")
		case <-ticker.C:
			inspect, err := o.dockerAPI.InspectContainer(ctx, dockerID)
			if err != nil {
				continue // Контейнер мог еще не успеть появиться, ждем дальше
			}

			state := inspect.State
			switch condition {
			case "service_healthy":
				if state.Health == nil {
					return fmt.Errorf("service_healthy requested, but no healthcheck defined for container")
				}
				if state.Health.Status == "healthy" {
					return nil // Зависимость здорова, идем дальше!
				}
				if state.Health.Status == "unhealthy" {
					return fmt.Errorf("dependency became unhealthy")
				}
				if !state.Running && state.ExitCode != 0 {
					return fmt.Errorf("dependency exited with code %d before becoming healthy", state.ExitCode)
				}
			case "service_completed_successfully":
				if !state.Running {
					if state.ExitCode == 0 {
						return nil // Успешно завершил работу (идеально для migrate)
					}
					return fmt.Errorf("dependency exited with non-zero code %d", state.ExitCode)
				}
			case "service_started":
				if state.Running {
					return nil // Просто запустился
				}
				if !state.Running && state.ExitCode != 0 {
					return fmt.Errorf("dependency failed to start")
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
