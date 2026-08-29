import 'package:flutter_test/flutter_test.dart';
import 'package:agentfleet_companion/core/models.dart';

/// The list arrives pinned-first, which is the right order to read but the
/// wrong one to resume into: returning to a bot should reopen the conversation
/// you were last having, not whichever chat sorts first.
ChatSession mostRecent(List<ChatSession> sessions) {
  ChatSession best = sessions.first;
  for (final c in sessions) {
    final at = c.lastMessageAt;
    if (at == null) continue;
    if (best.lastMessageAt == null || at.isAfter(best.lastMessageAt!)) {
      best = c;
    }
  }
  return best;
}

ChatSession session(String id, {DateTime? at, bool pinned = false}) =>
    ChatSession(
      id: id,
      title: id,
      pinned: pinned,
      messageCount: 1,
      lastMessageAt: at,
    );

void main() {
  test('resumes the most recent chat, not the pinned one', () {
    // Server order: pinned first, then activity.
    final sessions = [
      session('pinned-but-stale',
          pinned: true, at: DateTime(2026, 8, 1)),
      session('used-just-now', at: DateTime(2026, 8, 29)),
      session('older', at: DateTime(2026, 8, 20)),
    ];

    expect(mostRecent(sessions).id, 'used-just-now');
  });

  test('resumes the most recent, not the first created', () {
    // The bug: the original chat is oldest and sorts last by activity, but a
    // naive fallback took whatever came first.
    final sessions = [
      session('first-created', at: DateTime(2026, 1, 1)),
      session('newest', at: DateTime(2026, 8, 29)),
    ];

    expect(mostRecent(sessions).id, 'newest');
  });

  test('a chat nobody has spoken in does not win', () {
    final sessions = [
      session('brand-new', at: null),
      session('has-history', at: DateTime(2026, 8, 29)),
    ];

    expect(mostRecent(sessions).id, 'has-history');
  });

  test('falls back to the only chat when none have timestamps', () {
    final sessions = [session('only', at: null)];
    expect(mostRecent(sessions).id, 'only');
  });

  test('an empty-id session is the earlier chat, not an untitled one', () {
    // The delete path hands back a sentinel when nothing is left; it must name
    // the original chat so the screen does not show "Untitled chat".
    const fallback = ChatSession(
      id: ChatSession.defaultId,
      title: '',
      pinned: false,
      messageCount: 0,
    );
    expect(fallback.isDefault, isTrue);
    expect(fallback.displayTitle, 'Earlier chat');
  });
}
