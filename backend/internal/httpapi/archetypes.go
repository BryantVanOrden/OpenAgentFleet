package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/fleet"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/internal/mcp"
	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Archetype export and import.
//
// This existed only in the SDK, and only half of it: `fleetctl hub export` built
// a manifest from the template catalogue with tools and recorded skills left
// empty, wrote it as .agentfleet.json while the docs and the module docstring
// both said .agentfleet.yaml, and `fleetctl hub import` read the file back and
// printed a summary. It created nothing. There was no endpoint behind it and no
// UI, so an archetype could be "exported" and never installed anywhere.
//
// The endpoints are here because the interesting content is here: the skills a
// fleet has recorded and the MCP servers it has registered are what make an
// archetype worth moving between installations, and neither is knowable from the
// SDK's side.

// ArchetypeManifest is the portable package.
//
// The shape matches the SDK's dataclass so a file written by either side loads
// on the other. Version is the manifest format, not the archetype's own version.
type ArchetypeManifest struct {
	Version            string              `json:"version" yaml:"version"`
	ID                 string              `json:"id" yaml:"id"`
	Name               string              `json:"name" yaml:"name"`
	Tagline            string              `json:"tagline" yaml:"tagline"`
	Category           string              `json:"category" yaml:"category"`
	Icon               string              `json:"icon,omitempty" yaml:"icon,omitempty"`
	RecommendedTier    string              `json:"recommended_tier" yaml:"recommended_tier"`
	VCPU               float64             `json:"vcpu" yaml:"vcpu"`
	MemoryMB           int64               `json:"memory_mb" yaml:"memory_mb"`
	DiskGB             int64               `json:"disk_gb" yaml:"disk_gb"`
	GPU                bool                `json:"gpu" yaml:"gpu"`
	PreinstalledTools  []string            `json:"preinstalled_tools" yaml:"preinstalled_tools"`
	PreinstalledRepos  []string            `json:"preinstalled_repos,omitempty" yaml:"preinstalled_repos,omitempty"`
	SystemPrompt       string              `json:"system_prompt" yaml:"system_prompt"`
	DefaultVoice       string              `json:"default_voice,omitempty" yaml:"default_voice,omitempty"`
	DefaultShellAccess bool                `json:"default_shell_access,omitempty" yaml:"default_shell_access,omitempty"`
	DefaultEnvironment map[string]string   `json:"default_environment" yaml:"default_environment"`
	MCPServers         []ManifestMCPServer `json:"mcp_servers" yaml:"mcp_servers"`
	RecordedSkills     []protocol.Skill    `json:"recorded_skills" yaml:"recorded_skills"`
}

// ManifestMCPServer is an MCP registration without its credentials.
//
// Env is deliberately reduced to the key names. A manifest is a file people mail
// each other and commit to repositories, and the env map on an MCP server row is
// where an operator puts an API key — exporting it would turn "share your
// archetype" into "publish your credentials". The importer recreates the server
// with the keys listed and no values, so it is obvious what has to be filled in.
type ManifestMCPServer struct {
	Name      string   `json:"name" yaml:"name"`
	Transport string   `json:"transport" yaml:"transport"`
	Command   string   `json:"command,omitempty" yaml:"command,omitempty"`
	Args      []string `json:"args,omitempty" yaml:"args,omitempty"`
	URL       string   `json:"url,omitempty" yaml:"url,omitempty"`
	EnvKeys   []string `json:"env_keys,omitempty" yaml:"env_keys,omitempty"`
}

