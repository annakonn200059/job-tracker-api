package applications_models

import (
	"errors"
	"time"
)

var (
	ErrAlreadyApplied = errors.New("already applied to this vacancy")
	ErrInvalidStage   = errors.New("invalid stage")
	ErrSameStage      = errors.New("application is already in this stage")
	ErrInvalidSort    = errors.New("invalid sort field")
)

type Stage string

const (
	StageSaved     Stage = "saved"
	StageApplied   Stage = "applied"
	StageScreening Stage = "screening"
	StageInterview Stage = "interview"
	StageFinal     Stage = "final"
	StageOffer     Stage = "offer"
	StageRejected  Stage = "rejected"
	StageWithdrawn Stage = "withdrawn"
)

func (s Stage) Valid() bool {
	switch s {
	case StageSaved, StageApplied, StageScreening, StageInterview,
		StageFinal, StageOffer, StageRejected, StageWithdrawn:
		return true
	}
	return false
}

// IsClosed reports whether the stage ends the pipeline.
func (s Stage) IsClosed() bool {
	return s == StageOffer || s == StageRejected || s == StageWithdrawn
}

type Application struct {
	ID         int64
	UserID     int64
	VacancyID  int64
	Stage      Stage
	BoardOrder float64
	Priority   int16
	AppliedAt  *time.Time
	ClosedAt   *time.Time
	Notes      *string
	CreatedAt  time.Time
	UpdatedAt  time.Time
}
