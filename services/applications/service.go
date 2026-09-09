package applications_service

import (
	"context"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	applications_models "github.com/annakonn200059/job-tracker-api/domains/applications"
	apperr "github.com/annakonn200059/job-tracker-api/domains/errors"
	events_models "github.com/annakonn200059/job-tracker-api/domains/events"
	"github.com/annakonn200059/job-tracker-api/internal/infrastructure/database"
	apprepo "github.com/annakonn200059/job-tracker-api/repos/applications"
	events_repo "github.com/annakonn200059/job-tracker-api/repos/events"
)

// orderGap is the spacing between cards. Large enough that midpoints stay
// well clear of float precision for a long time.
const orderGap = 1000.0

// minGapBeforeRenumber: below this, midpoints stop being representable and
// the column is renumbered.
const minGapBeforeRenumber = 0.001

type Service struct {
	pool   *pgxpool.Pool
	apps   *apprepo.Repo
	events *events_repo.Repo
}

func NewService(pool *pgxpool.Pool, apps *apprepo.Repo, events *events_repo.Repo) *Service {
	return &Service{pool: pool, apps: apps, events: events}
}

type Board map[applications_models.Stage][]applications_models.Application

// Board groups every active application into its column. Grouping is
// presentation shape, so it happens here rather than in SQL.
func (s *Service) Board(ctx context.Context, userID int64) (Board, error) {
	apps, err := s.apps.List(ctx, applications_models.Filter{
		UserID: userID,
		Sort:   applications_models.SortBoard,
		Limit:  200,
	})
	if err != nil {
		return nil, err
	}

	b := make(Board, 8)
	for _, a := range apps {
		b[a.Stage] = append(b[a.Stage], a)
	}
	return b, nil
}

func (s *Service) List(ctx context.Context, f applications_models.Filter) ([]applications_models.Application, int64, error) {
	items, err := s.apps.List(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	total, err := s.apps.Count(ctx, f)
	if err != nil {
		return nil, 0, err
	}
	return items, total, nil
}

// Apply creates an application at the bottom of its starting column.
func (s *Service) Apply(ctx context.Context, userID, vacancyID int64, stage applications_models.Stage) (*applications_models.Application, error) {
	if !stage.Valid() {
		return nil, applications_models.ErrInvalidStage
	}

	max, err := s.apps.MaxOrder(ctx, userID, stage)
	if err != nil {
		return nil, err
	}
	order := orderGap
	if max != nil {
		order = *max + orderGap
	}

	a := &applications_models.Application{
		UserID:     userID,
		VacancyID:  vacancyID,
		Stage:      stage,
		BoardOrder: order,
	}
	if stage == applications_models.StageApplied {
		now := time.Now()
		a.AppliedAt = &now
	}

	if err := s.apps.Create(ctx, a); err != nil {
		return nil, err
	}
	return a, nil
}

// CanChangeStage holds the transition rules. Everything is permitted for now
// except no-ops; this is the place to tighten later.
func (s *Service) CanChangeStage(a *applications_models.Application, to applications_models.Stage) error {
	if !to.Valid() {
		return applications_models.ErrInvalidStage
	}
	if a.Stage == to {
		return applications_models.ErrSameStage
	}
	return nil
}

// ChangeStage moves an application and records the transition. Both writes
// happen in one transaction: an application whose history has gaps would make
// the funnel statistics wrong.
func (s *Service) ChangeStage(ctx context.Context, userID, id int64, to applications_models.Stage) (*applications_models.Application, error) {
	var result *applications_models.Application

	err := database.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		apps := s.apps.WithTx(tx)
		events := s.events.WithTx(tx)

		// FOR UPDATE: reading the current stage and writing the new one are
		// two statements. Without the lock, concurrent requests could both
		// read the same "from" value and log contradictory events.
		current, err := apps.GetByIDForUpdate(ctx, userID, id)
		if err != nil {
			return err
		}
		if err := s.CanChangeStage(current, to); err != nil {
			return err
		}

		max, err := apps.MaxOrder(ctx, userID, to)
		if err != nil {
			return err
		}
		order := orderGap
		if max != nil {
			order = *max + orderGap
		}

		appliedAt := current.AppliedAt
		if to == applications_models.StageApplied && appliedAt == nil {
			now := time.Now()
			appliedAt = &now
		}

		var closedAt *time.Time
		if to.IsClosed() {
			now := time.Now()
			closedAt = &now
		}

		updated, err := apps.SetStage(ctx, userID, id, to, order, appliedAt, closedAt)
		if err != nil {
			return err
		}

		from := current.Stage
		if err := events.Create(ctx, &events_models.Event{
			UserID:        userID,
			ApplicationID: id,
			Kind:          events_models.KindStageChange,
			FromStage:     &from,
			ToStage:       &to,
			CreatedBy:     events_models.ByUser,
		}); err != nil {
			return err
		}

		result = updated
		return nil
	})

	return result, err
}