// handleExportArchetype builds a manifest for one archetype.
//
// Skills and MCP servers are actually included, which is the half the SDK left
// empty: a manifest carrying only the hardware profile and the prompt is a
// description of an archetype rather than a package of one.
func (s *Server) handleExportArchetype(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	tmpl := protocol.BotTemplateByID(id)
	if tmpl == nil {
		fail(w, http.StatusNotFound, "bot template not found")
		return
	}

	manifest := ArchetypeManifest{
		Version:            "1.0.0",
		ID:                 tmpl.ID,
		Name:               tmpl.Name,
		Tagline:            tmpl.Tagline,
		Category:           tmpl.Category,
		Icon:               tmpl.Icon,
		RecommendedTier:    string(tmpl.RecommendedTier),
		VCPU:               tmpl.VCPU,
		MemoryMB:           tmpl.MemoryMB,
		DiskGB:             tmpl.DiskGB,
		GPU:                tmpl.GPU,
		PreinstalledTools:  orEmptyStrings(tmpl.PreinstalledTools),
		PreinstalledRepos:  tmpl.PreinstalledRepos,
		SystemPrompt:       tmpl.SpecializedPrompt,
		DefaultVoice:       tmpl.DefaultVoice,
		DefaultShellAccess: tmpl.DefaultShellAccess,
		DefaultEnvironment: orEmptyMap(tmpl.DefaultEnvironment),
		MCPServers:         []ManifestMCPServer{},
		RecordedSkills:     []protocol.Skill{},
	}

	// The fleet's recorded skills. Optionally filtered, because a fleet with
	// fifty skills does not want all of them in every archetype package.
	if s.db != nil {
		skills, err := s.db.ListSkills(r.Context())
		if err != nil {
			failErr(w, err)
			return
		}
		wanted := splitCSV(r.URL.Query().Get("skills"))
		for _, sk := range skills {
			if len(wanted) > 0 && !wanted[sk.ID] && !wanted[sk.Name] {
				continue
			}
			manifest.RecordedSkills = append(manifest.RecordedSkills, sk)
		}
	}

	// Registered MCP servers, credentials stripped.
	for _, srv := range mcp.GlobalMCP.ListServers(r.Context()) {
		manifest.MCPServers = append(manifest.MCPServers, ManifestMCPServer{
			Name:      srv.Name,
			Transport: srv.Transport,
			Command:   srv.Command,
			Args:      srv.Args,
			URL:       srv.URL,
			// EnvKeys, never Env: ListServers already redacts the values, and
			// this is the shape that says "you will need these" without saying
			// what they were.
			EnvKeys: srv.EnvKeys,
		})
	}

	// Content-Disposition so a browser saves it with the documented name rather
	// than calling it "export".
	w.Header().Set("Content-Disposition",
		fmt.Sprintf("attachment; filename=%q", tmpl.ID+".agentfleet.json"))
	writeJSON(w, http.StatusOK, manifest)
}

// ImportArchetypeResult reports what an import actually did.
//
// Itemised because "imported successfully" is what the SDK printed while
// creating nothing, and the useful question is which parts landed.
type ImportArchetypeResult struct {
	Archetype      string   `json:"archetype"`
	SkillsCreated  []string `json:"skills_created"`
	SkillsSkipped  []string `json:"skills_skipped"`
	MCPRegistered  []string `json:"mcp_registered"`
	MCPFailed      []string `json:"mcp_failed"`
	NeedsSecrets   []string `json:"needs_secrets,omitempty"`
	InstanceID     string   `json:"instance_id,omitempty"`
	InstanceName   string   `json:"instance_name,omitempty"`
	InstanceStatus string   `json:"instance_status,omitempty"`
}

type importArchetypeReq struct {
	Manifest ArchetypeManifest `json:"manifest"`
	// Overwrite lets an import replace skills that already exist by name.
	// Off by default: an import must not quietly overwrite a skill a fleet has
	// been refining.
	Overwrite bool `json:"overwrite"`
	// CreateInstance provisions a bot from the manifest straight away.
	CreateInstance bool   `json:"create_instance"`
	InstanceName   string `json:"instance_name,omitempty"`
}

