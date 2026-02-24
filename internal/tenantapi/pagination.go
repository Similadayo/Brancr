package tenantapi

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"gorm.io/gorm"
)

// PaginationParams holds pagination parameters from request
type PaginationParams struct {
	Page   int
	Limit  int
	Offset int
}

// ParsePagination parses pagination parameters from request
func ParsePagination(r *http.Request) PaginationParams {
	page := 1
	limit := 100

	if p := r.URL.Query().Get("page"); p != "" {
		if parsed, err := strconv.Atoi(p); err == nil && parsed > 0 {
			page = parsed
		}
	}

	if l := r.URL.Query().Get("limit"); l != "" {
		if parsed, err := strconv.Atoi(l); err == nil && parsed > 0 && parsed <= 1000 {
			limit = parsed
		}
	}

	offset := (page - 1) * limit

	return PaginationParams{
		Page:   page,
		Limit:  limit,
		Offset: offset,
	}
}

// ApplyPagination applies pagination to GORM query
func ApplyPagination(query *gorm.DB, params PaginationParams) *gorm.DB {
	return query.Offset(params.Offset).Limit(params.Limit)
}

// PaginatedResponse wraps response with pagination metadata
type PaginatedResponse struct {
	Data       interface{}    `json:"data"`
	Pagination PaginationMeta `json:"pagination"`
}

// PaginationMeta contains pagination metadata
type PaginationMeta struct {
	Page       int   `json:"page"`
	Limit      int   `json:"limit"`
	Total      int64 `json:"total"`
	TotalPages int   `json:"total_pages"`
	HasNext    bool  `json:"has_next"`
	HasPrev    bool  `json:"has_prev"`
}

// NewPaginatedResponse creates a paginated response
func NewPaginatedResponse(data interface{}, total int64, params PaginationParams) PaginatedResponse {
	totalPages := int((total + int64(params.Limit) - 1) / int64(params.Limit))
	if totalPages == 0 {
		totalPages = 1
	}

	return PaginatedResponse{
		Data: data,
		Pagination: PaginationMeta{
			Page:       params.Page,
			Limit:      params.Limit,
			Total:      total,
			TotalPages: totalPages,
			HasNext:    params.Page < totalPages,
			HasPrev:    params.Page > 1,
		},
	}
}

// GetIDFromURL extracts ID from URL parameter
func GetIDFromURL(r *http.Request, paramName string) (uuid.UUID, error) {
	idStr := chi.URLParam(r, paramName)
	if idStr == "" {
		return uuid.Nil, ErrInvalidID
	}

	id, err := uuid.Parse(idStr)
	if err != nil {
		return uuid.Nil, ErrInvalidID
	}

	return id, nil
}

// ErrInvalidID is returned when ID parameter is invalid
var ErrInvalidID = &ValidationError{Field: "id", Message: "invalid ID parameter"}

// ValidationError represents a validation error
type ValidationError struct {
	Field   string
	Message string
}

func (e *ValidationError) Error() string {
	return e.Message
}
