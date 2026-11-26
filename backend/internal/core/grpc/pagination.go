package grpc

// pageRequest is a list request's page and page size.
type pageRequest interface {
	GetPage() int32
	GetLimit() int32
}

func pagination(req pageRequest) (int, int) {
	limit := int(req.GetLimit())
	if limit <= 0 || limit > 100 {
		limit = 20
	}
	offset := (int(req.GetPage()) - 1) * limit
	if offset < 0 {
		offset = 0
	}
	return limit, offset
}
