package handler

import (
	"context"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/dto"
	"github.com/callmerussell04/docker-cloud-manager/internal/gateway/model"
	"github.com/callmerussell04/docker-cloud-manager/pkg/apperrors"
	"github.com/callmerussell04/docker-cloud-manager/pkg/httpresponse"
	"github.com/callmerussell04/docker-cloud-manager/pkg/validation"
)

type UserManagementService interface {
	ListUsers(ctx context.Context, page, limit int) (model.PaginatedAdminUsers, error)
	GetUser(ctx context.Context, userID string) (model.AdminUser, error)
	CreateUser(ctx context.Context, input model.CreateUserInput) (model.AdminUser, error)
	UpdateUser(ctx context.Context, userID string, input model.UpdateUserInput) (model.AdminUser, error)
	DeactivateUser(ctx context.Context, userID string) (model.AdminUser, error)
	ReactivateUser(ctx context.Context, userID string) (model.AdminUser, error)
}

type UserManagementHandler struct {
	service UserManagementService
}

func NewUserManagementHandler(service UserManagementService) *UserManagementHandler {
	return &UserManagementHandler{service: service}
}

func (h *UserManagementHandler) ListUsers(c *gin.Context) {
	page, limit, ok := getPaginationParams(c)
	if !ok {
		return
	}

	users, err := h.service.ListUsers(c.Request.Context(), page, limit)
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}

	c.JSON(http.StatusOK, dto.PaginatedAdminUsers{
		Users:      usersToDTO(users.Users),
		TotalCount: users.TotalCount,
	})
}

func (h *UserManagementHandler) GetUser(c *gin.Context) {
	userID, ok := pathUUID(c, "id")
	if !ok {
		return
	}

	user, err := h.service.GetUser(c.Request.Context(), userID)
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	c.JSON(http.StatusOK, userToDTO(user))
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

	user, err := h.service.CreateUser(c.Request.Context(), model.CreateUserInput{
		Username:    req.Username,
		Email:       req.Email,
		Password:    req.Password,
		Role:        req.Role,
		Status:      req.Status,
		QuotaCPU:    req.QuotaCPU,
		QuotaRAMMB:  req.QuotaRAMMB,
		QuotaDiskMB: req.QuotaDiskMB,
	})
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	c.JSON(http.StatusCreated, userToDTO(user))
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

	user, err := h.service.UpdateUser(c.Request.Context(), userID, model.UpdateUserInput{
		Username:    req.Username,
		Email:       req.Email,
		Password:    req.Password,
		Role:        req.Role,
		Status:      req.Status,
		QuotaCPU:    req.QuotaCPU,
		QuotaRAMMB:  req.QuotaRAMMB,
		QuotaDiskMB: req.QuotaDiskMB,
	})
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	c.JSON(http.StatusOK, userToDTO(user))
}

func (h *UserManagementHandler) DeactivateUser(c *gin.Context) {
	userID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	user, err := h.service.DeactivateUser(c.Request.Context(), userID)
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	c.JSON(http.StatusOK, userToDTO(user))
}

func (h *UserManagementHandler) ReactivateUser(c *gin.Context) {
	userID, ok := pathUUID(c, "id")
	if !ok {
		return
	}
	user, err := h.service.ReactivateUser(c.Request.Context(), userID)
	if err != nil {
		httpresponse.Respond(c, httpresponse.Status(err), err)
		return
	}
	c.JSON(http.StatusOK, userToDTO(user))
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

func usersToDTO(users []model.AdminUser) []dto.AdminUserDTO {
	resp := make([]dto.AdminUserDTO, 0, len(users))
	for _, user := range users {
		resp = append(resp, userToDTO(user))
	}
	return resp
}

func userToDTO(user model.AdminUser) dto.AdminUserDTO {
	return dto.AdminUserDTO{
		UserID:      user.UserID,
		Username:    user.Username,
		Email:       user.Email,
		Role:        user.Role,
		Status:      user.Status,
		QuotaCPU:    user.QuotaCPU,
		QuotaRAMMB:  user.QuotaRAMMB,
		QuotaDiskMB: user.QuotaDiskMB,
	}
}
