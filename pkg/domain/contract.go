package domain

import (
	"context"
)

type Contract interface {
	Status(ctx context.Context) (string, error)
}