// handleImportArchetype installs a manifest.
//
// This is what did not exist. `fleetctl hub import` loaded the file, printed
// three lines about it, and returned — so importing an archetype left the fleet
// exactly as it was.
func (s *Server) handleImportArchetype(w http.ResponseWriter, r *http.Request) {
	var req importArchetypeReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 8<<20)).Decode(&req); err != nil {
		fail(w, http.StatusBadRequest, "invalid request body")
		return
	}
	m := req.Manifest
	if strings.TrimSpace(m.ID) == "" || strings.TrimSpace(m.Name) == "" {
		fail(w, http.StatusBadRequest, "a manifest needs an id and a name")
		return
	}

	res := ImportArchetypeResult{
		Archetype:     m.ID,
		SkillsCreated: []string{},
		SkillsSkipped: []string{},
		MCPRegistered: []string{},
		MCPFailed:     []string{},
	}

	// Skills first: they are the part with no side effects beyond the database,
	// so a failure later leaves the fleet in a state the operator can read.
	if s.db != nil {
		existing, err := s.db.ListSkills(r.Context())
		if err != nil {
			failErr(w, err)
			return
		}
		byName := make(map[string]bool, len(existing))
		byID := make(map[string]bool, len(existing))
		for _, sk := range existing {
			byName[sk.Name] = true
			byID[sk.ID] = true
		}

		for _, sk := range m.RecordedSkills {
			if strings.TrimSpace(sk.Name) == "" {
				continue
			}
			if (byName[sk.Name] || byID[sk.ID]) && !req.Overwrite {
				// Skipped, not overwritten. A skill a fleet has been refining
				// for months should not be replaced by whatever was in a file
				// someone mailed over.
				res.SkillsSkipped = append(res.SkillsSkipped, sk.Name)
				continue
			}
			if err := s.db.UpsertSkill(r.Context(), &sk); err != nil {
				s.logger().Warn("archetype import: skill not created",
					"skill", sk.Name, "err", err)
				res.SkillsSkipped = append(res.SkillsSkipped, sk.Name)
				continue
			}
			res.SkillsCreated = append(res.SkillsCreated, sk.Name)
		}
	}

	// MCP servers. Registering opens a real connection, so one that cannot be
	// reached is reported rather than stored as configured-and-broken.
	for _, srv := range m.MCPServers {
		if len(srv.EnvKeys) > 0 {
			// The values were deliberately not exported. Say which ones the
			// operator has to supply instead of failing with an auth error.
			for _, k := range srv.EnvKeys {
				res.NeedsSecrets = append(res.NeedsSecrets, srv.Name+"."+k)
			}
			res.MCPFailed = append(res.MCPFailed, srv.Name+
				" (needs credentials: "+strings.Join(srv.EnvKeys, ", ")+")")
			continue
		}
		if _, err := mcp.GlobalMCP.RegisterServer(r.Context(), protocol.MCPServer{
			Name:      srv.Name,
			Transport: srv.Transport,
			Command:   srv.Command,
			Args:      srv.Args,
			URL:       srv.URL,
		}); err != nil {
			res.MCPFailed = append(res.MCPFailed, srv.Name+" ("+err.Error()+")")
			continue
		}
		res.MCPRegistered = append(res.MCPRegistered, srv.Name)
	}

	// Optionally provision a bot from it, which is the point of importing one.
	if req.CreateInstance {
		name := strings.TrimSpace(req.InstanceName)
		if name == "" {
			name = m.Name
		}
		inst, err := s.instanceFromManifest(r.Context(), m, name)
		if err != nil {
			// Not fatal to the whole import: the skills and servers above did
			// land, and reporting them as lost would be wrong.
			res.InstanceStatus = "not created: " + err.Error()
		} else {
			res.InstanceID = inst.ID
			res.InstanceName = inst.Name
			res.InstanceStatus = string(inst.State)
		}
	}

	writeJSON(w, http.StatusOK, res)
}

// instanceFromManifest provisions a bot described by a manifest.
//
// The hardware numbers come across as an explicit override rather than being
// left to the tier, because the whole reason a manifest carries vcpu/memory/disk
// is that the archetype's author decided the tier defaults were not right for it.
func (s *Server) instanceFromManifest(ctx context.Context, m ArchetypeManifest, name string) (*protocol.Instance, error) {
	tier := protocol.Tier(m.RecommendedTier)
	if strings.TrimSpace(m.RecommendedTier) == "" {
		tier = protocol.TierStandard
	}

	override := &protocol.ResourceOverride{}
	if m.VCPU > 0 {
		v := m.VCPU
		override.VCPU = &v
	}
	if m.MemoryMB > 0 {
		v := m.MemoryMB
		override.MemoryMB = &v
	}
	if m.DiskGB > 0 {
		v := m.DiskGB
		override.DiskGB = &v
	}
	if m.GPU {
		v := true
		override.GPU = &v
	}

	req := fleet.CreateRequest{
		Name:              name,
		ArchetypeID:       m.ID,
		SystemPrompt:      m.SystemPrompt,
		PreinstalledTools: m.PreinstalledTools,
		Tier:              tier,
		Override:          override,
		ShellAccess:       m.DefaultShellAccess,
		// Never from a manifest. Sudo is granted deliberately, per bot, by
		// someone who meant to — not by a file that arrived from elsewhere.
		SudoAccess: false,
		Voice:      m.DefaultVoice,
		OwnerID:    userFrom(ctx).Subject,
	}
	return s.fleet.Create(ctx, req)
}

func splitCSV(v string) map[string]bool {
	out := map[string]bool{}
	for _, part := range strings.Split(v, ",") {
		if p := strings.TrimSpace(part); p != "" {
			out[p] = true
		}
	}
	return out
}

func orEmptyStrings(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func orEmptyMap(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	return in
}
