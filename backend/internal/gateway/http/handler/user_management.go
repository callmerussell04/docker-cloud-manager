package handler

import (
	"context"
	"net/http"
	"strconv"

	"github.com/gin-gonic/gin"

	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/auditlog"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/httpresponse"
	"github.com/callmerussell04/docker-cloud-manager/backend/internal/platform/validation"
)

// UserDirectory changes users' accounts in SSO.
type UserDirectory interface {
	CreateUser(ctx context.Context, input model.CreateUserInput) (model.AdminUser, error)
	UpdateUser(ctx context.Context, userID string, input model.UpdateUserInput) (model.AdminUser, error)
	DeactivateUser(ctx context.Context, userID string) (model.AdminUser, error)
	ReactivateUser(ctx context.Context, userID string) (model.AdminUser, error)
}

// AdminUserService combines SSO's users with Core's quotas.
type AdminUserService interface {
	ListUsers(ctx context.Context, page, limit int) (model.PaginatedAdminUsersWithQuota, error)
	GetUser(ctx context.Context, userID string) (model.AdminUserWithQuota, error)
	SetUserQuota(ctx context.Context, userID string, limits model.UserQuotaLimits) (model.UserQuota, error)
	ResetUserQuota(ctx context.Context, userID string) (model.UserQuota, error)
}

type UserManagementDeps struct {
	Users      UserDirectory
	AdminUsers AdminUserService
	Audit      AuditRecorder
}

type UserManagementHandler struct {
	users      UserDirectory
	adminUsers AdminUserService
	audit      AuditRecorder
}

func NewUserManagementHandler(deps UserManagementDeps) *UserManagementHandler {
	return &UserManagementHandler{users: deps.Users, adminUsers: deps.AdminUsers, audit: deps.Audit}
}

func (h *UserManagementHandler) ListUsers(c *gin.Context) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}

	users, err := h.adminUsers.ListUsers(c.Request.Context(), page, limit)
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}

	c.JSON(http.StatusOK, dto.PaginatedAdminUsers{
		Users:      usersWithQuotaToDTO(users.Users),
		TotalCount: users.TotalCount,
	})
}

func (h *UserManagementHandler) GetUser(c *gin.Context) {
	userID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	user, err := h.adminUsers.GetUser(c.Request.Context(), userID)
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	c.JSON(http.StatusOK, userWithQuotaToDTO(user))
}

func (h *UserManagementHandler) CreateUser(c *gin.Context) {
	var req dto.CreateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}
	if !validateUserRequest(c, req.Username, req.Role, req.Status) {
		return
	}

	user, err := h.users.CreateUser(c.Request.Context(), model.CreateUserInput{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
		Role:     req.Role,
		Status:   req.Status,
	})
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	c.JSON(http.StatusCreated, accountToDTO(user))
}

func (h *UserManagementHandler) UpdateUser(c *gin.Context) {
	userID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	var req dto.UpdateUserRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		httpresponse.Respond(c, http.StatusBadRequest, apperrors.ErrBadRequest)
		return
	}
	if !validateUserRequest(c, req.Username, req.Role, req.Status) {
		return
	}

	user, err := h.users.UpdateUser(c.Request.Context(), userID, model.UpdateUserInput{
		Username: req.Username,
		Email:    req.Email,
		Password: req.Password,
		Role:     req.Role,
		Status:   req.Status,
	})
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	c.JSON(http.StatusOK, accountToDTO(user))
}

func (h *UserManagementHandler) DeactivateUser(c *gin.Context) {
	userID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	user, err := h.users.DeactivateUser(c.Request.Context(), userID)
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	c.JSON(http.StatusOK, accountToDTO(user))
}

func (h *UserManagementHandler) ReactivateUser(c *gin.Context) {
	userID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	user, err := h.users.ReactivateUser(c.Request.Context(), userID)
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	c.JSON(http.StatusOK, accountToDTO(user))
}

func validateUserRequest(c *gin.Context, username, role, status string) bool {
	if err := validation.ResourceName(username); err != nil {
		badRequest(c, err.Error())
		return false
	}
	switch role {
	case "admin", "user":
	default:
		badRequest(c, "role must be one of: admin, user")
		return false
	}
	switch status {
	case "active", "deactivated":
	default:
		badRequest(c, "status must be one of: active, deactivated")
		return false
	}
	return true
}

