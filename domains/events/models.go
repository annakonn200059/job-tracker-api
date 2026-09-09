package events_models

import (
	"time"

	applications_models "github.com/annakonn200059/job-tracker-api/domains/applications"
)

type Kind string

const (
	KindStageChange   Kind = "stage_change"
	KindEmailSent     Kind = "email_sent"
	KindEmailReceived Kind = "email_received"
	KindCall          Kind = "call"
	KindInterview     Kind = "interview"
	KindTask          Kind = "task"
	KindNote          Kind = "note"
)

func (k Kind) Valid() bool {
	switch k {
	case KindStageChange, KindEmailSent, KindEmailReceived,
		KindCall, KindInterview, KindTask, KindNote:
		return true
	}
	return false
}

type Author string

const (
	ByUser   Author = "user"
	ByAI     Author = "ai"
	BySystem Author = "system"
)

// Event is an immutable record.
// the table is the source of truth for funnel analytics
type Event struct {
	ID            int64
	UserID        int64
	ApplicationID int64
	ContactID     *int64
	Kind          Kind
	FromStage     *applications_models.Stage
	ToStage       *applications_models.Stage
	Title         *string
	Body          *string
	CreatedBy     Author
	OccurredAt    time.Time
	CreatedAt     time.Time
}
