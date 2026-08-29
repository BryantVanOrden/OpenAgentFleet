import 'package:flutter_test/flutter_test.dart';
import 'package:agentfleet_companion/core/models.dart';

Conversation conv(String id, List<String> members, {String kind = 'pair'}) =>
    Conversation(
      id: id,
      kind: kind,
      title: id,
      members: members,
      messageCount: 0,
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

  test('the broadcast channel is its own group', () {
    // Grouping it with operator-created everyone-chats would hide the one
    // thread that cannot be deleted behind ones that can.
    final broadcast = conv(Conversation.broadcastId, const [], kind: 'group');
    final everyone = conv('other', const [], kind: 'group');

    expect(broadcast.isBroadcast, isTrue);
    expect(broadcast.participantKey, Conversation.broadcastId);
    expect(everyone.participantKey, isNot(Conversation.broadcastId));
  });

  test('a group of three keys separately from either of its pairs', () {
    final trio = conv('t', ['x', 'y', 'z'], kind: 'group');
    expect(trio.participantKey, isNot(conv('p', ['x', 'y']).participantKey));
    expect(trio.participantKey, isNot(conv('q', ['y', 'z']).participantKey));
  });
}