// SetUserQuota gives the user a custom quota. Lowering it below what the
// user already reserves stops nothing; it applies from their next admission.
func (h *UserManagementHandler) SetUserQuota(c *gin.Context) {
	userID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	var req dto.SetUserQuotaRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		badRequest(c, "quota_cpu, quota_ram_mb and quota_disk_mb are required numbers")
		return
	}
	// The message never repeats the rejected value.
	if *req.QuotaCPU <= 0 || *req.QuotaRAMMB <= 0 || *req.QuotaDiskMB <= 0 {
		badRequest(c, "quota_cpu, quota_ram_mb and quota_disk_mb must be positive")
		return
	}

	quota, err := h.adminUsers.SetUserQuota(c.Request.Context(), userID, model.UserQuotaLimits{
		CPU:    *req.QuotaCPU,
		RAMMB:  *req.QuotaRAMMB,
		DiskMB: *req.QuotaDiskMB,
	})
	outcome, errorCode := auditOutcome(err)
	details := "{}"
	if err == nil {
		details = auditlog.SafeDetailsJSON(map[string]string{
			auditlog.DetailQuotaCPU:    strconv.FormatFloat(quota.CPU, 'f', -1, 64),
			auditlog.DetailQuotaRAMMB:  strconv.FormatInt(quota.RAMMB, 10),
			auditlog.DetailQuotaDiskMB: strconv.FormatInt(quota.DiskMB, 10),
		})
	}
	recordAudit(c, h.audit, auditInput{
		Action:       auditlog.ActionUserQuotaSet,
		ResourceType: auditlog.ResourceUser,
		ResourceID:   userID,
		OwnerID:      userID,
		Outcome:      outcome,
		ErrorCode:    errorCode,
		DetailsJSON:  details,
	})
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	c.JSON(http.StatusOK, quotaToDTO(quota))
}

// ResetUserQuota returns the user to the default quota.
func (h *UserManagementHandler) ResetUserQuota(c *gin.Context) {
	userID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	quota, err := h.adminUsers.ResetUserQuota(c.Request.Context(), userID)
	outcome, errorCode := auditOutcome(err)
	recordAudit(c, h.audit, auditInput{
		Action:       auditlog.ActionUserQuotaReset,
		ResourceType: auditlog.ResourceUser,
		ResourceID:   userID,
		OwnerID:      userID,
		Outcome:      outcome,
		ErrorCode:    errorCode,
	})
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	c.JSON(http.StatusOK, quotaToDTO(quota))
}

func usersWithQuotaToDTO(users []model.AdminUserWithQuota) []dto.AdminUserDTO {
	resp := make([]dto.AdminUserDTO, 0, len(users))
	for _, user := range users {
		resp = append(resp, userWithQuotaToDTO(user))
	}
	return resp
}

func userWithQuotaToDTO(user model.AdminUserWithQuota) dto.AdminUserDTO {
	return dto.AdminUserDTO{
		UserID:      user.User.UserID,
		Username:    user.User.Username,
		Email:       user.User.Email,
		Role:        user.User.Role,
		Status:      user.User.Status,
		QuotaCPU:    user.Quota.CPU,
		QuotaRAMMB:  user.Quota.RAMMB,
		QuotaDiskMB: user.Quota.DiskMB,
		QuotaCustom: user.Quota.Custom,
	}
}

func accountToDTO(user model.AdminUser) dto.AdminUserAccountDTO {
	return dto.AdminUserAccountDTO{
		UserID:   user.UserID,
		Username: user.Username,
		Email:    user.Email,
		Role:     user.Role,
		Status:   user.Status,
	}
}

func quotaToDTO(quota model.UserQuota) dto.UserQuotaDTO {
	return dto.UserQuotaDTO{
		UserID:      quota.UserID,
		QuotaCPU:    quota.CPU,
		QuotaRAMMB:  quota.RAMMB,
		QuotaDiskMB: quota.DiskMB,
		QuotaCustom: quota.Custom,
	}
}
