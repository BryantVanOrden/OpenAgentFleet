/// Wire models. Hand-written rather than generated: the payloads are small,
/// stable, and shared with `backend/pkg/protocol/types.go`, which is the actual
/// source of truth.
library;

class TierProfile {
  TierProfile({
    required this.name,
    required this.vcpu,
    required this.memoryMb,
    required this.diskGb,
    required this.gpu,
    this.description = '',
  });

  final String name;
  final double vcpu;
  final int memoryMb;
  final int diskGb;
  final bool gpu;
  final String description;

  factory TierProfile.fromJson(Map<String, dynamic> j) => TierProfile(
        name: j['name'] as String? ?? 'standard',
        vcpu: (j['vcpu'] as num?)?.toDouble() ?? 1,
        memoryMb: (j['memory_mb'] as num?)?.toInt() ?? 2048,
        diskGb: (j['disk_gb'] as num?)?.toInt() ?? 15,
        gpu: j['gpu'] as bool? ?? false,
        description: j['description'] as String? ?? '',
      );
}

class Instance {
  Instance({
    required this.id,
    required this.name,
    required this.tier,
    required this.state,
    required this.profile,
    required this.shellAccess,
    this.sudoAccess = false,
    this.providerIds = const [],
    this.orgIds = const [],
    this.voice = '',
    this.voiceSpeed = 0,
    this.archetypeId = '',
    this.systemPrompt = '',
    required this.createdAt,
    this.lastError = '',
  });

  final String id;
  final String name;
  final String tier;
  final String state;
  final TierProfile profile;
  final bool shellAccess;

  /// Whether sudo works inside this sandbox.
  final bool sudoAccess;

  /// This bot's own model fallback chain, most preferred first. Empty means
  /// the fleet-wide order.
  final List<String> providerIds;

  /// The departments this bot belongs to. Empty is unassigned, which only a
  /// deployment administrator can see.
  ///
  /// Several, because a bot two teams both rely on used to have to be filed
  /// under one of them and be invisible to the other. A member of any of
  /// these can reach it.
  final List<String> orgIds;

  /// Voice this agent speaks in. Empty uses the app-wide default. Per agent so
  /// a fleet is legible by ear rather than every bot sounding identical.
  final String voice;

  /// How fast this agent talks, as a multiplier. 0 uses the app-wide default.
  /// Two bots sharing a voice are still told apart by pace.
  final double voiceSpeed;

  /// Which archetype this bot was built from. Drives the default personality.
  final String archetypeId;

  /// This bot's personality, in its own words. Empty means it was created
  /// before personalities were editable and falls back to its archetype's.
  final String systemPrompt;

  final DateTime createdAt;
  final String lastError;

  bool get isRunning => state == 'running';

  factory Instance.fromJson(Map<String, dynamic> j) => Instance(
        id: j['id'] as String,
        name: j['name'] as String? ?? '',
        tier: j['tier'] as String? ?? 'standard',
        state: j['state'] as String? ?? 'stopped',
        profile: TierProfile.fromJson(
          (j['profile'] as Map?)?.cast<String, dynamic>() ?? const {},
        ),
        shellAccess: j['shell_access'] as bool? ?? false,
        sudoAccess: j['sudo_access'] as bool? ?? false,
        providerIds: ((j['provider_ids'] as List?) ?? const [])
            .map((e) => '$e')
            .toList(growable: false),
        orgIds: ((j['org_ids'] as List?) ?? const [])
            .map((e) => '$e')
            .toList(growable: false),
        voice: j['voice'] as String? ?? '',
        voiceSpeed: (j['voice_speed'] as num?)?.toDouble() ?? 0,
        archetypeId: j['archetype_id'] as String? ?? '',
        systemPrompt: j['system_prompt'] as String? ?? '',
        createdAt: DateTime.tryParse(j['created_at'] as String? ?? '') ??
            DateTime.now(),
        lastError: j['last_error'] as String? ?? '',
      );
}

class InstanceStats {
  InstanceStats({
    required this.instanceId,
    required this.cpuPercent,
    required this.memoryBytes,
    required this.memoryLimit,
  });

  final String instanceId;
  final double cpuPercent;
  final int memoryBytes;
  final int memoryLimit;

  factory InstanceStats.fromJson(Map<String, dynamic> j) => InstanceStats(
        instanceId: j['instance_id'] as String? ?? '',
        cpuPercent: (j['cpu_percent'] as num?)?.toDouble() ?? 0,
        memoryBytes: (j['memory_bytes'] as num?)?.toInt() ?? 0,
        memoryLimit: (j['memory_limit'] as num?)?.toInt() ?? 0,
      );
}

class Task {
  Task({
    required this.id,
    required this.instanceId,
    required this.goal,
    required this.state,
    required this.step,
    required this.maxSteps,
    required this.createdAt,
    this.error = '',
    this.result = '',
    this.parentTaskId = '',
  });

  final String id;
  final String instanceId;
  final String goal;
  final String state;
  final int step;
  final int maxSteps;
  final DateTime createdAt;
  final String error;
  final String result;

  /// Set when this run was started by another run — a sub-agent. Empty for a
  /// task the operator (or a trigger) started directly.
  final String parentTaskId;

  bool get isLive =>
      state == 'running' || state == 'queued' || state == 'awaiting_human';

  factory Task.fromJson(Map<String, dynamic> j) => Task(
        id: j['id'] as String,
        instanceId: j['instance_id'] as String? ?? '',
        goal: j['goal'] as String? ?? '',
        state: j['state'] as String? ?? 'queued',
        step: (j['step'] as num?)?.toInt() ?? 0,
        maxSteps: (j['max_steps'] as num?)?.toInt() ?? 0,
        createdAt: DateTime.tryParse(j['created_at'] as String? ?? '') ??
            DateTime.now(),
        error: j['error'] as String? ?? '',
        result: j['result'] as String? ?? '',
        parentTaskId: j['parent_task_id'] as String? ?? '',
      );
}

/// What the agent decided to do on one step, as recorded by the loop.
///
/// Only the fields the timeline renders; the wire payload carries more
/// (tool definitions, sub-goal plumbing) that a phone has no use for.
class AgentAction {
  const AgentAction({
    this.action = '',
    this.thought = '',
    this.target = '',
    this.mark = 0,
    this.coordinates = const [],
    this.text = '',
    this.key = '',
    this.query = '',
  });

  final String action;
  final String thought;
  final String target;

  /// Set-of-Marks index the model clicked, when it picked an element by its
  /// numbered overlay rather than by coordinates. 0 means none.
  final int mark;
  final List<num> coordinates;
  final String text;
  final String key;
  final String query;

  /// What the action was aimed at, in the console's precedence order:
  /// an element, typed text, a key chord, or raw coordinates.
  String get detail {
    if (target.isNotEmpty) return target;
    if (text.isNotEmpty) return text;
    if (key.isNotEmpty) return key;
    if (coordinates.isNotEmpty) return coordinates.join(',');
    return '';
  }

  factory AgentAction.fromJson(Map<String, dynamic> j) => AgentAction(
        action: j['action'] as String? ?? '',
        thought: j['thought'] as String? ?? '',
        target: j['target'] as String? ?? '',
        mark: (j['mark'] as num?)?.toInt() ?? 0,
        coordinates: ((j['coordinates'] as List?) ?? const [])
            .whereType<num>()
            .toList(growable: false),
        text: j['text'] as String? ?? '',
        key: j['key'] as String? ?? '',
        query: j['query'] as String? ?? '',
      );
}

/// One step of a run: what the agent saw, thought, did, and what came of it.
class StepRecord {
  StepRecord({
    required this.id,
    required this.taskId,
    required this.step,
    required this.action,
    required this.outcome,
    required this.durationMs,
    required this.promptTokens,
    required this.outputTokens,
    this.observationKey = '',
  });

  final String id;
  final String taskId;
  final int step;
  final AgentAction action;

  /// Artifact key of the screenshot the agent acted on. Empty when the step
  /// had no frame (a shell step, say).
  final String observationKey;
  final String outcome;
  final int durationMs;
  final int promptTokens;
  final int outputTokens;