// MoveCard repositions a card between two neighbours. The caller sends the
// IDs of the cards above and below the drop point; the midpoint is computed
// here, and the column is renumbered when gaps grow too small to split.
func (s *Service) MoveCard(ctx context.Context, userID, id int64, stage applications_models.Stage, afterID, beforeID *int64) (*applications_models.Application, error) {
	if !stage.Valid() {
		return nil, applications_models.ErrInvalidStage
	}

	var result *applications_models.Application

	err := database.InTx(ctx, s.pool, func(tx pgx.Tx) error {
		apps := s.apps.WithTx(tx)
		events := s.events.WithTx(tx)

		current, err := apps.GetByIDForUpdate(ctx, userID, id)
		if err != nil {
			return err
		}

		order, err := s.computeOrder(ctx, apps, userID, stage, afterID, beforeID)
		if err != nil {
			return err
		}

		appliedAt, closedAt := current.AppliedAt, current.ClosedAt
		if stage != current.Stage {
			if stage == applications_models.StageApplied && appliedAt == nil {
				now := time.Now()
				appliedAt = &now
			}
			closedAt = nil
			if stage.IsClosed() {
				now := time.Now()
				closedAt = &now
			}
		}

		updated, err := apps.SetStage(ctx, userID, id, stage, order, appliedAt, closedAt)
		if err != nil {
			return err
		}

		// Dragging across columns is a stage change and belongs in history.
		// Reordering within a column is not.
		if stage != current.Stage {
			from := current.Stage
			if err := events.Create(ctx, &events_models.Event{
				UserID:        userID,
				ApplicationID: id,
				Kind:          events_models.KindStageChange,
				FromStage:     &from,
				ToStage:       &stage,
				CreatedBy:     events_models.ByUser,
			}); err != nil {
				return err
			}
		}

		result = updated
		return nil
	})

	return result, err
}

// computeOrder places a card between its neighbours, renumbering the column
// first if the available gap has collapsed.
func (s *Service) computeOrder(ctx context.Context, apps *apprepo.Repo, userID int64, stage applications_models.Stage, afterID, beforeID *int64) (float64, error) {
	prev, next, err := apps.Neighbours(ctx, userID, stage, afterID, beforeID)
	if err != nil {
		return 0, err
	}

	switch {
	case prev == nil && next == nil: // empty column
		return orderGap, nil
	case prev == nil: // dropped at the top
		return *next / 2, nil
	case next == nil: // dropped at the bottom
		return *prev + orderGap, nil
	}

	if *next-*prev < minGapBeforeRenumber {
		if err := apps.Renumber(ctx, userID, stage); err != nil {
			return 0, err
		}
		prev, next, err = apps.Neighbours(ctx, userID, stage, afterID, beforeID)
		if err != nil {
			return 0, err
		}
		if prev == nil || next == nil {
			return orderGap, nil
		}
	}

	return *prev + (*next-*prev)/2, nil
}

func (s *Service) Update(ctx context.Context, userID, id int64, priority int16, notes *string) (*applications_models.Application, error) {
	return s.apps.Update(ctx, userID, id, priority, notes)
}

func (s *Service) Delete(ctx context.Context, userID, id int64) error {
	return s.apps.SoftDelete(ctx, userID, id)
}

func (s *Service) Get(ctx context.Context, userID, id int64) (*applications_models.Application, error) {
	return s.apps.GetByID(ctx, userID, id)
}

var _ = apperr.ErrNotFound // errors flow up from the repo unchanged
