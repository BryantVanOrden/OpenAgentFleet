import 'package:flutter_test/flutter_test.dart';
import 'package:agentfleet_companion/core/models.dart';

void main() {
  test('a conversation parses its members and kind', () {
    final c = Conversation.fromJson(const {
      'id': 'conv-abc',
      'kind': 'pair',
      'title': 'Scouting',
      'members': ['a', 'b'],
      'message_count': 3,
      'last_message_at': '2026-08-29T10:00:00Z',
    });

    expect(c.id, 'conv-abc');
    expect(c.isPair, isTrue);
    expect(c.isBroadcast, isFalse);
    expect(c.members, ['a', 'b']);
    expect(c.messageCount, 3);
  });

  test('a conversation survives a response missing every optional field', () {
    // The server omits last_message_at for a thread nobody has spoken in, and
    // an empty members list marshals as absent. Neither should throw.
    final c = Conversation.fromJson(const {'id': 'broadcast'});

    expect(c.isBroadcast, isTrue);
    expect(c.members, isEmpty);
    expect(c.lastMessageAt, isNull);
    expect(c.messageCount, 0);
  });

  test('a summary message reports how many messages it replaced', () {
    final m = PeerMessage.fromJson(const {
      'id': 'peer-msg-1',
      'conversation_id': 'conv-abc',
      'from_instance_name': 'Summary',
      'kind': 'summary',
      'content': 'they agreed on a plan',
      'data': {'compacted_messages': 12},
    });

    expect(m.kind, 'summary');
    expect(m.compactedCount, 12);
    expect(m.conversationId, 'conv-abc');
  });

  test('an ordinary message has no compaction count', () {
    final m = PeerMessage.fromJson(const {
      'id': 'peer-msg-2',
      'from_instance_id': 'a',
      'content': 'hello',
    });

    expect(m.compactedCount, 0);
    expect(m.conversationId, isEmpty);
  });

  test('a bot memory parses what the agent kept', () {
    final m = BotMemory.fromJson(const {
      'id': 'mem-1',
      'title': 'build flag',
      'content': 'the dev image needs --no-sandbox',
      'tags': ['agent', 'scout'],
      'created_at': '2026-08-29T10:00:00Z',
      'source_task_id': 'task-9',
    });

    expect(m.title, 'build flag');
    expect(m.tags, ['agent', 'scout']);
    expect(m.sourceTaskId, 'task-9');
  });
}
