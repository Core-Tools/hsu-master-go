package domain

import (
	"context"

	"github.com/core-tools/hsu-master/pkg/domain"
	"github.com/core-tools/hsu-master/pkg/logging"
)

func NewMasterHandler(logger logging.Logger) domain.Contract {
	return &masterHandler{
		logger: logger,
	}
}

type masterHandler struct {
	logger logging.Logger
}

func (h *masterHandler) Status(ctx context.Context) (string, error) {
	return "hsu-master: OK", nil
}
