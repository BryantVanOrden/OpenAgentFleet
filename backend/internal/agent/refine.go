package agent

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/bus"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/connectors"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/recorder"
	"github.com/BryantVanOrden/AgentFleet/backend/internal/store"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// Refiner implements the continual harness self-improvement loop inspired by Prime Agent.
// It analyzes task execution trajectories to self-heal fragile selectors, prune redundant steps,
// and synthesize reusable skills from scratch.
type Refiner struct {
	db     *store.Store
	models *connectors.Registry
	bus    *bus.Bus
	log    *slog.Logger
}

func NewRefiner(db *store.Store, models *connectors.Registry, bus *bus.Bus, log *slog.Logger) *Refiner {
	return &Refiner{db: db, models: models, bus: bus, log: log}
}

type refineOutput struct {
	Name            string               `json:"name"`
	Description     string               `json:"description"`
	Params          []string             `json:"params"`
	Steps           []protocol.SkillStep `json:"steps"`
	RefinementNotes string               `json:"refinement_notes"`
}

const refineSystemPrompt = `You are an expert AI workflow optimizer.
Your job is to analyze a completed task execution trace and refine the corresponding SKILL.md.
Identify:
1. Steps that failed or needed retries.
2. Fragile coordinate clicks that can be replaced with stable accessible labels (AT-SPI roles/labels).
3. Window focus transitions or waits that can be made cleaner with wait_for.
4. Opportunities to parameterize hardcoded input values.

Output MUST be a single JSON object with this schema:
{
  "name": "Skill Name",
  "description": "Short description of what the skill achieves",
  "params": ["param1", "param2"],
  "steps": [
    {
      "index": 1,
      "kind": "click|double_click|right_click|type|key|scroll|drag|wait|wait_for|focus|shell|python|assert",
      "window": "window title",
      "role": "push button|text|etc",
      "label": "accessible label",
      "text": "text to type",
      "param": "parameter name if variable",
      "key": "shortcut key",
      "assert": "assertion string"
    }
  ],
  "refinement_notes": "Summary of optimizations made (e.g. replaced coordinate click on step 3 with accessible label 'Submit')"
}`

// RefineSkill inspects a completed task and updates the associated skill with self-healed steps.
func (rf *Refiner) RefineSkill(ctx context.Context, task *protocol.Task, skill *protocol.Skill, steps []protocol.StepRecord) (*protocol.Skill, error) {
	if skill == nil {
		return nil, fmt.Errorf("skill is nil")
	}

	var trace strings.Builder
	fmt.Fprintf(&trace, "GOAL: %s\n", task.Goal)
	fmt.Fprintf(&trace, "ORIGINAL SKILL:\n%s\n\n", skill.Markdown)
	trace.WriteString("EXECUTION TRACE:\n")
	for _, s := range steps {
		fmt.Fprintf(&trace, "Step %d: Action=%s, Target=%q, Coords=%v, Outcome=%s\n",
			s.Step, s.Action.Action, s.Action.Target, s.Action.Coordinates, s.Outcome)
	}

	resp, err := rf.models.Complete(ctx, task.ProviderID, connectors.Request{
		System:   refineSystemPrompt,
		JSONOnly: true,
		Messages: []connectors.Message{{
			Role: connectors.RoleUser,
			Text: trace.String(),
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("model refinement failed: %w", err)
	}

	body := extractJSON(resp.Text)
	if body == "" {
		return nil, fmt.Errorf("no JSON returned from refinement: %s", clip(resp.Text, 200))
	}

	var out refineOutput
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, fmt.Errorf("parse refinement output: %w", err)
	}

	if out.Name != "" {
		skill.Name = out.Name
	}
	if out.Description != "" {
		skill.Description = out.Description
	}
	if len(out.Params) > 0 {
		skill.Params = out.Params
	}
	if len(out.Steps) > 0 {
		for i := range out.Steps {
			out.Steps[i].Index = i + 1
		}
		skill.Steps = out.Steps
	}
	skill.Version++
	skill.RefinementNotes = out.RefinementNotes
	skill.Markdown = recorder.Render(skill)
	skill.UpdatedAt = time.Now().UTC()

	if err := rf.db.UpsertSkill(ctx, skill); err != nil {
		return nil, fmt.Errorf("save refined skill: %w", err)
	}

	rf.bus.Emit("skill.refined", task.InstanceID, task.ID, map[string]any{
		"skill_id": skill.ID, "version": skill.Version, "notes": skill.RefinementNotes,
	})
	rf.log.Info("skill refined successfully", "skill_id", skill.ID, "version", skill.Version, "notes", skill.RefinementNotes)
	return skill, nil
}

// SynthesizeSkill creates a brand new reusable skill from a scratch task execution trace.
func (rf *Refiner) SynthesizeSkill(ctx context.Context, task *protocol.Task, steps []protocol.StepRecord) (*protocol.Skill, error) {
	var trace strings.Builder
	fmt.Fprintf(&trace, "GOAL: %s\n", task.Goal)
	trace.WriteString("EXECUTION TRACE (executed without prior skill demonstration):\n")
	for _, s := range steps {
		fmt.Fprintf(&trace, "Step %d: Action=%s, Target=%q, Coords=%v, Text=%q, Outcome=%s\n",
			s.Step, s.Action.Action, s.Action.Target, s.Action.Coordinates, s.Action.Text, s.Outcome)
	}

	resp, err := rf.models.Complete(ctx, task.ProviderID, connectors.Request{
		System:   refineSystemPrompt,
		JSONOnly: true,
		Messages: []connectors.Message{{
			Role: connectors.RoleUser,
			Text: trace.String(),
		}},
	})
	if err != nil {
		return nil, fmt.Errorf("model skill synthesis failed: %w", err)
	}

	body := extractJSON(resp.Text)
	if body == "" {
		return nil, fmt.Errorf("no JSON returned from synthesis: %s", clip(resp.Text, 200))
	}

	var out refineOutput
	if err := json.Unmarshal([]byte(body), &out); err != nil {
		return nil, fmt.Errorf("parse synthesized skill output: %w", err)
	}

	name := out.Name
	if name == "" {
		name = task.Goal
	}
	for i := range out.Steps {
		out.Steps[i].Index = i + 1
	}

	skill := &protocol.Skill{
		ID:              store.NewID(),
		Name:            name,
		Description:     out.Description,
		Params:          out.Params,
		Steps:           out.Steps,
		SourceRunID:     task.ID,
		Version:         1,
		RefinementNotes: "Synthesized from autonomous task execution",
		CreatedAt:       time.Now().UTC(),
		UpdatedAt:       time.Now().UTC(),
	}
	skill.Markdown = recorder.Render(skill)

	if err := rf.db.UpsertSkill(ctx, skill); err != nil {
		return nil, fmt.Errorf("save synthesized skill: %w", err)
	}

	rf.bus.Emit("skill.synthesized", task.InstanceID, task.ID, map[string]any{
		"skill_id": skill.ID, "name": skill.Name,
	})
	rf.log.Info("skill synthesized successfully", "skill_id", skill.ID, "name", skill.Name)
	return skill, nil
}
