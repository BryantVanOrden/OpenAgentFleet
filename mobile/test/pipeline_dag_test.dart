import 'package:agentfleet_companion/core/models.dart';
import 'package:flutter_test/flutter_test.dart';

PipelineNode _n(String id) =>
    PipelineNode(id: id, name: id, archetypeId: 'x', goalTemplate: '');

void main() {
  group('pipeline dependency layers', () {
    // With no edges the API tells us nothing about ordering, so the honest
    // reading is a straight line — one stage per layer.
    test('no edges renders as a sequence', () {
      final p = WorkflowPipeline(
          id: 'p', name: 'p', nodes: [_n('a'), _n('b'), _n('c')]);
      expect(p.layers.map((l) => l.map((n) => n.id).toList()),
          [['a'], ['b'], ['c']]);
    });

    // The whole point of reading edges: independent stages belong on the same
    // row, which a flat chip list could never show.
    test('independent stages share a layer', () {
      final p = WorkflowPipeline(
        id: 'p',
        name: 'p',
        nodes: [_n('root'), _n('left'), _n('right'), _n('join')],
        edges: const [
          PipelineEdge(fromNodeId: 'root', toNodeId: 'left'),
          PipelineEdge(fromNodeId: 'root', toNodeId: 'right'),
          PipelineEdge(fromNodeId: 'left', toNodeId: 'join'),
          PipelineEdge(fromNodeId: 'right', toNodeId: 'join'),
        ],
      );
      final layers = p.layers.map((l) => l.map((n) => n.id).toSet()).toList();
      expect(layers, [
        {'root'},
        {'left', 'right'},
        {'join'},
      ]);
    });

    // A cycle must not hang the UI. Show what can be ordered, then the rest.
    test('a cycle degrades instead of looping forever', () {
      final p = WorkflowPipeline(
        id: 'p',
        name: 'p',
        nodes: [_n('a'), _n('b')],
        edges: const [
          PipelineEdge(fromNodeId: 'a', toNodeId: 'b'),
          PipelineEdge(fromNodeId: 'b', toNodeId: 'a'),
        ],
      );
      final all = p.layers.expand((l) => l).map((n) => n.id).toSet();
      expect(all, {'a', 'b'}, reason: 'every stage must still be shown');
    });

    // An edge naming a stage that is not in the pipeline must be ignored
    // rather than stranding the node it points at.
    test('edges to unknown nodes are ignored', () {
      final p = WorkflowPipeline(
        id: 'p',
        name: 'p',
        nodes: [_n('a')],
        edges: const [PipelineEdge(fromNodeId: 'ghost', toNodeId: 'a')],
      );
      expect(p.layers.expand((l) => l).map((n) => n.id), ['a']);
    });
  });

  group('chat plan state', () {
    ChatMessage msg(String kind, String state) => ChatMessage.fromJson({
          'id': 'm', 'role': 'agent', 'body': 'b',
          'kind': kind, 'plan_state': state,
        });

    test('only an unanswered plan offers approval', () {
      expect(msg('plan', '').isOpenPlan, isTrue);
      expect(msg('plan', 'approved').isOpenPlan, isFalse);
      expect(msg('plan', 'discarded').isOpenPlan, isFalse);
      expect(msg('message', '').isOpenPlan, isFalse);
    });

    // Rows written before the columns existed come back without them and must
    // read as ordinary messages, not as plans awaiting a decision.
    test('a message from before the plan columns is not a plan', () {
      final old = ChatMessage.fromJson({'id': 'm', 'role': 'agent', 'body': 'b'});
      expect(old.kind, 'message');
      expect(old.isOpenPlan, isFalse);
    });
  });
}
