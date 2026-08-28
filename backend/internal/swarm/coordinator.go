package swarm

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Coordinator manages active multi-agent swarms, message routing, and blackboard state.
type Coordinator struct {
	mu     sync.RWMutex
	swarms map[string]*protocol.SwarmTeam
}

func NewCoordinator() *Coordinator {
	return &Coordinator{
		swarms: make(map[string]*protocol.SwarmTeam),
	}
}

// CreateSwarm initializes a collaborative swarm with assigned members.
func (c *Coordinator) CreateSwarm(ctx context.Context, name, mission string, members []protocol.SwarmMember) (*protocol.SwarmTeam, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	id := fmt.Sprintf("swarm-%s-%d", time.Now().Format("200601021504"), len(c.swarms)+1)
	now := time.Now().UTC()

	swarm := &protocol.SwarmTeam{
		ID:        id,
		Name:      name,
		Mission:   mission,
		Status:    protocol.SwarmStatusInitializing,
		Members:   members,
		Messages:  make([]protocol.SwarmMessage, 0),
		Artifacts: make([]protocol.SwarmArtifact, 0),
		CreatedAt: now,
		UpdatedAt: now,
	}

	// Initial blackboard kickoff message
	swarm.Messages = append(swarm.Messages, protocol.SwarmMessage{
		ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		SwarmID:   id,
		FromBot:   "Mission Coordinator",
		ToBot:     "all",
		Phase:     "planning",
		Content:   fmt.Sprintf("🚀 Swarm mission initialized: %s. All %d bots reporting for collaborative execution.", mission, len(members)),
		CreatedAt: now,
	})

	swarm.Status = protocol.SwarmStatusRunning
	c.swarms[id] = swarm
	return swarm, nil
}

// PostMessage sends an inter-bot communication across the swarm blackboard.
func (c *Coordinator) PostMessage(ctx context.Context, swarmID, fromBot, toBot, phase, content string, artifacts []string) (*protocol.SwarmMessage, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	swarm, ok := c.swarms[swarmID]
	if !ok {
		return nil, fmt.Errorf("swarm %s not found", swarmID)
	}

	msg := protocol.SwarmMessage{
		ID:        fmt.Sprintf("msg-%d", time.Now().UnixNano()),
		SwarmID:   swarmID,
		FromBot:   fromBot,
		ToBot:     toBot,
		Phase:     phase,
		Content:   content,
		Artifacts: artifacts,
		CreatedAt: time.Now().UTC(),
	}

	swarm.Messages = append(swarm.Messages, msg)
	swarm.UpdatedAt = time.Now().UTC()
	return &msg, nil
}

// PublishArtifact posts a verified deliverable to the swarm blackboard.
func (c *Coordinator) PublishArtifact(ctx context.Context, swarmID, title, author, category, content string) (*protocol.SwarmArtifact, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	swarm, ok := c.swarms[swarmID]
	if !ok {
		return nil, fmt.Errorf("swarm %s not found", swarmID)
	}

	art := protocol.SwarmArtifact{
		ID:        fmt.Sprintf("art-%d", time.Now().UnixNano()),
		SwarmID:   swarmID,
		Title:     title,
		Author:    author,
		Category:  category,
		Content:   content,
		CreatedAt: time.Now().UTC(),
	}

	swarm.Artifacts = append(swarm.Artifacts, art)
	swarm.UpdatedAt = time.Now().UTC()
	return &art, nil
}

// ListSwarms returns all active and historical swarms.
func (c *Coordinator) ListSwarms(ctx context.Context) []*protocol.SwarmTeam {
	c.mu.RLock()
	defer c.mu.RUnlock()

	out := make([]*protocol.SwarmTeam, 0, len(c.swarms))
	for _, s := range c.swarms {
		out = append(out, s)
	}
	return out
}

// GetSwarm returns details for a specific swarm ID.
func (c *Coordinator) GetSwarm(ctx context.Context, id string) (*protocol.SwarmTeam, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()

	swarm, ok := c.swarms[id]
	if !ok {
		return nil, fmt.Errorf("swarm %s not found", id)
	}
	return swarm, nil
}
