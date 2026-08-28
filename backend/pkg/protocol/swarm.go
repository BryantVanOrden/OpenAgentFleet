package protocol

import "time"

// SwarmStatus represents the lifecycle state of a collaborative multi-agent swarm.
type SwarmStatus string

const (
	SwarmStatusInitializing SwarmStatus = "initializing"
	SwarmStatusRunning      SwarmStatus = "running"
	SwarmStatusReviewing    SwarmStatus = "reviewing"
	SwarmStatusCompleted    SwarmStatus = "completed"
	SwarmStatusFailed       SwarmStatus = "failed"
)

// SwarmMember represents an individual bot instance assigned to a role in the swarm.
type SwarmMember struct {
	InstanceID   string `json:"instance_id"`
	InstanceName string `json:"instance_name"`
	Role         string `json:"role"`         // e.g. "Lead Architect", "QA Auditor", "Red Teamer", "CRM Analyst"
	ArchetypeID  string `json:"archetype_id"` // e.g. "fullstack_dev", "qa_ui_ux", "cyber_ops"
	Status       string `json:"status"`       // "idle", "working", "done", "error"
}

// SwarmMessage is an inter-bot communication exchanged across the shared blackboard.
type SwarmMessage struct {
	ID        string    `json:"id"`
	SwarmID   string    `json:"swarm_id"`
	FromBot   string    `json:"from_bot"` // Member Instance Name/Role
	ToBot     string    `json:"to_bot"`   // Target Member or "all"
	Phase     string    `json:"phase"`    // "planning", "execution", "qa", "security", "handoff"
	Content   string    `json:"content"`
	Artifacts []string  `json:"artifacts,omitempty"`
	CreatedAt time.Time `json:"created_at"`
}

// SwarmArtifact represents a verified deliverable produced by a swarm member.
type SwarmArtifact struct {
	ID         string    `json:"id"`
	SwarmID    string    `json:"swarm_id"`
	Title      string    `json:"title"`
	Author     string    `json:"author"`
	Category   string    `json:"category"` // "code_patch", "test_report", "security_audit", "crm_memo"
	Content    string    `json:"content"`
	ApprovedBy []string  `json:"approved_by,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}

// SwarmTeam defines a multi-agent collaborative mission.
type SwarmTeam struct {
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Mission   string          `json:"mission"`
	Status    SwarmStatus     `json:"status"`
	Members   []SwarmMember   `json:"members"`
	Messages  []SwarmMessage  `json:"messages"`
	Artifacts []SwarmArtifact `json:"artifacts"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
}