  static final _failure =
      RegExp(r'fail|error|refused|timed out', caseSensitive: false);

  /// Whether the outcome reads as a failure — the same heuristic the console
  /// uses to tint a step red, kept identical so the two clients agree on
  /// which steps look alarming.
  bool get failed => _failure.hasMatch(outcome);

  factory StepRecord.fromJson(Map<String, dynamic> j) => StepRecord(
        id: j['id'] as String? ?? '',
        taskId: j['task_id'] as String? ?? '',
        step: (j['step'] as num?)?.toInt() ?? 0,
        action: AgentAction.fromJson(
            ((j['action'] as Map?) ?? const {}).cast<String, dynamic>()),
        observationKey: j['observation_key'] as String? ?? '',
        outcome: j['outcome'] as String? ?? '',
        durationMs: (j['duration_ms'] as num?)?.toInt() ?? 0,
        promptTokens: (j['prompt_tokens'] as num?)?.toInt() ?? 0,
        outputTokens: (j['output_tokens'] as num?)?.toInt() ?? 0,
      );
}

class Alert {
  Alert({
    required this.id,
    required this.kind,
    required this.severity,
    required this.title,
    required this.body,
    required this.needsReply,
    required this.createdAt,
    this.instanceId = '',
    this.taskId = '',
    this.screenshotId = '',
    this.reply = '',
    this.resolvedAt,
  });

  final String id;
  final String kind;
  final String severity;
  final String title;
  final String body;
  final bool needsReply;
  final DateTime createdAt;
  final String instanceId;
  final String taskId;
  final String screenshotId;
  final String reply;
  final DateTime? resolvedAt;

  bool get isOpen => needsReply && resolvedAt == null;

  factory Alert.fromJson(Map<String, dynamic> j) => Alert(
        id: j['id'] as String,
        kind: j['kind'] as String? ?? '',
        severity: j['severity'] as String? ?? 'info',
        title: j['title'] as String? ?? '',
        body: j['body'] as String? ?? '',
        needsReply: j['needs_reply'] as bool? ?? false,
        createdAt: DateTime.tryParse(j['created_at'] as String? ?? '') ??
            DateTime.now(),
        instanceId: j['instance_id'] as String? ?? '',
        taskId: j['task_id'] as String? ?? '',
        screenshotId: j['screenshot_id'] as String? ?? '',
        reply: j['reply'] as String? ?? '',
        resolvedAt: DateTime.tryParse(j['resolved_at'] as String? ?? ''),
      );
}

class ChatMessage {
  ChatMessage({
    required this.id,
    required this.role,
    required this.body,
    required this.createdAt,
    this.kind = 'message',
    this.planState = '',
  });

  final String id;
  final String role;
  final String body;
  final DateTime createdAt;

  /// "message" for ordinary talk, "plan" for a proposal awaiting approval.
  final String kind;

  /// "" while a plan is still open, then "approved" or "discarded".
  final String planState;

  bool get isUser => role == 'user';

  /// A plan the operator has not answered yet — the only case that should
  /// render Approve / Discard controls.
  bool get isOpenPlan => kind == 'plan' && planState.isEmpty;

  factory ChatMessage.fromJson(Map<String, dynamic> j) => ChatMessage(
        id: j['id'] as String? ?? '',
        role: j['role'] as String? ?? 'agent',
        body: j['body'] as String? ?? '',
        // Older rows predate these columns and come back absent, which must
        // read as an ordinary message rather than an unanswered plan.
        kind: j['kind'] as String? ?? 'message',
        planState: j['plan_state'] as String? ?? '',
        createdAt: DateTime.tryParse(j['created_at'] as String? ?? '') ??
            DateTime.now(),
      );
}

/// One recorded action inside a skill.
///
/// [label] is nullable rather than defaulting to '': absent means the recorder
/// captured no accessibility information at all for this step, which is what
/// the coordinate-only warning keys off. Fields the app does not render
/// ([assertText], [meta]) are still carried, so saving an edited skill does not
/// silently strip what the recorder captured.
class SkillStep {
  SkillStep({
    this.index = 0,
    this.kind = '',
    this.window = '',
    this.role = '',
    this.label,
    this.coordinates = const [],
    this.text = '',
    this.key = '',
    this.param = '',
    this.assertText = '',
    this.meta = const {},
  });

  final int index;
  final String kind;
  final String window;
  final String role;
  String? label;
  final List<num> coordinates;
  String text;
  String param;
  final String key;
  final String assertText;
  final Map<String, dynamic> meta;

  /// Replay will fall back to raw coordinates — fragile if the layout moves.
  bool get coordinateOnly =>
      (label == null || label!.isEmpty) && coordinates.isNotEmpty;

  factory SkillStep.fromJson(Map<String, dynamic> j) => SkillStep(
        index: (j['index'] as num?)?.toInt() ?? 0,
        kind: j['kind'] as String? ?? '',
        window: j['window'] as String? ?? '',
        role: j['role'] as String? ?? '',
        label: j['label'] as String?,
        coordinates: ((j['coordinates'] as List?) ?? const [])
            .whereType<num>()
            .toList(),
        text: j['text'] as String? ?? '',
        key: j['key'] as String? ?? '',
        param: j['param'] as String? ?? '',
        assertText: j['assert'] as String? ?? '',
        meta: ((j['meta'] as Map?) ?? const {}).cast<String, dynamic>(),
      );

  Map<String, dynamic> toJson() => {
        'index': index,
        'kind': kind,
        if (window.isNotEmpty) 'window': window,
        if (role.isNotEmpty) 'role': role,
        if (label != null) 'label': label,
        if (coordinates.isNotEmpty) 'coordinates': coordinates,
        if (text.isNotEmpty) 'text': text,
        if (key.isNotEmpty) 'key': key,
        if (param.isNotEmpty) 'param': param,
        if (assertText.isNotEmpty) 'assert': assertText,
        if (meta.isNotEmpty) 'meta': meta,
      };
}

class Skill {
  Skill({
    required this.id,
    required this.name,
    this.description = '',
    this.params = const [],
    this.steps = const [],
    this.markdown = '',
    this.version = 1,
    this.refinementNotes = '',
  });

  final String id;
  String name;
  String description;
  List<String> params;
  List<SkillStep> steps;

  /// The compiled instructions the model actually sees. Regenerated by the
  /// server on save, so it is read-only here.
  final String markdown;
  final int version;

  /// What the last AI refinement pass changed, in its own words.
  final String refinementNotes;

  int get stepCount => steps.length;

  factory Skill.fromJson(Map<String, dynamic> j) => Skill(
        id: j['id'] as String,
        name: j['name'] as String? ?? '',
        description: j['description'] as String? ?? '',
        params:
            ((j['params'] as List?) ?? const []).map((e) => '$e').toList(),
        steps: ((j['steps'] as List?) ?? const [])
            .map((e) => SkillStep.fromJson((e as Map).cast<String, dynamic>()))
            .toList(),
        markdown: j['markdown'] as String? ?? '',
        version: (j['version'] as num?)?.toInt() ?? 1,
        refinementNotes: j['refinement_notes'] as String? ?? '',
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'description': description,
        'params': params,
        'steps': [for (final s in steps) s.toJson()],
      };
}

class FleetEvent {
  FleetEvent({required this.type, this.instanceId, this.taskId, this.payload});

  final String type;
  final String? instanceId;
  final String? taskId;
  final dynamic payload;

  factory FleetEvent.fromJson(Map<String, dynamic> j) => FleetEvent(
        type: j['type'] as String? ?? '',
        instanceId: j['instance_id'] as String?,
        taskId: j['task_id'] as String?,
        payload: j['payload'],
      );
}

class SwarmMember {
  SwarmMember({
    required this.instanceId,
    required this.instanceName,
    required this.role,
    required this.archetypeId,
    required this.status,
  });

  final String instanceId;
  final String instanceName;
  final String role;
  final String archetypeId;
  final String status;

