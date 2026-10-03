package service

import (
	"context"
	"fmt"
	"sync"

	"private-release/internal/domain"
	"private-release/internal/privacy"
	"private-release/internal/store"
)

type Service struct {
	db    *store.DB
	locks *keyedLocks
}

type PublicProject struct {
	ID               string       `json:"id"`
	Lower            int64        `json:"lower"`
	Upper            int64        `json:"upper"`
	Bins             []domain.Bin `json:"bins"`
	TotalEpsilon     string       `json:"total_epsilon"`
	SpentEpsilon     string       `json:"spent_epsilon"`
	RemainingEpsilon string       `json:"remaining_epsilon"`
}

type QueryRequest struct {
	Operation string `json:"operation"`
	Epsilon   string `json:"epsilon"`
}

type CountResult struct {
	Count int64 `json:"count"`
}
type SumResult struct {
	Sum int64 `json:"sum"`
}
type HistogramBinResult struct {
	Index          int   `json:"index"`
	Lower          int64 `json:"lower"`
	Upper          int64 `json:"upper"`
	UpperInclusive bool  `json:"upper_inclusive"`
	NoisyCount     int64 `json:"noisy_count"`
}
type HistogramResult struct {
	Bins []HistogramBinResult `json:"bins"`
}

func New(db *store.DB) *Service { return &Service{db: db, locks: newKeyedLocks()} }

func (s *Service) CreateProject(in *domain.CreateProjectInput) error {
	return s.db.CreateProject(in)
}

func (s *Service) Project(id string) (PublicProject, error) {
	p, spent, err := s.db.Project(id)
	if err != nil {
		return PublicProject{}, err
	}
	return PublicProject{
		ID: p.ID, Lower: p.Lower, Upper: p.Upper, Bins: p.Bins,
		TotalEpsilon: p.Budget.String(), SpentEpsilon: spent.String(),
		RemainingEpsilon: p.Budget.Sub(spent).String(),
	}, nil
}

func (s *Service) Query(ctx context.Context, projectID string, req QueryRequest) (store.QueryResult, error) {
	op, err := domain.ParseOperation(req.Operation)
	if err != nil {
		return store.QueryResult{}, err
	}
	epsilon, err := domain.ParseDecimal(req.Epsilon)
	if err != nil {
		return store.QueryResult{}, err
	}
	unlock := s.locks.lock(projectID)
	defer unlock()
	return s.db.Publish(ctx, projectID, op, epsilon, func(records []domain.Record, project store.LoadedProject, eps domain.Decimal) (any, error) {
		switch op {
		case domain.OpCount:
			v, err := privacy.Count(records, eps)
			return CountResult{Count: v}, err
		case domain.OpSum:
			v, err := privacy.Sum(records, project.Lower, project.Upper, eps)
			return SumResult{Sum: v}, err
		case domain.OpHistogram:
			values, err := privacy.Histogram(records, project.Bins, eps)
			if err != nil {
				return nil, err
			}
			bins := make([]HistogramBinResult, len(project.Bins))
			for i := range project.Bins {
				bins[i] = HistogramBinResult{Index: project.Bins[i].Index, Lower: project.Bins[i].Lower, Upper: project.Bins[i].Upper, UpperInclusive: project.Bins[i].UpperInclusive, NoisyCount: values[i]}
			}
			return HistogramResult{Bins: bins}, nil
		default:
			return nil, fmt.Errorf("unsupported operation %q", op)
		}
	})
}

type keyedLocks struct {
	mu    sync.Mutex
	locks map[string]*projectLock
}

type projectLock struct {
	mu sync.Mutex
	n  int
}

func newKeyedLocks() *keyedLocks { return &keyedLocks{locks: make(map[string]*projectLock)} }

func (k *keyedLocks) lock(key string) func() {
	k.mu.Lock()
	pl := k.locks[key]
	if pl == nil {
		pl = &projectLock{}
		k.locks[key] = pl
	}
	pl.n++
	k.mu.Unlock()
	pl.mu.Lock()
	return func() {
		pl.mu.Unlock()
		k.mu.Lock()
		pl.n--
		if pl.n == 0 {
			delete(k.locks, key)
		}
		k.mu.Unlock()
	}
}
