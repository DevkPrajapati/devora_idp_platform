package pagination

import idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"

const (
	DefaultPageSize = 20
	MaxPageSize     = 100
)

// Normalize ensures page and page size are within valid bounds.
func Normalize(page, pageSize int32) (int32, int32) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = DefaultPageSize
	}
	if pageSize > MaxPageSize {
		pageSize = MaxPageSize
	}
	return page, pageSize
}

// Offset calculates the SQL offset from page and page size.
func Offset(page, pageSize int32) int32 {
	return (page - 1) * pageSize
}

// PageInfo builds pagination metadata.
func PageInfo(page, pageSize int32, total int64) *idpv1.PageInfo {
	totalPages := int32(0)
	if pageSize > 0 {
		totalPages = int32((total + int64(pageSize) - 1) / int64(pageSize))
	}
	return &idpv1.PageInfo{
		Page:       page,
		PageSize:   pageSize,
		TotalCount: total,
		TotalPages: totalPages,
	}
}
