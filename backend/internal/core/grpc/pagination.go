package grpc

import coreapi "github.com/callmerussell04/docker-cloud-manager/api/core"

func pagination(req *coreapi.PaginationRequest) (int, int) {
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
