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
    required this.createdAt,
    this.lastError = '',
  });

  final String id;
  final String name;
  final String tier;
  final String state;
  final TierProfile profile;
  final bool shellAccess;
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
  });

  final String id;
  final String role;
  final String body;
  final DateTime createdAt;

  bool get isUser => role == 'user';

  factory ChatMessage.fromJson(Map<String, dynamic> j) => ChatMessage(
        id: j['id'] as String? ?? '',
        role: j['role'] as String? ?? 'agent',
        body: j['body'] as String? ?? '',
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
        createdAt: DateTime.tryParse(j['created_at'] as String? ?? '') ??
            DateTime.now(),
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
