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
    this.orgId = '',
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

  /// The department this bot belongs to. Empty is unassigned, which only a
  /// deployment administrator can see.
  final String orgId;

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
        orgId: j['org_id'] as String? ?? '',
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

class Skill {
  Skill({required this.id, required this.name, required this.stepCount});

  final String id;
  final String name;
  final int stepCount;

  factory Skill.fromJson(Map<String, dynamic> j) => Skill(
        id: j['id'] as String,
        name: j['name'] as String? ?? '',
        stepCount: (j['steps'] as List?)?.length ?? 0,
      );
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
  });

  final String id;
  final String title;
  final String author;
  final String category;
  final String content;
  final DateTime createdAt;

  factory SwarmArtifact.fromJson(Map<String, dynamic> j) => SwarmArtifact(
        id: j['id'] as String? ?? '',
        title: j['title'] as String? ?? '',
        author: j['author'] as String? ?? '',
        category: j['category'] as String? ?? '',
        content: j['content'] as String? ?? '',
        createdAt: DateTime.tryParse(j['created_at'] as String? ?? '') ??
            DateTime.now(),
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
  });

  final String id;
  final String name;
  final String archetypeId;
  final String goalTemplate;

  factory PipelineNode.fromJson(Map<String, dynamic> j) => PipelineNode(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        archetypeId: j['archetype_id'] as String? ?? 'fullstack_dev',
        goalTemplate: j['goal_template'] as String? ?? '',
      );
}

class WorkflowPipeline {
  WorkflowPipeline({
    required this.id,
    required this.name,
    this.description = '',
    required this.nodes,
    this.edges = const [],
  });

  final String id;
  final String name;
  final String description;
  final List<PipelineNode> nodes;

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
      );

  /// Stages grouped into dependency layers: nothing in layer 0 depends on
  /// anything else, layer 1 waits on layer 0, and so on. This describes the
  /// dependency structure, not the execution schedule — the engine runs stages
  /// one at a time in topological order, so a layer is a set of stages whose
  /// relative order does not matter, not a set that runs at once. With no edges
  /// the pipeline is a straight line, which is what the flat list used to imply
  /// for every pipeline whether it was true or not.
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
  });

  final String id;
  final String pipelineId;
  final String status;
  final DateTime startedAt;

  factory PipelineRun.fromJson(Map<String, dynamic> j) => PipelineRun(
        id: j['id'] as String? ?? '',
        pipelineId: j['pipeline_id'] as String? ?? '',
        status: j['status'] as String? ?? 'running',
        startedAt: DateTime.tryParse(j['started_at'] as String? ?? '') ?? DateTime.now(),
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
  });

  final String id;
  final String name;
  final String goalTemplate;
  final String targetArchetype;
  final String targetInstanceId;

  factory WebhookTrigger.fromJson(Map<String, dynamic> j) => WebhookTrigger(
        id: j['id'] as String? ?? '',
        name: j['name'] as String? ?? '',
        goalTemplate: j['goal_template'] as String? ?? '',
        targetArchetype: j['target_archetype'] as String? ?? '',
        targetInstanceId: j['target_instance_id'] as String? ?? '',
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
