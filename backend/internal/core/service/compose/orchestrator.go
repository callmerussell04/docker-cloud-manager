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
	"time"

	"github.com/callmerussell04/docker-cloud-manager/internal/core/domain"
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
	Start(ctx context.Context, ownerID, containerID uuid.UUID) error // НОВЫЙ МЕТОД ДЛЯ АВТОЗАПУСКА
	Delete(ctx context.Context, ownerID, containerID uuid.UUID) error
}

type Orchestrator struct {
	parser         *Parser
	projectRepo    ProjectRepository
	buildRepo      BuildRepository
	volumeService  VolumeService
	contService    ContainerService
	builderHTTPUrl string
	httpClient     *http.Client
}

func NewOrchestrator(
	projectRepo ProjectRepository,
	buildRepo BuildRepository,
	volumeService VolumeService,
	contService ContainerService,
	builderHTTPUrl string,
) *Orchestrator {
	return &Orchestrator{
		parser:         NewParser(),
		projectRepo:    projectRepo,
		buildRepo:      buildRepo,
		volumeService:  volumeService,
		contService:    contService,
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
			ProjectID:         &projectID,
			Name:              fmt.Sprintf("%s_%s", projectName, srv.Name), // Визуальное имя для юзера
			NetworkAlias:      srv.Name,                                    // DNS алиас из compose
			ImageTag:          srv.ImageTag,
			InternalPort:      srv.InternalPort,
			DomainPrefix:      srv.DomainPrefix,
			EnvVars:           srv.EnvVars,
			VolumeMounts:      resolvedMounts,
			RequestedMemoryMB: 0, // 0 = использует DefaultMemoryReservation из конфига
		}

		contID, err := o.contService.Create(ctx, ownerID, createParams)
		if err != nil {
			// Если нет квоты или Docker упал -> откатываем весь проект
			rollback(fmt.Errorf("failed to create service %s: %w", srv.Name, err))
			return
		}
		createdContainers = append(createdContainers, contID)
	}

	// 3.3 Запуск контейнеров (Авто-старт после создания)
	for _, cid := range createdContainers {
		err := o.contService.Start(ctx, ownerID, cid)
		if err != nil {
			// Если Docker не смог запустить (например, Entrypoint крашнулся)
			rollback(fmt.Errorf("failed to start container: %w", err))
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
	r, err := zip.NewReader(bytes.NewReader(archiveBytes), int64(len(archiveBytes)))
	if err != nil {
		return nil, err
	}
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
	return nil, fmt.Errorf("docker-compose.yml not found in archive root")
}
