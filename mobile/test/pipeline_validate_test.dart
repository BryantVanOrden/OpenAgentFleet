import 'package:flutter_test/flutter_test.dart';

import 'package:agentfleet_companion/features/pipelines/pipeline_draft.dart';

// The editor disables Save on any problem, so the validator IS the gate: a
// rule it misses becomes a server round-trip error, and a rule it invents
// blocks a pipeline the server would happily run.
void main() {
  DraftNode node(String id, {String goal = 'do things', String role = 'r'}) =>
      DraftNode(id: id, name: id, goalTemplate: goal, archetypeId: role);

  PipelineDraft draft({List<DraftNode>? nodes, List<DraftEdge>? edges}) =>
      PipelineDraft(name: 'p', nodes: nodes, edges: edges);

  test('a well-formed pipeline has no problems', () {
    final d = draft(
      nodes: [node('a'), node('b')],
      edges: [DraftEdge(fromNodeId: 'a', toNodeId: 'b', condition: 'success')],
    );
    expect(validateDraft(d), isEmpty);
  });

  test('missing name, empty goal, and unassigned stage are each caught', () {
    final d = PipelineDraft(
      name: '',
      nodes: [DraftNode(id: 'a', name: 'first')],
    );
    final problems = validateDraft(d).join('\n');
    expect(problems, contains('needs a name'));
    expect(problems, contains('no goal'));
    expect(problems, contains('which bot or role'));
  });

  test('duplicate stage ids are caught', () {
    final d = draft(nodes: [node('a'), node('a')]);
    expect(validateDraft(d).join(), contains('share the id'));
  });

  test('self-dependency is caught', () {
    final d = draft(
      nodes: [node('a')],
      edges: [DraftEdge(fromNodeId: 'a', toNodeId: 'a')],
    );
    expect(validateDraft(d).join(), contains('depend on itself'));
  });

  test('argument-taking conditions demand their text', () {
    final d = draft(
      nodes: [node('a'), node('b')],
      edges: [DraftEdge(fromNodeId: 'a', toNodeId: 'b', condition: 'contains:')],
    );
    expect(validateDraft(d).join(), contains('needs text'));
  });

  test('an invalid regex on matches: is caught, a valid one passes', () {
    final bad = draft(
      nodes: [node('a'), node('b')],
      edges: [
        DraftEdge(fromNodeId: 'a', toNodeId: 'b', condition: 'matches:[unclosed'),
      ],
    );
    expect(validateDraft(bad).join(), contains('regex'));

    final good = draft(
      nodes: [node('a'), node('b')],
      edges: [
        DraftEdge(fromNodeId: 'a', toNodeId: 'b', condition: r'matches:^deploy \d+$'),
      ],
    );
    expect(validateDraft(good), isEmpty);
  });

  test('a cycle through three stages is caught', () {
    final d = draft(
      nodes: [node('a'), node('b'), node('c')],
      edges: [
        DraftEdge(fromNodeId: 'a', toNodeId: 'b'),
        DraftEdge(fromNodeId: 'b', toNodeId: 'c'),
        DraftEdge(fromNodeId: 'c', toNodeId: 'a'),
      ],
    );
    expect(validateDraft(d).join(), contains('cycle'));
  });

  test('a diamond is not a cycle', () {
    final d = draft(
      nodes: [node('a'), node('b'), node('c'), node('d')],
      edges: [
        DraftEdge(fromNodeId: 'a', toNodeId: 'b'),
        DraftEdge(fromNodeId: 'a', toNodeId: 'c'),
        DraftEdge(fromNodeId: 'b', toNodeId: 'd'),
        DraftEdge(fromNodeId: 'c', toNodeId: 'd'),
      ],
    );
    expect(hasCycle(d), isFalse);
    expect(validateDraft(d), isEmpty);
  });

  test('edges naming removed stages do not crash the cycle check', () {
    final d = draft(
      nodes: [node('a')],
      edges: [DraftEdge(fromNodeId: 'ghost', toNodeId: 'a')],
    );
    expect(hasCycle(d), isFalse);
  });
}