  factory SwarmMember.fromJson(Map<String, dynamic> j) => SwarmMember(
        instanceId: j['instance_id'] as String? ?? '',
        instanceName: j['instance_name'] as String? ?? '',
        role: j['role'] as String? ?? '',
        archetypeId: j['archetype_id'] as String? ?? '',
        status: j['status'] as String? ?? 'working',
      );
}

class SwarmMessage {
  SwarmMessage({
    required this.id,
    required this.swarmId,
    required this.fromBot,
    required this.toBot,
    required this.phase,
    required this.content,
    required this.createdAt,
  });

  final String id;
  final String swarmId;
  final String fromBot;
  final String toBot;
  final String phase;
  final String content;
  final DateTime createdAt;

  factory SwarmMessage.fromJson(Map<String, dynamic> j) => SwarmMessage(
        id: j['id'] as String? ?? '',
        swarmId: j['swarm_id'] as String? ?? '',
        fromBot: j['from_bot'] as String? ?? '',
        toBot: j['to_bot'] as String? ?? '',
        phase: j['phase'] as String? ?? '',
        content: j['content'] as String? ?? '',
        createdAt: DateTime.tryParse(j['created_at'] as String? ?? '') ??
            DateTime.now(),
      );
}

class SwarmArtifact {
  SwarmArtifact({
    required this.id,
    required this.title,
    required this.author,
    required this.category,
    required this.content,
    required this.createdAt,
    this.approvedBy = const [],
  });

  final String id;
  final String title;
  final String author;
  final String category;
  final String content;
  final DateTime createdAt;

  /// Who has signed off — peer bots and, when the operator weighs in, the
  /// literal reviewer name "operator". Empty means still awaiting review.
  final List<String> approvedBy;

  factory SwarmArtifact.fromJson(Map<String, dynamic> j) => SwarmArtifact(
        id: j['id'] as String? ?? '',
        title: j['title'] as String? ?? '',
        author: j['author'] as String? ?? '',
        category: j['category'] as String? ?? '',
        content: j['content'] as String? ?? '',
        createdAt: DateTime.tryParse(j['created_at'] as String? ?? '') ??
            DateTime.now(),
        approvedBy: ((j['approved_by'] as List?) ?? const [])
            .map((e) => '$e')
            .toList(growable: false),
      );
}

class SwarmTeam {
  SwarmTeam({
    required this.id,
    required this.name,
    required this.mission,
    required this.status,
    required this.members,
    required this.messages,
    required this.artifacts,
    required this.createdAt,
  });

  final String id;
  final String name;
  final String mission;
  final String status;
  final List<SwarmMember> members;
  final List<SwarmMessage> messages;
  final List<SwarmArtifact> artifacts;
  final DateTime createdAt;

  factory SwarmTeam.fromJson(Map<String, dynamic> j) => SwarmTeam(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        mission: j['mission'] as String? ?? '',
        status: j['status'] as String? ?? 'running',
        members: (j['members'] as List?)
                ?.map((m) => SwarmMember.fromJson(m as Map<String, dynamic>))
                .toList() ??
            [],
        messages: (j['messages'] as List?)
                ?.map((m) => SwarmMessage.fromJson(m as Map<String, dynamic>))
                .toList() ??
            [],
        artifacts: (j['artifacts'] as List?)
                ?.map((a) => SwarmArtifact.fromJson(a as Map<String, dynamic>))
                .toList() ??
            [],
        createdAt: DateTime.tryParse(j['created_at'] as String? ?? '') ?? DateTime.now(),
      );
}

/// A shared fleet secret as the API returns it — metadata only.
///
/// There is no value field, deliberately: the list endpoint is readable by the
/// auditor role, so returning values handed every fleet credential to anyone
/// who could sign in. [hasValue] says whether one is set; the value itself
/// never crosses the wire.
class SharedSecret {
  SharedSecret({
    required this.key,
    required this.scope,
    this.note = '',
    this.createdBy = '',
    this.hasValue = false,
  });

  final String key;
  final String scope;
  final String note;
  final String createdBy;
  final bool hasValue;

  factory SharedSecret.fromJson(Map<String, dynamic> j) => SharedSecret(
        key: j['key'] as String? ?? '',
        scope: j['scope'] as String? ?? 'fleet',
        note: j['note'] as String? ?? '',
        createdBy: j['created_by'] as String? ?? '',
        hasValue: j['has_value'] as bool? ?? false,
      );
}

class SharedSession {
  SharedSession({
    required this.id,
    required this.domain,
    required this.title,
    required this.cookiesJson,
    this.createdByInstance = '',
  });

  final String id;
  final String domain;
  final String title;
  final String cookiesJson;
  final String createdByInstance;

  factory SharedSession.fromJson(Map<String, dynamic> j) => SharedSession(
        id: j['id'] as String? ?? '',
        domain: j['domain'] as String? ?? '',
        title: j['title'] as String? ?? '',
        cookiesJson: j['cookies_json'] as String? ?? '',
        createdByInstance: j['created_by_instance'] as String? ?? '',
      );
}

class PeerMessage {
  PeerMessage({
    required this.id,
    required this.fromInstanceId,
    required this.fromInstanceName,
    required this.toInstanceId,
    required this.kind,
    required this.content,
    required this.createdAt,
    this.conversationId = '',
    this.compactedCount = 0,
  });

  final String id;

  /// The thread this message belongs to.
  final String conversationId;

  /// For a summary message, how many messages it stands in for.
  final int compactedCount;
  final String fromInstanceId;
  final String fromInstanceName;
  final String toInstanceId;
  final String kind;
  final String content;
  final DateTime createdAt;

  factory PeerMessage.fromJson(Map<String, dynamic> j) => PeerMessage(
        id: j['id'] as String? ?? '',
        fromInstanceId: j['from_instance_id'] as String? ?? '',
        fromInstanceName: j['from_instance_name'] as String? ?? '',
        toInstanceId: j['to_instance_id'] as String? ?? 'broadcast',
        kind: j['kind'] as String? ?? 'message',
        content: j['content'] as String? ?? '',
        createdAt: DateTime.tryParse(j['created_at'] as String? ?? '') ?? DateTime.now(),
        conversationId: j['conversation_id'] as String? ?? '',
        compactedCount:
            ((j['data'] as Map?)?['compacted_messages'] as num?)?.toInt() ?? 0,
      );
}

/// One chat with a single bot.
///
/// A bot used to have one unbounded history, so there was no way to start
/// fresh or clear a chat that had gone somewhere unhelpful without losing
/// every conversation you had ever had with it.
class ChatSession {
  const ChatSession({
    required this.id,
    required this.title,
    required this.pinned,
    required this.messageCount,
    this.lastMessageAt,
    this.createdAt,
  });

  /// The chat holding messages from before chats could be separated. It is not
  /// a real row, so it cannot be renamed or pinned.
  static const defaultId = 'default';

  final String id;
  final String title;
  final bool pinned;
  final int messageCount;
  final DateTime? lastMessageAt;

  /// When the chat was started. Null for the implicit default chat.
  final DateTime? createdAt;

  /// How recently the chat was used, for picking which one to reopen.
  ///
  /// A chat you have just started has no last message. Skipping those meant
  /// a new chat could never be the most recent, so opening the bot went back
  /// to an old chat instead of the one you had just made.
  DateTime get lastUsedAt =>
      lastMessageAt ?? createdAt ?? DateTime.fromMillisecondsSinceEpoch(0);

  bool get isDefault => id == defaultId;

  /// What to show when the chat has no name of its own.
  String get displayTitle {
    if (title.isNotEmpty) return title;
    return isDefault ? 'Earlier chat' : 'Untitled chat';
  }

  factory ChatSession.fromJson(Map<String, dynamic> j) => ChatSession(
        id: j['id'] as String? ?? '',
        title: j['title'] as String? ?? '',
        pinned: j['pinned'] as bool? ?? false,
        messageCount: (j['message_count'] as num?)?.toInt() ?? 0,
        lastMessageAt: DateTime.tryParse(j['last_message_at'] as String? ?? ''),
        createdAt: DateTime.tryParse(j['created_at'] as String? ?? ''),
      );
}

