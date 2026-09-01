import '../../core/models.dart';

/// The editable form of a pipeline, and the validation that mirrors the
/// server's. Pure Dart — no Flutter imports — so the rules that decide
/// whether Save is offered are testable without a widget tree.

/// The condition vocabulary, fixed and validated server-side at save. The
/// last four take a free-text argument after the colon.
const edgeConditions = [
  (value: '', label: 'always (plain dependency)'),
  (value: 'success', label: 'only if it succeeded'),
  (value: 'failure', label: 'only if it failed'),
  (value: 'contains:', label: 'only if the result contains…'),
  (value: 'not_contains:', label: 'only if the result does not contain…'),
  (value: 'equals:', label: 'only if the result is exactly…'),
  (value: 'matches:', label: 'only if the result matches the regex…'),
];

/// Splits "contains:approved" into its dropdown value and its free-text half,
/// so switching between two conditions that both take text keeps what was
/// typed.
({String kind, String arg}) splitCondition(String condition) {
  final c = condition.trim();
  if (c.isEmpty) return (kind: '', arg: '');
  final idx = c.indexOf(':');
  if (idx < 0) return (kind: c, arg: '');
  return (kind: c.substring(0, idx + 1), arg: c.substring(idx + 1));
}

class DraftNode {
  DraftNode({
    required this.id,
    this.name = '',
    this.goalTemplate = '',
    this.archetypeId = '',
    this.instanceId = '',
  });

  final String id;
  String name;
  String goalTemplate;

  /// A node names an instance or an archetype, never both — the editor's
  /// single "Run on" selector enforces it, and validation catches neither.
  String archetypeId;
  String instanceId;

  factory DraftNode.from(PipelineNode n) => DraftNode(
        id: n.id,
        name: n.name,
        goalTemplate: n.goalTemplate,
        archetypeId: n.archetypeId,
        instanceId: n.instanceId,
      );

  Map<String, dynamic> toJson() => {
        'id': id,
        'name': name.trim(),
        'goal_template': goalTemplate.trim(),
        'archetype_id': archetypeId,
        'instance_id': instanceId,
      };
}

class DraftEdge {
  DraftEdge({
    required this.fromNodeId,
    required this.toNodeId,
    this.condition = '',
  });

  String fromNodeId;
  String toNodeId;
  String condition;

  factory DraftEdge.from(PipelineEdge e) => DraftEdge(
        fromNodeId: e.fromNodeId,
        toNodeId: e.toNodeId,
        condition: e.condition,
      );

  Map<String, dynamic> toJson() => {
        'from_node_id': fromNodeId,
        'to_node_id': toNodeId,
        'condition': condition,
      };
}

class PipelineDraft {
  PipelineDraft({
    this.name = '',
    this.description = '',
    List<DraftNode>? nodes,
    List<DraftEdge>? edges,
    this.maxParallel = 0,
  })  : nodes = nodes ?? [blankNode(1)],
        edges = edges ?? [];

  String name;
  String description;
  final List<DraftNode> nodes;
  final List<DraftEdge> edges;
  int maxParallel;

  /// One empty node for a new pipeline, because a pipeline needs at least one
  /// and an empty form gives no hint about what a stage consists of.
  static DraftNode blankNode(int n) => DraftNode(id: 'node-$n');

  factory PipelineDraft.from(WorkflowPipeline p) => PipelineDraft(
        name: p.name,
        description: p.description,
        nodes: [for (final n in p.nodes) DraftNode.from(n)],
        edges: [for (final e in p.edges) DraftEdge.from(e)],
        maxParallel: p.maxParallel,
      );

  /// The next stage id, numbered past every taken id so removing node-2 and
  /// adding one does not collide with a node-2 an edge still references.
  String nextNodeId() {
    var n = nodes.length + 1;
    final taken = {for (final x in nodes) x.id};
    while (taken.contains('node-$n')) {
      n++;
    }
    return 'node-$n';
  }
}

/// Local validation, mirroring the server's Validate. The server stays
/// authoritative and rejects the same things; running it here means the
/// operator does not have to press Save to find out.
List<String> validateDraft(PipelineDraft d) {
  final out = <String>[];
  if (d.name.trim().isEmpty) out.add('The pipeline needs a name.');
  if (d.nodes.isEmpty) out.add('The pipeline needs at least one stage.');

  final seen = <String>{};
  for (final n in d.nodes) {
    final label = n.name.trim().isEmpty ? n.id : n.name.trim();
    if (!seen.add(n.id)) out.add('Two stages share the id ${n.id}.');
    if (n.goalTemplate.trim().isEmpty) out.add('Stage "$label" has no goal.');
    if (n.instanceId.trim().isEmpty && n.archetypeId.trim().isEmpty) {
      out.add('Stage "$label" does not say which bot or role runs it.');
    }
  }

  for (final e in d.edges) {
    if (e.fromNodeId == e.toNodeId) {
      out.add('A stage cannot depend on itself (${e.fromNodeId}).');
    }
    final c = splitCondition(e.condition);
    if (c.kind.endsWith(':') && c.arg.trim().isEmpty) {
      out.add('The ${c.kind.substring(0, c.kind.length - 1)} condition on '
          '${e.fromNodeId} → ${e.toNodeId} needs text.');
    }
    if (c.kind == 'matches:' && c.arg.trim().isNotEmpty) {
      try {
        RegExp(c.arg);
      } catch (_) {
        out.add('The regex on ${e.fromNodeId} → ${e.toNodeId} is not valid.');
      }
    }
  }

  if (hasCycle(d)) {
    out.add('The dependencies form a cycle, so no order can run them.');
  }
  return out;
}

/// Kahn's algorithm; the same check the server does before storing.
bool hasCycle(PipelineDraft d) {
  final indegree = <String, int>{for (final n in d.nodes) n.id: 0};
  final next = <String, List<String>>{};
  for (final e in d.edges) {
    if (!indegree.containsKey(e.fromNodeId) ||
        !indegree.containsKey(e.toNodeId)) {
      continue;
    }
    if (e.fromNodeId == e.toNodeId) return true;
    indegree[e.toNodeId] = indegree[e.toNodeId]! + 1;
    (next[e.fromNodeId] ??= []).add(e.toNodeId);
  }

  final ready = [
    for (final entry in indegree.entries)
      if (entry.value == 0) entry.key,
  ];
  var settled = 0;
  while (ready.isNotEmpty) {
    final id = ready.removeLast();
    settled++;
    for (final dep in next[id] ?? const <String>[]) {
      final left = indegree[dep]! - 1;
      indegree[dep] = left;
      if (left == 0) ready.add(dep);
    }
  }
  return settled != d.nodes.length;
}
