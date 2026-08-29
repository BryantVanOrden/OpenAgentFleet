import 'package:flutter_test/flutter_test.dart';
import 'package:agentfleet_companion/core/models.dart';

Conversation conv(String id, List<String> members,
        {String kind = 'pair', DateTime? lastMessageAt, DateTime? createdAt}) =>
    Conversation(
      id: id,
      kind: kind,
      title: id,
      members: members,
      messageCount: 0,
      lastMessageAt: lastMessageAt,
      createdAt: createdAt,
    );

void main() {
  test('threads with the same people share a key whatever the member order',
      () {
    expect(conv('a', ['x', 'y']).participantKey,
        conv('b', ['y', 'x']).participantKey);
  });

  test('different people do not collapse together', () {
    expect(conv('a', ['x', 'y']).participantKey,
        isNot(conv('b', ['x', 'z']).participantKey));
  });

  test('every everyone-channel groups with the built-in one', () {
    // A new chat opened from the broadcast is between the same people --
    // everyone -- so it belongs in that row rather than one of its own.
    // Keying them apart is what made "new chat" look like it had left the
    // broadcast and made a stray thread somewhere else.
    final broadcast = conv(Conversation.broadcastId, const [], kind: 'broadcast');
    final another = conv('conv-1', const [], kind: 'broadcast');

    expect(broadcast.isBroadcast, isTrue);
    expect(another.isBroadcast, isFalse, reason: 'only one built-in channel');
    expect(another.isEveryone, isTrue);
    expect(another.participantKey, broadcast.participantKey);
  });

  test('an everyone-channel does not collapse into an ordinary group', () {
    final everyone = conv('e', const [], kind: 'broadcast');
    final trio = conv('t', ['x', 'y', 'z'], kind: 'group');
    expect(everyone.participantKey, isNot(trio.participantKey));
  });

  test('a group of three keys separately from either of its pairs', () {
    final trio = conv('t', ['x', 'y', 'z'], kind: 'group');
    expect(trio.participantKey, isNot(conv('p', ['x', 'y']).participantKey));
    expect(trio.participantKey, isNot(conv('q', ['y', 'z']).participantKey));
  });

  test('a brand-new chat is the most recently used one in its group', () {
    // Tapping a row opens group.first, and the group is ordered by
    // lastUsedAt. A new chat has no messages, so ordering on last message
    // alone put it LAST -- tapping Everyone opened the oldest thread and the
    // chat just made was the hardest one to reach.
    final old = conv('old', const [],
        kind: 'broadcast', lastMessageAt: DateTime(2026, 8, 1));
    final fresh = conv('fresh', const [],
        kind: 'broadcast', createdAt: DateTime(2026, 8, 29));

    final group = [old, fresh]
      ..sort((a, b) => b.lastUsedAt.compareTo(a.lastUsedAt));

    expect(group.first.id, 'fresh');
  });

  test('a thread that has been talked in beats one merely opened earlier', () {
    final chatty = conv('chatty', const [],
        kind: 'broadcast',
        createdAt: DateTime(2026, 8, 1),
        lastMessageAt: DateTime(2026, 8, 29, 12));
    final quiet = conv('quiet', const [],
        kind: 'broadcast', createdAt: DateTime(2026, 8, 29, 9));

    final group = [quiet, chatty]
      ..sort((a, b) => b.lastUsedAt.compareTo(a.lastUsedAt));

    expect(group.first.id, 'chatty');
  });
}