/// A tool the operator added by hand, with how to fetch it.
///
/// The archetype catalogue cannot know about a company's internal CLI.
class CustomTool {
  const CustomTool({
    required this.name,
    required this.method,
    required this.spec,
  });

  /// How to fetch it. `github` clones into the workspace rather than putting
  /// anything on PATH, which is right for wordlists and template collections.
  static const methods = ['apt', 'pip', 'npm', 'go', 'url', 'github'];

  static const methodHints = {
    'apt': 'Debian package name',
    'pip': 'Python package name',
    'npm': 'npm package name',
    'go': 'module path, e.g. github.com/x/y/cmd/z@latest',
    'url': 'direct download URL of a single binary',
    'github': 'owner/repo — cloned into the workspace',
  };

  final String name;
  final String method;
  final String spec;

  factory CustomTool.fromJson(Map<String, dynamic> j) => CustomTool(
        name: j['name'] as String? ?? '',
        method: j['method'] as String? ?? 'apt',
        spec: j['spec'] as String? ?? '',
      );
}

/// An organisation or department.
class Org {
  const Org({
    required this.id,
    required this.name,
    this.description = '',
    this.memberCount = 0,
    this.botCount = 0,
  });

  final String id;
  final String name;
  final String description;
  final int memberCount;
  final int botCount;

  factory Org.fromJson(Map<String, dynamic> j) => Org(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        description: j['description'] as String? ?? '',
        memberCount: (j['member_count'] as num?)?.toInt() ?? 0,
        botCount: (j['bot_count'] as num?)?.toInt() ?? 0,
      );
}

/// One person's standing in one department.
class OrgMember {
  const OrgMember({
    required this.userId,
    required this.email,
    required this.orgRole,
  });

  /// Roles, most to least capable.
  static const roles = ['owner', 'admin', 'member', 'viewer'];

  /// What each role can do, in the terms an administrator thinks in.
  static const roleSummary = {
    'owner': 'Everything, including who else has access',
    'admin': 'Create, edit and delete bots; use shared secrets',
    'member': 'Talk to bots and use their desktops',
    'viewer': 'Read only — cannot make anything happen',
  };

  final String userId;
  final String email;
  final String orgRole;

  factory OrgMember.fromJson(Map<String, dynamic> j) => OrgMember(
        userId: j['user_id'] as String? ?? '',
        email: j['email'] as String? ?? '',
        orgRole: j['org_role'] as String? ?? 'member',
      );
}

/// A per-bot exception to what someone may do.
class BotGrant {
  const BotGrant({required this.userId, required this.permissions});

  /// Permissions, least to most dangerous.
  static const all = [
    'view', 'read', 'chat', 'desktop',
    'edit', 'create', 'delete', 'secrets', 'manage_members',
  ];

  static const labels = {
    'view': 'See the bot exists',
    'read': 'Read its replies and history',
    'chat': 'Talk to it',
    'desktop': 'Use its desktop',
    'edit': 'Change its settings',
    'create': 'Create new bots',
    'delete': 'Delete bots',
    'secrets': 'Use shared secrets and sessions',
    'manage_members': 'Manage who has access',
  };

  /// The ones that mean anything for a single bot.
  static const perBot = ['view', 'read', 'chat', 'desktop', 'edit', 'delete'];

  final String userId;
  final List<String> permissions;

  factory BotGrant.fromJson(Map<String, dynamic> j) => BotGrant(
        userId: j['user_id'] as String? ?? '',
        permissions: ((j['permissions'] as List?) ?? const [])
            .map((e) => '$e')
            .toList(),
      );
}

/// A user account.
class FleetUser {
  const FleetUser({required this.id, required this.email, required this.role});

  final String id;
  final String email;

  /// Deployment-wide role: admin, operator or auditor.
  final String role;

  factory FleetUser.fromJson(Map<String, dynamic> j) => FleetUser(
        id: j['id'] as String? ?? '',
        email: j['email'] as String? ?? '',
        role: j['role'] as String? ?? 'operator',
      );
}

/// Which model does what.
///
/// A combination assigns models to roles, so the model that reads the screen
/// need not be the one that reasons about it. It is selectable anywhere a
/// single provider is, including inside a bot's fallback chain.
class ModelCombo {
  const ModelCombo({
    required this.id,
    required this.name,
    required this.roles,
    this.description = '',
  });

  /// Roles, in the order a picker should offer them.
  static const roleVision = 'vision';
  static const roleReasoning = 'reasoning';
  static const roleChat = 'chat';
  static const roleSummarize = 'summarize';
  static const roleRefine = 'refine';
  static const allRoles = [
    roleVision,
    roleReasoning,
    roleChat,
    roleSummarize,
    roleRefine,
  ];

  /// What each role is for, in the operator's terms rather than the code's.
  static const roleLabels = {
    roleVision: 'Hands — sees the screen and clicks',
    roleReasoning: 'Brain — plans, deduces, writes code',
    roleChat: 'Chat — talks to you',
    roleSummarize: 'Summarise — compacts long threads',
    roleRefine: 'Refine — hardens recorded skills',
  };

  static const roleShort = {
    roleVision: 'Hands',
    roleReasoning: 'Brain',
    roleChat: 'Chat',
    roleSummarize: 'Summarise',
    roleRefine: 'Refine',
  };

  final String id;
  final String name;
  final String description;

  /// role -> provider id.
  final Map<String, String> roles;

  /// A brain-and-hands pair rather than a full assignment.
  bool get isSimple =>
      roles.length <= 2 &&
      roles.keys.every((r) => r == roleVision || r == roleReasoning);

  factory ModelCombo.fromJson(Map<String, dynamic> j) => ModelCombo(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        description: j['description'] as String? ?? '',
        roles: ((j['roles'] as Map?) ?? const {})
            .map((k, v) => MapEntry('$k', '$v')),
      );
}

/// Something a bot decided was worth keeping.
///
/// Agents choose what to remember; this is what that turned out to be. Worth
/// looking at: a wrong conclusion recorded once is recalled indefinitely.
class BotMemory {
  const BotMemory({
    required this.id,
    required this.title,
    required this.content,
    required this.tags,
    required this.createdAt,
    this.sourceTaskId = '',
  });

  final String id;
  final String title;
  final String content;
  final List<String> tags;
  final DateTime createdAt;

  /// The task the agent was running when it recorded this, if any.
  final String sourceTaskId;

  factory BotMemory.fromJson(Map<String, dynamic> j) => BotMemory(
        id: j['id'] as String? ?? '',
        title: j['title'] as String? ?? '',
        content: j['content'] as String? ?? '',
        tags: ((j['tags'] as List?) ?? const [])
            .map((e) => '$e')
            .toList(growable: false),
        createdAt:
            DateTime.tryParse(j['created_at'] as String? ?? '') ?? DateTime.now(),
        sourceTaskId: j['source_task_id'] as String? ?? '',
      );
}

/// A thread in fleet comms: you and a bot, two bots, or a group.
///
/// Threads are created and deleted deliberately rather than inferred from who
/// happened to message whom, so you can put two agents in a room before they
/// have anything to say to each other.
class Conversation {
  const Conversation({
    required this.id,
    required this.kind,
    required this.title,
    required this.members,
    required this.messageCount,
    this.lastMessageAt,
    this.createdAt,
    this.pinned = false,
  });

  /// The always-present channel every agent hears.
  static const broadcastId = 'broadcast';

  /// How you are identified in a member list.
  static const operatorId = 'operator';

  final String id;

  /// 'direct', 'pair', 'group' or 'broadcast'.
  final String kind;
  final String title;
  final List<String> members;
  final int messageCount;
  final DateTime? lastMessageAt;

  /// When the thread was opened. Null only for the built-in channel, which is
  /// implicit and has no row until it is renamed or pinned.
  final DateTime? createdAt;

  /// How recently a thread was used, for ordering.
  ///
  /// A thread that has just been made has no last message, and ordering on
  /// that alone sent every new chat to the BOTTOM of its group -- so opening
  /// the group went to the oldest thread and the chat you had just made was
  /// the hardest one to reach. Falling back to when it was opened puts it
  /// where you expect: at the top, until something newer happens elsewhere.
  DateTime get lastUsedAt =>
      lastMessageAt ?? createdAt ?? DateTime.fromMillisecondsSinceEpoch(0);

