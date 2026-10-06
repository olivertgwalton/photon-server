package model

import (
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type TaskState struct {
	Key         domain.TaskKey
	StartedAt   time.Time
	FinishedAt  *time.Time
	Result      *domain.TaskResult
	Error       *string
	RequestedAt *time.Time
}
