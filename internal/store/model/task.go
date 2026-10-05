package model

import (
	"time"

	"github.com/olivertgwalton/photon-server/internal/domain"
)

type TaskState struct {
	Key        domain.TaskKey `gorm:"primaryKey"`
	StartedAt  time.Time
	FinishedAt *time.Time
	Result     *domain.TaskResult
	Error      *string
}

func (TaskState) TableName() string { return "task_state" }
