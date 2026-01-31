package project

import (
	"asset-server/generated/api"
	"asset-server/generated/db"
	"context"
	"errors"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
)

type Service interface {
	CreateProject(
		ctx context.Context,
		name string,
		updateProtocol api.UpdateProtocol,
	) (*db.Project, error)
	ProjectByID(ctx context.Context, id uuid.UUID) (*db.Project, error)
}

type service struct {
	q *db.Queries
}

func NewService(q *db.Queries) Service {
	return &service{q}
}

func (s *service) CreateProject(
	ctx context.Context,
	name string,
	updateProtocol api.UpdateProtocol,
) (*db.Project, error) {
	project, err := s.q.CreateProject(
		ctx,
		uuid.Must(uuid.NewV7()),
		name,
		db.UpdateProtocol(updateProtocol),
	)
	if err != nil {
		return nil, err
	}

	return &project, nil
}

func (s *service) ProjectByID(ctx context.Context, id uuid.UUID) (*db.Project, error) {
	project, err := s.q.GetProjectById(ctx, id)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}

		return nil, err
	}

	return &project, nil
}
