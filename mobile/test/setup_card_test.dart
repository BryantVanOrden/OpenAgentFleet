import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:agentfleet_companion/core/models.dart';
import 'package:agentfleet_companion/core/theme/theme.dart';
import 'package:agentfleet_companion/features/home/setup_card.dart';

SetupStatus _status(String next) {
  final order = ['model', 'bot', 'goal'];
  final upTo = order.indexOf(next);
  return SetupStatus(
    providers: upTo > 0 ? 1 : 0,
    bots: upTo > 1 ? 1 : 0,
    runningBots: upTo > 1 ? 1 : 0,
    tasks: 0,
    next: next,
    steps: [
      SetupStep(id: 'model', title: 'Connect a model', done: upTo > 0, hint: 'Oaf can find it.', action: '/setup'),
      SetupStep(id: 'bot', title: 'Create your first bot', done: upTo > 1, action: '/new fullstack_dev'),
      const SetupStep(id: 'goal', title: 'Give it something to do', done: false),
    ],
  );
}

Widget _wrap(Widget child) => MaterialApp(
      theme: buildTheme(Brightness.dark, FleetAccent.amber),
      home: Scaffold(body: SingleChildScrollView(child: child)),
    );

void main() {
  testWidgets('the model step runs /setup from its one button', (tester) async {
    final ran = <String>[];
    await tester.pumpWidget(_wrap(SetupCard(
      status: _status('model'),
      busy: false,
      readOnly: false,
      onRun: ran.add,
      onFocusComposer: () {},
      leading: const SizedBox(width: 44, height: 44),
    )));
    expect(find.text('STEP 1 OF 3'), findsOneWidget);
    expect(find.text('Connect a model'), findsWidgets);
    await tester.tap(find.widgetWithText(FilledButton, 'Find my model'));
    expect(ran, ['/setup']);
  });

  testWidgets('an address typed by hand becomes /setup <url>', (tester) async {
    final ran = <String>[];
    await tester.pumpWidget(_wrap(SetupCard(
      status: _status('model'),
      busy: false,
      readOnly: false,
      onRun: ran.add,
      onFocusComposer: () {},
      leading: const SizedBox(width: 44, height: 44),
    )));
    await tester.tap(find.text('I have an address'));
    await tester.pumpAndSettle();
    await tester.enterText(find.byType(TextField), 'http://10.0.0.5:11434');
    await tester.tap(find.widgetWithText(FilledButton, 'Connect'));
    expect(ran, ['/setup http://10.0.0.5:11434']);
  });

  testWidgets('the bot step provisions a bot; the goal step focuses the composer', (tester) async {
    final ran = <String>[];
    var focused = 0;
    await tester.pumpWidget(_wrap(SetupCard(
      status: _status('bot'),
      busy: false,
      readOnly: false,
      onRun: ran.add,
      onFocusComposer: () => focused++,
      leading: const SizedBox(width: 44, height: 44),
    )));
    expect(find.text('STEP 2 OF 3'), findsOneWidget);
    await tester.tap(find.widgetWithText(FilledButton, 'Create a bot'));
    expect(ran.single, startsWith('/new fullstack_dev'));

    await tester.pumpWidget(_wrap(SetupCard(
      status: _status('goal'),
      busy: false,
      readOnly: false,
      onRun: ran.add,
      onFocusComposer: () => focused++,
      leading: const SizedBox(width: 44, height: 44),
    )));
    await tester.tap(find.widgetWithText(FilledButton, 'Tell it what to do'));
    expect(focused, 1);
  });

  testWidgets('read-only viewers see the plan but cannot act on it', (tester) async {
    await tester.pumpWidget(_wrap(SetupCard(
      status: _status('model'),
      busy: false,
      readOnly: true,
      onRun: (_) => fail('a viewer must not run commands'),
      onFocusComposer: () {},
      leading: const SizedBox(width: 44, height: 44),
    )));
    final button = tester.widget<FilledButton>(find.widgetWithText(FilledButton, 'Find my model'));
    expect(button.onPressed, isNull);
  });
}
