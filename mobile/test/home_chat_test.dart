import 'package:agentfleet_companion/core/models.dart';
import 'package:agentfleet_companion/core/theme/theme.dart';
import 'package:agentfleet_companion/features/fleet_comms/message_tile.dart';
import 'package:agentfleet_companion/features/home/home_chat_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  const commands = [
    FleetCommand(name: 'bots', usage: '/bots', description: 'List every agent'),
    FleetCommand(
        name: 'status',
        usage: '/status',
        description: 'What the fleet is doing right now'),
    FleetCommand(
        name: 'mission',
        usage: '/mission <goal>',
        description: 'Start a mission and show its status as it runs',
        mutates: true),
    FleetCommand(
        name: 'stop', usage: '/stop <bot>', description: 'Halt one agent'),
  ];

  group('filterCommands', () {
    test('an empty prefix is every command', () {
      expect(filterCommands(commands, '/'), hasLength(4));
      expect(filterCommands(commands, ''), hasLength(4));
    });

    test('a name prefix ranks above a mention in the description', () {
      // "st" also occurs in "List every agent" and "Start a mission", so
      // those match too — but after the two whose names begin with it.
      final out = filterCommands(commands, '/st').map((c) => c.name).toList();
      expect(out.sublist(0, 2), ['status', 'stop']);
      expect(out.sublist(2), containsAll(['bots', 'mission']));

      final loose = filterCommands(commands, '/status');
      expect(loose.first.name, 'status');
      expect(loose.map((c) => c.name), contains('mission'));
    });

    test('matching ignores case and the leading slash', () {
      // /stop's usage mentions "<bot>", so it trails /bots rather than
      // being excluded.
      expect(filterCommands(commands, '/BO').first.name, 'bots');
      expect(filterCommands(commands, 'bo').map((c) => c.name),
          ['bots', 'stop']);
      expect(filterCommands(commands, '/miss').single.name, 'mission');
    });

    test('nothing matches nonsense', () {
      expect(filterCommands(commands, '/zzz'), isEmpty);
    });
  });

  PeerMessage message({
    required String kind,
    String fromId = 'bot-1',
    String fromName = 'Scout',
  }) =>
      PeerMessage(
        id: 'm-$kind',
        fromInstanceId: fromId,
        fromInstanceName: fromName,
        toInstanceId: 'broadcast',
        kind: kind,
        content: '**Mission** started',
        createdAt: DateTime.now(),
      );

  Widget host(Widget child) => MaterialApp(
        theme: buildTheme(),
        home: Scaffold(body: SingleChildScrollView(child: child)),
      );

  testWidgets('a system message renders as an Oaf card with the mascot',
      (tester) async {
    await tester.pumpWidget(host(FleetMessageTile(
      message: message(kind: 'system', fromId: 'system', fromName: 'Oaf'),
    )));
    await tester.pump();

    expect(find.byType(OafNoteCard), findsOneWidget);
    expect(find.byType(MessageTile), findsNothing);
    expect(find.text('Oaf'), findsOneWidget);
    expect(
      find.byWidgetPredicate((w) =>
          w is Image &&
          w.image is AssetImage &&
          (w.image as AssetImage).assetName == 'assets/branding/mascot.png'),
      findsOneWidget,
    );
    // The body is markdown, so the emphasis is rendered rather than shown raw.
    expect(find.textContaining('**'), findsNothing);
  });

  testWidgets('an ordinary agent message uses the shared tile',
      (tester) async {
    await tester.pumpWidget(host(FleetMessageTile(
      message: message(kind: 'message'),
    )));
    await tester.pump();

    expect(find.byType(MessageTile), findsOneWidget);
    expect(find.byType(OafNoteCard), findsNothing);
    expect(find.text('Scout'), findsOneWidget);
  });

  testWidgets('a failed command result is warn-toned, a good one is not',
      (tester) async {
    await tester.pumpWidget(host(Column(children: const [
      CommandResultCard(
        result: FleetCommandResult(
            command: '/status', ok: true, title: 'Fleet', body: 'All quiet.'),
      ),
      CommandResultCard(
        result: FleetCommandResult(
            command: '/approve',
            ok: false,
            title: 'Nothing to approve',
            body: 'No mission is waiting.'),
      ),
    ])));
    await tester.pump();

    expect(find.byIcon(Icons.terminal), findsOneWidget);
    expect(find.byIcon(Icons.warning_amber_rounded), findsOneWidget);
    expect(find.text('All quiet.'), findsOneWidget);
    expect(find.text('No mission is waiting.'), findsOneWidget);
  });
}