  /// Keeps a thread at the top of the list however long it has been quiet.
  final bool pinned;

  bool get isBroadcast => id == broadcastId;

  /// An everyone-channel: the whole fleet hears it. True for the built-in
  /// channel and for any other opened since.
  bool get isEveryone => isBroadcast || kind == 'broadcast';

  /// Identifies the set of people a thread is between, so several threads with
  /// the same participants collapse to one row in the comms list.
  ///
  /// The built-in channel and any everyone-channel opened since group
  /// together: they are between the same people -- everyone -- so a new chat
  /// made from the broadcast belongs alongside it rather than in a row of its
  /// own. The built-in one cannot be deleted, but it is still reachable from
  /// the thread bar like any other sibling.
  String get participantKey {
    if (isBroadcast || isEveryone) return 'everyone';
    if (members.isEmpty) return 'everyone';
    final sorted = [...members]..sort();
    return sorted.join('|');
  }

  /// A pair thread is two agents talking with you watching, which is worth
  /// showing differently from a thread you are in.
  bool get isPair => kind == 'pair';

  factory Conversation.fromJson(Map<String, dynamic> j) => Conversation(
        id: j['id'] as String? ?? '',
        kind: j['kind'] as String? ?? 'group',
        title: j['title'] as String? ?? '',
        members: ((j['members'] as List?) ?? const [])
            .map((e) => '$e')
            .toList(growable: false),
        messageCount: (j['message_count'] as num?)?.toInt() ?? 0,
        lastMessageAt: DateTime.tryParse(j['last_message_at'] as String? ?? ''),
          createdAt: DateTime.tryParse(j['created_at'] as String? ?? ''),
        pinned: j['pinned'] as bool? ?? false,
      );
}

class PipelineNode {
  PipelineNode({
    required this.id,
    required this.name,
    required this.archetypeId,
    required this.goalTemplate,
    this.instanceId = '',
  });

  final String id;
  final String name;
  final String archetypeId;
  final String goalTemplate;

  /// Pins the stage to one bot. Empty falls back to [archetypeId] — a node
  /// names one or the other, never both.
  final String instanceId;

  factory PipelineNode.fromJson(Map<String, dynamic> j) => PipelineNode(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        // No archetype fallback: a node pinned to an instance has none, and
        // inventing one here corrupted the node on the next save.
        archetypeId: j['archetype_id'] as String? ?? '',
        goalTemplate: j['goal_template'] as String? ?? '',
        instanceId: j['instance_id'] as String? ?? '',
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name,
        'goal_template': goalTemplate,
        'archetype_id': archetypeId,
        'instance_id': instanceId,
      };
}

class WorkflowPipeline {
  WorkflowPipeline({
    required this.id,
    required this.name,
    this.description = '',
    required this.nodes,
    this.edges = const [],
    this.maxParallel = 0,
  });

  final String id;
  final String name;
  final String description;
  final List<PipelineNode> nodes;

  /// Bounds concurrent stages. 0 means the engine default (4).
  final int maxParallel;

  /// Which stage feeds which. The API has always returned these; the app
  /// simply never read them, so a pipeline rendered as a flat list and the
  /// actual shape of the graph was invisible.
  final List<PipelineEdge> edges;

  factory WorkflowPipeline.fromJson(Map<String, dynamic> j) => WorkflowPipeline(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        description: j['description'] as String? ?? '',
        nodes: (j['nodes'] as List?)
                ?.map((n) => PipelineNode.fromJson(n as Map<String, dynamic>))
                .toList() ??
            [],
        edges: (j['edges'] as List?)
                ?.map((e) => PipelineEdge.fromJson(
                    (e as Map).cast<String, dynamic>()))
                .toList() ??
            const [],
        maxParallel: (j['max_parallel'] as num?)?.toInt() ?? 0,
      );

  /// Stages grouped into dependency layers: nothing in layer 0 depends on
  /// anything else, layer 1 waits on layer 0, and so on. A layer is also how
  /// the engine schedules now — stages whose dependencies have settled run
  /// concurrently, bounded by the pipeline's max_parallel — so the picture and
  /// the execution finally agree. With no edges the pipeline is a straight
  /// line, which is what the flat list used to imply for every pipeline
  /// whether it was true or not.
  List<List<PipelineNode>> get layers {
    if (edges.isEmpty) return nodes.map((n) => [n]).toList();

    final incoming = <String, Set<String>>{for (final n in nodes) n.id: {}};
    for (final e in edges) {
      if (incoming.containsKey(e.toNodeId) &&
          incoming.containsKey(e.fromNodeId)) {
        incoming[e.toNodeId]!.add(e.fromNodeId);
      }
    }

    final out = <List<PipelineNode>>[];
    final placed = <String>{};
    var guard = 0;
    while (placed.length < nodes.length && guard++ < nodes.length + 1) {
      final layer = nodes
          .where((n) =>
              !placed.contains(n.id) && incoming[n.id]!.every(placed.contains))
          .toList();
      // A cycle leaves nothing schedulable; show the rest rather than looping.
      if (layer.isEmpty) break;
      out.add(layer);
      placed.addAll(layer.map((n) => n.id));
    }
    final leftover = nodes.where((n) => !placed.contains(n.id)).toList();
    if (leftover.isNotEmpty) out.add(leftover);
    return out;
  }
}

class PipelineRun {
  PipelineRun({
    required this.id,
    required this.pipelineId,
    required this.status,
    required this.startedAt,
    this.nodeStates = const {},
    this.nodeResults = const {},
    this.finishedAt,
  });

  final String id;
  final String pipelineId;
  final String status;
  final DateTime startedAt;

  /// Per-node state: waiting | running | done | failed | skipped. Several
  /// stages genuinely run at once, so a single "current node" cannot describe
  /// a run; this can.
  final Map<String, String> nodeStates;
  final Map<String, String> nodeResults;
  final DateTime? finishedAt;

  /// How many nodes sit in each state, for the one-line run summary.
  Map<String, int> get stateTally {
    final out = <String, int>{};
    for (final s in nodeStates.values) {
      out[s] = (out[s] ?? 0) + 1;
    }
    return out;
  }

  factory PipelineRun.fromJson(Map<String, dynamic> j) => PipelineRun(
        id: j['id'] as String? ?? '',
        pipelineId: j['pipeline_id'] as String? ?? '',
        status: j['status'] as String? ?? 'running',
        startedAt: DateTime.tryParse(j['started_at'] as String? ?? '') ?? DateTime.now(),
        nodeStates: ((j['node_states'] as Map?) ?? const {})
            .map((k, v) => MapEntry('$k', '$v')),
        nodeResults: ((j['node_results'] as Map?) ?? const {})
            .map((k, v) => MapEntry('$k', '$v')),
        finishedAt: DateTime.tryParse(j['finished_at'] as String? ?? ''),
      );
}

class AIProvider {
  AIProvider({
    required this.id,
    required this.name,
    required this.kind,
    required this.model,
    this.baseUrl = '',
    this.vision = true,
    this.priority = 100,
    this.enabled = true,
    this.apiKeyRef = '',
    this.authMode = 'api_key',
    this.oauthClientId = '',
    this.signedIn = false,
  });

  final String id;
  final String name;
  final String kind;
  final String model;
  final String baseUrl;
  final bool vision;
  final int priority;
  final bool enabled;

  /// Names the vault entry holding this connection's key. The key itself never
  /// leaves the server, so this is only ever "is one set", never the secret.
  final String apiKeyRef;

  bool get hasKey => apiKeyRef.isNotEmpty;

  /// 'api_key' or 'oauth'.
  final String authMode;
  final String oauthClientId;

  /// Whether an account sign-in is stored. The token itself never leaves the
  /// server, so this is all the app is told.
  final bool signedIn;

  bool get usesOAuth => authMode == 'oauth';

