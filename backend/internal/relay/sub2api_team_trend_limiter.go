package relay

import (
	"context"
	"sync"
)

const maxConcurrentTeamTrendOrigins = 24

type teamTrendOriginLimiter struct {
	once  sync.Once
	slots chan struct{}
}

func (l *teamTrendOriginLimiter) Do(
	ctx context.Context,
	load func(context.Context) ([]UsageTrendPoint, error),
) ([]UsageTrendPoint, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	l.once.Do(func() {
		l.slots = make(chan struct{}, maxConcurrentTeamTrendOrigins)
	})

	select {
	case l.slots <- struct{}{}:
		defer func() { <-l.slots }()
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		return load(ctx)
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}
