import 'package:agentfleet_companion/core/models.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  // The backend now returns [] rather than null, but the client should not
  // depend on that: an older orchestrator, or any endpoint that regresses to a
  // nil slice, previously took the whole screen down with
  // "type 'Null' is not a subtype of type 'List<dynamic>'".
  group('models tolerate absent lists', () {
    test('a team with no members, messages or artifacts', () {
      final t = SwarmTeam.fromJson({'id': 's1', 'name': 'empty team'});
      expect(t.members, isEmpty);
      expect(t.messages, isEmpty);
      expect(t.artifacts, isEmpty);
    });

    test('a skill with no steps counts zero rather than throwing', () {
      final s = Skill.fromJson({'id': 'sk1', 'name': 'empty skill'});
      expect(s.stepCount, 0);
    });

    test('an explicit null list is treated the same as an absent one', () {
      final t = SwarmTeam.fromJson({
        'id': 's2',
        'name': 'nulls',
        'members': null,
        'messages': null,
        'artifacts': null,
      });
      expect(t.members, isEmpty);
      expect(t.messages, isEmpty);
      expect(t.artifacts, isEmpty);
    });
  });

  // Mirrors what api_client now does with a null body: `as List? ?? const []`.
  // A bare `as List` here is what threw before.
  test('a null list payload degrades to empty instead of throwing', () {
    const Object? payload = null;
    expect(payload as List? ?? const [], isEmpty);
    expect(() => payload as List, throwsA(isA<TypeError>()));
  });
}