  factory AIProvider.fromJson(Map<String, dynamic> j) => AIProvider(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        kind: j['kind'] as String? ?? 'ollama',
        model: j['model'] as String? ?? '',
        baseUrl: j['base_url'] as String? ?? '',
        vision: j['vision'] as bool? ?? true,
        priority: (j['priority'] as num?)?.toInt() ?? 100,
        enabled: j['enabled'] as bool? ?? true,
        apiKeyRef: j['api_key_ref'] as String? ?? '',
        authMode: j['auth_mode'] as String? ?? 'api_key',
        oauthClientId: j['oauth_client_id'] as String? ?? '',
        signedIn: j['signed_in'] as bool? ?? false,
      );
}

String humanAgo(DateTime at) {
  final seconds = DateTime.now().difference(at).inSeconds;
  if (seconds < 45) return 'just now';
  if (seconds < 3600) return '${(seconds / 60).round()}m ago';
  if (seconds < 86400) return '${(seconds / 3600).round()}h ago';
  return '${(seconds / 86400).round()}d ago';
}

String humanBytes(int n) {
  if (n <= 0) return '0 B';
  const units = ['B', 'KB', 'MB', 'GB', 'TB'];
  var value = n.toDouble();
  var i = 0;
  while (value >= 1024 && i < units.length - 1) {
    value /= 1024;
    i++;
  }
  return '${value.toStringAsFixed(i == 0 ? 0 : 1)} ${units[i]}';
}

/// Live resource usage of the machine running the orchestrator, from
/// GET /api/telemetry/host. Distinct from InstanceStats, which is per sandbox.
class HostStats {
  HostStats({
    required this.cpuPercent,
    required this.cpuCores,
    required this.load1,
    required this.memoryUsed,
    required this.memoryTotal,
    required this.swapUsed,
    required this.swapTotal,
    required this.diskUsed,
    required this.diskTotal,
    required this.uptimeSec,
    this.gpu,
    this.gpuMessage = '',
  });

  final double cpuPercent;
  final int cpuCores;
  final double load1;
  final int memoryUsed;
  final int memoryTotal;
  final int swapUsed;
  final int swapTotal;
  final int diskUsed;
  final int diskTotal;
  final double uptimeSec;

  /// Null on a host with no GPU, or where the orchestrator cannot read one.
  /// [gpuMessage] then says which, so the UI never has to guess.
  final GpuStats? gpu;
  final String gpuMessage;

  factory HostStats.fromJson(Map<String, dynamic> j) => HostStats(
        cpuPercent: (j['cpu_percent'] as num?)?.toDouble() ?? 0,
        cpuCores: (j['cpu_cores'] as num?)?.toInt() ?? 0,
        load1: (j['load1'] as num?)?.toDouble() ?? 0,
        memoryUsed: (j['memory_used_bytes'] as num?)?.toInt() ?? 0,
        memoryTotal: (j['memory_total_bytes'] as num?)?.toInt() ?? 0,
        swapUsed: (j['swap_used_bytes'] as num?)?.toInt() ?? 0,
        swapTotal: (j['swap_total_bytes'] as num?)?.toInt() ?? 0,
        diskUsed: (j['disk_used_bytes'] as num?)?.toInt() ?? 0,
        diskTotal: (j['disk_total_bytes'] as num?)?.toInt() ?? 0,
        uptimeSec: (j['uptime_sec'] as num?)?.toDouble() ?? 0,
        gpu: j['gpu'] == null
            ? null
            : GpuStats.fromJson((j['gpu'] as Map).cast<String, dynamic>()),
        gpuMessage: j['gpu_message'] as String? ?? '',
      );

  double get memoryFraction =>
      memoryTotal == 0 ? 0 : memoryUsed / memoryTotal;
  double get diskFraction => diskTotal == 0 ? 0 : diskUsed / diskTotal;
}

class GpuStats {
  GpuStats({
    required this.name,
    required this.memoryUsed,
    required this.memoryTotal,
    required this.utilisation,
    this.temperatureC = 0,
  });

  final String name;
  final int memoryUsed;
  final int memoryTotal;
  final double utilisation;
  final double temperatureC;

  factory GpuStats.fromJson(Map<String, dynamic> j) => GpuStats(
        name: j['name'] as String? ?? 'GPU',
        memoryUsed: (j['memory_used_bytes'] as num?)?.toInt() ?? 0,
        memoryTotal: (j['memory_total_bytes'] as num?)?.toInt() ?? 0,
        utilisation: (j['utilisation_percent'] as num?)?.toDouble() ?? 0,
        temperatureC: (j['temperature_c'] as num?)?.toDouble() ?? 0,
      );

  double get memoryFraction =>
      memoryTotal == 0 ? 0 : memoryUsed / memoryTotal;
}

/// Fleet-lifetime totals from GET /api/telemetry/financials.
class FinancialSummary {
  const FinancialSummary({
    this.totalPromptTokens = 0,
    this.totalCompletionTokens = 0,
    this.totalCachedTokens = 0,
    this.totalCostUsd = 0,
    this.avgLatencyMs = 0,
    this.turnsCount = 0,
  });

  final int totalPromptTokens;
  final int totalCompletionTokens;
  final int totalCachedTokens;
  final double totalCostUsd;
  final int avgLatencyMs;
  final int turnsCount;

  factory FinancialSummary.fromJson(Map<String, dynamic> j) =>
      FinancialSummary(
        totalPromptTokens: (j['total_prompt_tokens'] as num?)?.toInt() ?? 0,
        totalCompletionTokens:
            (j['total_completion_tokens'] as num?)?.toInt() ?? 0,
        totalCachedTokens: (j['total_cached_tokens'] as num?)?.toInt() ?? 0,
        totalCostUsd: (j['total_cost_usd'] as num?)?.toDouble() ?? 0,
        avgLatencyMs: (j['avg_latency_ms'] as num?)?.toInt() ?? 0,
        turnsCount: (j['turns_count'] as num?)?.toInt() ?? 0,
      );
}

/// One model round-trip: which engine answered, what it cost, how long it took.
class TokenTelemetryRecord {
  const TokenTelemetryRecord({
    required this.id,
    required this.taskId,
    required this.instanceId,
    required this.providerId,
    required this.modelName,
    required this.promptTokens,
    required this.completionTokens,
    required this.costUsd,
    required this.latencyMs,
    required this.createdAt,
  });

  final String id;
  final String taskId;
  final String instanceId;
  final String providerId;
  final String modelName;
  final int promptTokens;
  final int completionTokens;
  final double costUsd;
  final int latencyMs;
  final DateTime createdAt;

  factory TokenTelemetryRecord.fromJson(Map<String, dynamic> j) =>
      TokenTelemetryRecord(
        id: j['id'] as String? ?? '',
        taskId: j['task_id'] as String? ?? '',
        instanceId: j['instance_id'] as String? ?? '',
        providerId: j['provider_id'] as String? ?? '',
        modelName: j['model_name'] as String? ?? '',
        promptTokens: (j['prompt_tokens'] as num?)?.toInt() ?? 0,
        completionTokens: (j['completion_tokens'] as num?)?.toInt() ?? 0,
        costUsd: (j['cost_usd'] as num?)?.toDouble() ?? 0,
        latencyMs: (j['latency_ms'] as num?)?.toInt() ?? 0,
        createdAt: DateTime.tryParse(j['created_at'] as String? ?? '') ??
            DateTime.now(),
      );
}

/// A bot archetype from GET /api/templates: a starting point for provisioning
/// rather than a blank instance.
class BotTemplate {
  BotTemplate({
    required this.id,
    required this.name,
    this.tagline = '',
    this.category = '',
    this.recommendedTier = '',
    this.tools = const [],
    this.defaultVoice = '',
    this.defaultShellAccess = false,
    this.specializedPrompt = '',
  });

  final String id;
  final String name;
  final String tagline;
  final String category;
  final String recommendedTier;

  /// What this archetype asks for. Not everything here can be installed — the
  /// server verifies after provisioning and only keeps what answered.
  final List<String> tools;

  final String defaultVoice;
  final bool defaultShellAccess;

  /// The personality written for this job, used to prefill a new bot's own.
  /// The operator can change it before creating and at any time after.
  final String specializedPrompt;

