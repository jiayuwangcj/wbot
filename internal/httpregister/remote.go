package httpregister

import (
	"context"

	"github.com/jiayu/wbot/internal/master"
)

// RemoteFacade adapts Client to master.Facade for poll.Run and tests.
type RemoteFacade struct {
	Client *Client
	// Ctx is used for each Register call; if nil, context.Background is used.
	Ctx context.Context
}

var _ master.Facade = (*RemoteFacade)(nil)

// Register implements master.Facade.
func (r *RemoteFacade) Register(agentID string) bool {
	ctx := r.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	newID, err := r.Client.Register(ctx, agentID)
	if err != nil {
		return false
	}
	return newID
}

// Agents implements master.Facade via GET /v1/agents when Client is configured.
func (r *RemoteFacade) Agents() []string {
	ctx := r.Ctx
	if ctx == nil {
		ctx = context.Background()
	}
	if r.Client == nil {
		return nil
	}
	ids, err := r.Client.ListAgents(ctx)
	if err != nil {
		return nil
	}
	return ids
}
