package companies_models

import (
	"errors"
	"time"
)

var ErrNameRequired = errors.New("name is required")

type Company struct {
	ID        int64
	UserID    int64
	Name      string
	Website   *string
	Location  *string
	Notes     *string
	CreatedAt time.Time
	UpdatedAt time.Time
}

func (c *Company) Validate() error {
	if c.Name == "" {
		return ErrNameRequired
	}
	return nil
}