  factory BotTemplate.fromJson(Map<String, dynamic> j) => BotTemplate(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        tagline: j['tagline'] as String? ?? '',
        category: j['category'] as String? ?? '',
        recommendedTier: j['recommended_tier'] as String? ?? '',
        tools: ((j['preinstalled_tools'] as List?) ?? const [])
            .map((e) => '$e')
            .toList(),
        defaultVoice: j['default_voice'] as String? ?? '',
        defaultShellAccess: j['default_shell_access'] as bool? ?? false,
        specializedPrompt: j['specialized_prompt'] as String? ?? '',
      );
}


class PipelineEdge {
  const PipelineEdge({
    required this.fromNodeId,
    required this.toNodeId,
    this.condition = '',
  });

  final String fromNodeId;
  final String toNodeId;

  /// e.g. "success". Stored and round-tripped, and shown in the DAG view, but
  /// the backend engine does not evaluate it yet: every stage in topological
  /// order runs regardless of how its upstream ended, and the only branching is
  /// the run-wide abort on error. Empty means unconditional.
  final String condition;

  factory PipelineEdge.fromJson(Map<String, dynamic> j) => PipelineEdge(
        fromNodeId: j['from_node_id'] as String? ?? '',
        toNodeId: j['to_node_id'] as String? ?? '',
        condition: j['condition'] as String? ?? '',
      );
}

/// A scheduled wakeup: the fleet starting work on its own, on a cron.
class CronTrigger {
  const CronTrigger({
    required this.id,
    required this.name,
    required this.scheduleCron,
    required this.goalTemplate,
    required this.active,
    this.targetArchetype = '',
    this.targetInstanceId = '',
    this.lastRunAt,
  });

  final String id;
  final String name;
  final String scheduleCron;
  final String goalTemplate;
  final bool active;
  final String targetArchetype;
  final String targetInstanceId;
  final DateTime? lastRunAt;

  factory CronTrigger.fromJson(Map<String, dynamic> j) => CronTrigger(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        scheduleCron: j['schedule_cron'] as String? ?? '',
        goalTemplate: j['goal_template'] as String? ?? '',
        active: j['active'] as bool? ?? false,
        targetArchetype: j['target_archetype'] as String? ?? '',
        targetInstanceId: j['target_instance_id'] as String? ?? '',
        lastRunAt: DateTime.tryParse(j['last_run_at'] as String? ?? ''),
      );
}

/// An inbound hook that starts work when something outside calls in.
class WebhookTrigger {
  const WebhookTrigger({
    required this.id,
    required this.name,
    required this.goalTemplate,
    this.targetArchetype = '',
    this.targetInstanceId = '',
    this.token = '',
    this.kind = 'generic',
    this.hasSecret = true,
    this.active = true,
    this.lastTriggeredAt,
    this.createdAt,
  });

  /// Which sender the hook expects; selects the signature scheme. A Stripe
  /// webhook filed as generic rejects every delivery, so it has to be visible.
  static const kinds = ['generic', 'github', 'stripe', 'crm'];

  static const kindHints = {
    'generic':
        'HMAC-SHA256 of the body in X-Hub-Signature-256 or X-OpenAgentFleet-Signature.',
    'github':
        "Paste the secret into the repository's webhook settings. Events are summarised.",
    'stripe':
        'Use the whsec_… signing secret from the Stripe dashboard, not your API key.',
    'crm':
        'A bare JSON document. Contact and deal fields are extracted where present.',
  };

  final String id;
  final String name;
  final String goalTemplate;
  final String targetArchetype;
  final String targetInstanceId;

  /// The path segment external senders call: POST /api/webhooks/<token>.
  final String token;
  final String kind;

  /// The signing secret itself never comes back from the API; this says
  /// whether one is set. Without one the endpoint refuses every delivery.
  final bool hasSecret;
  final bool active;
  final DateTime? lastTriggeredAt;
  final DateTime? createdAt;

  factory WebhookTrigger.fromJson(Map<String, dynamic> j) => WebhookTrigger(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        goalTemplate: j['goal_template'] as String? ?? '',
        targetArchetype: j['target_archetype'] as String? ?? '',
        targetInstanceId: j['target_instance_id'] as String? ?? '',
        token: j['token'] as String? ?? '',
        kind: j['kind'] as String? ?? 'generic',
        hasSecret: j['has_secret'] as bool? ?? true,
        active: j['active'] as bool? ?? true,
        lastTriggeredAt:
            DateTime.tryParse(j['last_triggered_at'] as String? ?? ''),
        createdAt: DateTime.tryParse(j['created_at'] as String? ?? ''),
      );
}


/// A voice offered by the server's speech service.
class ServerVoice {
  const ServerVoice({
    required this.id,
    required this.name,
    this.description = '',
    this.preset = false,
  });

  final String id;
  final String name;
  final String description;

  /// One of the fleet's named presets rather than a raw model speaker.
  final bool preset;

  factory ServerVoice.fromJson(Map<String, dynamic> j) => ServerVoice(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        description: j['description'] as String? ?? '',
        preset: j['preset'] as bool? ?? false,
      );
}


/// The signed-in operator.
///
/// The app used to keep only the token, so it had no idea what the person
/// using it was allowed to do — every screen was offered to everyone and the
/// server refused the ones they could not have. Knowing the role lets the
/// admin surfaces be hidden rather than merely denied.
class CurrentUser {
  const CurrentUser({
    required this.id,
    required this.email,
    required this.role,
  });

  final String id;
  final String email;

  /// 'admin', 'operator', 'auditor' or 'viewer'.
  final String role;

  /// Deployment administrator: provider connections, users, departments and
  /// API keys. Not the same as an org role, which scopes one department.
  bool get isAdmin => role == 'admin';

  factory CurrentUser.fromJson(Map<String, dynamic> j) => CurrentUser(
        id: j['id'] as String? ?? '',
        email: j['email'] as String? ?? '',
        role: j['role'] as String? ?? '',
      );
}


/// A person with an account on this deployment.
class AdminUser {
  const AdminUser({
    required this.id,
    required this.email,
    required this.role,
    required this.createdAt,
    this.disabledAt,
  });

  final String id;
  final String email;
  final String role;
  final DateTime createdAt;

  /// Set when the account has been turned off. Disabled rather than deleted,
  /// so what they did stays traceable and their keys die with them.
  final DateTime? disabledAt;

  bool get disabled => disabledAt != null;

  static const roles = ['admin', 'operator', 'auditor', 'viewer'];

  factory AdminUser.fromJson(Map<String, dynamic> j) => AdminUser(
        id: j['id'] as String? ?? '',
        email: j['email'] as String? ?? '',
        role: j['role'] as String? ?? 'operator',
        createdAt: DateTime.tryParse(j['created_at'] as String? ?? '') ??
            DateTime.now(),
        disabledAt: DateTime.tryParse(j['disabled_at'] as String? ?? ''),
      );
}

/// A long-lived access key for scripts and CI.
class ApiKey {
  const ApiKey({
    required this.id,
    required this.name,
    required this.userEmail,
    required this.createdAt,
    this.lastUsedAt,
    this.revokedAt,
    this.secret = '',
  });

  final String id;
  final String name;
  final String userEmail;
  final DateTime createdAt;

  /// Null for a key that has never been used, which is how a key issued and
  /// forgotten is told apart from one in daily service.
  final DateTime? lastUsedAt;
  final DateTime? revokedAt;

  /// Only ever set on the response that creates the key. There is no second
  /// copy to fetch later.
  final String secret;

  bool get revoked => revokedAt != null;

  factory ApiKey.fromJson(Map<String, dynamic> j) => ApiKey(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        userEmail: j['user_email'] as String? ?? '',
        createdAt: DateTime.tryParse(j['created_at'] as String? ?? '') ??
            DateTime.now(),
        lastUsedAt: DateTime.tryParse(j['last_used_at'] as String? ?? ''),
        revokedAt: DateTime.tryParse(j['revoked_at'] as String? ?? ''),
        secret: j['secret'] as String? ?? '',
      );
}


/// Something an agent published to the shared work catalog.
///
/// Agents could message each other and share credentials, but had nowhere to
/// put the work itself, so whatever one produced lived in its container and
/// died with it. Kinds differ in what this app does with the content, not in
/// how it is stored.
class WorkItem {
  const WorkItem({
    required this.id,
    required this.name,
    required this.kind,
    required this.version,
    required this.updatedAt,
    this.description = '',
    this.content = '',
    this.mime = '',
    this.createdByName = '',
    this.parentId = '',
  });

  /// Text: notes, code, data.
  static const kindFile = 'file';

  /// A self-contained HTML document this app can render and run.
  static const kindApp = 'app';

  /// A group of related items several agents worked on together.
  static const kindWorkspace = 'workspace';

  final String id;
  final String name;

  /// 'file', 'app' or 'workspace'.
  final String kind;
  final String description;
  final String content;
  final String mime;

  /// The bot that made it, or the operator's email.
  final String createdByName;

  /// Set when this item belongs inside a workspace.
  final String parentId;

  /// Bumped on every write, so improving a colleague's work reads as an edit
  /// rather than as a second copy.
  final int version;
  final DateTime updatedAt;

  bool get isApp => kind == kindApp;
  bool get isWorkspace => kind == kindWorkspace;

  /// Whether there is actually something to run.
  bool get runnable => isApp && content.trim().isNotEmpty;

  factory WorkItem.fromJson(Map<String, dynamic> j) => WorkItem(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        kind: j['kind'] as String? ?? 'file',
        description: j['description'] as String? ?? '',
        content: j['content'] as String? ?? '',
        mime: j['mime'] as String? ?? '',
        createdByName: j['created_by_name'] as String? ?? '',
        parentId: j['parent_id'] as String? ?? '',
        version: (j['version'] as num?)?.toInt() ?? 1,
        updatedAt: DateTime.tryParse(j['updated_at'] as String? ?? '') ??
            DateTime.now(),
      );
}


/// An external MCP tool server mounted on the fleet.
class McpServer {
  const McpServer({
    required this.id,
    required this.name,
    required this.transport,
    required this.command,
    this.url = '',
    this.toolsCount = 0,
    this.active = true,
    this.createdAt,
  });

  final String id;
  final String name;

  /// 'stdio' (a subprocess command) or 'sse' (a remote URL).
  final String transport;
  final String command;
  final String url;
  final int toolsCount;
  final bool active;
  final DateTime? createdAt;

  /// What to show as the address: the command for stdio, the URL for sse.
  String get endpoint => transport == 'sse' && url.isNotEmpty ? url : command;

  factory McpServer.fromJson(Map<String, dynamic> j) => McpServer(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        transport: j['transport'] as String? ?? 'stdio',
        command: j['command'] as String? ?? '',
        url: j['url'] as String? ?? '',
        toolsCount: (j['tools_count'] as num?)?.toInt() ?? 0,
        active: j['active'] as bool? ?? true,
        createdAt: DateTime.tryParse(j['created_at'] as String? ?? ''),
      );
}

/// A tool an MCP server offers, discovered by asking the server itself.
class McpTool {
  const McpTool({
    required this.serverId,
    required this.name,
    this.description = '',
  });

  final String serverId;
  final String name;
  final String description;

  factory McpTool.fromJson(Map<String, dynamic> j) => McpTool(
        serverId: j['server_id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        description: j['description'] as String? ?? '',
      );
}

/// A sealed platform credential, metadata only. The value is encrypted under
/// MASTER_KEY on the server and is never readable through the API.
class SecretRef {
  const SecretRef({required this.ref, this.note = '', this.updatedAt});

  final String ref;
  final String note;
  final DateTime? updatedAt;

  factory SecretRef.fromJson(Map<String, dynamic> j) => SecretRef(
        ref: j['ref'] as String? ?? '',
        note: j['note'] as String? ?? '',
        updatedAt: DateTime.tryParse(j['updated_at'] as String? ?? ''),
      );
}

/// The orchestrator's own health figures, from GET /healthz.
class PlatformHealth {
  const PlatformHealth({
    this.status = '?',
    this.liveInstances = 0,
    this.maxInstances = 0,
    this.wsSubscribers = 0,
    this.eventsDropped = 0,
  });

  final String status;
  final int liveInstances;
  final int maxInstances;

  /// Connected console/app clients on the event bus.
  final int wsSubscribers;

  /// Events the bus shed because a subscriber could not keep up. Nonzero means
  /// some client somewhere rendered a stale picture.
  final int eventsDropped;

  factory PlatformHealth.fromJson(Map<String, dynamic> j) => PlatformHealth(
        status: '${j['status'] ?? '?'}',
        liveInstances: (j['live_instances'] as num?)?.toInt() ?? 0,
        maxInstances: (j['max_instances'] as num?)?.toInt() ?? 0,
        wsSubscribers: (j['ws_subscribers'] as num?)?.toInt() ?? 0,
        eventsDropped: (j['events_dropped'] as num?)?.toInt() ?? 0,
      );
}

/// A portable archetype package.
///
/// Kept as the raw map plus typed accessors rather than fully parsed: import
/// posts the manifest back to the server verbatim, and round-tripping through
/// a typed model would silently drop any field this app does not know about.
class ArchetypeManifest {
  const ArchetypeManifest(this.raw);

  final Map<String, dynamic> raw;

  String get id => '${raw['id'] ?? ''}';
  String get name => '${raw['name'] ?? ''}';
  String get category => '${raw['category'] ?? ''}';
  String get recommendedTier => '${raw['recommended_tier'] ?? ''}';
  double get vcpu => (raw['vcpu'] as num?)?.toDouble() ?? 0;
  int get memoryMb => (raw['memory_mb'] as num?)?.toInt() ?? 0;
  bool get defaultShellAccess => raw['default_shell_access'] == true;
  List<dynamic> get tools => (raw['preinstalled_tools'] as List?) ?? const [];
  List<dynamic> get recordedSkills =>
      (raw['recorded_skills'] as List?) ?? const [];
  List<dynamic> get mcpServers => (raw['mcp_servers'] as List?) ?? const [];

  /// MCP env keys the installer must supply, named "server.KEY". Named up
  /// front rather than discovered as an auth failure later.
  List<String> get neededEnvKeys => [
        for (final s in mcpServers.whereType<Map>())
          for (final k in (s['env_keys'] as List?) ?? const [])
            '${s['name'] ?? ''}.$k',
      ];
}

/// What an archetype import actually did, itemised — "imported successfully"
/// is what the old CLI printed while creating nothing.
class ImportArchetypeResult {
  const ImportArchetypeResult({
    this.archetype = '',
    this.skillsCreated = const [],
    this.skillsSkipped = const [],
    this.mcpRegistered = const [],
    this.mcpFailed = const [],
    this.needsSecrets = const [],
    this.instanceId = '',
    this.instanceName = '',
    this.instanceStatus = '',
  });

  final String archetype;
  final List<String> skillsCreated;
  final List<String> skillsSkipped;
  final List<String> mcpRegistered;
  final List<String> mcpFailed;
  final List<String> needsSecrets;
  final String instanceId;
  final String instanceName;
  final String instanceStatus;

  bool get nothingInstalled =>
      skillsCreated.isEmpty && mcpRegistered.isEmpty && instanceId.isEmpty;

  static List<String> _list(dynamic v) =>
      ((v as List?) ?? const []).map((e) => '$e').toList();

  factory ImportArchetypeResult.fromJson(Map<String, dynamic> j) =>
      ImportArchetypeResult(
        archetype: j['archetype'] as String? ?? '',
        skillsCreated: _list(j['skills_created']),
        skillsSkipped: _list(j['skills_skipped']),
        mcpRegistered: _list(j['mcp_registered']),
        mcpFailed: _list(j['mcp_failed']),
        needsSecrets: _list(j['needs_secrets']),
        instanceId: j['instance_id'] as String? ?? '',
        instanceName: j['instance_name'] as String? ?? '',
        instanceStatus: j['instance_status'] as String? ?? '',
      );
}
