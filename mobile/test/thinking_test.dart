import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';
import 'package:agentfleet_companion/core/widgets/thinking.dart';

Widget _host(Widget child) => MaterialApp(home: Scaffold(body: child));

void main() {
  testWidgets('a thinking bubble names the thinker and what it is doing',
      (tester) async {
    await tester.pumpWidget(_host(const ThinkingBubble(
      who: 'Oaf',
      hint: 'read the shell result, deciding what is next',
    )));
    await tester.pump();
    expect(find.textContaining('Oaf'), findsOneWidget);
    expect(find.textContaining('is thinking'), findsOneWidget);
    expect(find.textContaining('deciding what is next'), findsOneWidget);
    // Screen readers get one live announcement, not three dots.
    expect(
      find.bySemanticsLabel(RegExp(r'Oaf is thinking')),
      findsOneWidget,
    );
    expect(find.byType(ThinkingDots), findsOneWidget);
  });

  testWidgets('the dots keep animating while shown and stop cleanly when gone',
      (tester) async {
    await tester.pumpWidget(_host(const ThinkingDots()));
    await tester.pump(const Duration(milliseconds: 400));
    await tester.pump(const Duration(milliseconds: 400));
    expect(find.byType(ThinkingDots), findsOneWidget);
    await tester.pumpWidget(_host(const SizedBox()));
    await tester.pump();
    expect(find.byType(ThinkingDots), findsNothing);
  });

  testWidgets('a message rises in once and then holds still', (tester) async {
    await tester.pumpWidget(_host(const MessageEnter(
      key: ValueKey('m1'),
      child: Text('hello'),
    )));
    final opacity0 = tester.widget<Opacity>(find.byType(Opacity)).opacity;
    expect(opacity0, lessThan(0.5));
    await tester.pumpAndSettle();
    final opacity1 = tester.widget<Opacity>(find.byType(Opacity)).opacity;
    expect(opacity1, 1.0);
  });

  testWidgets('a working line shows the bot, its step and its goal',
      (tester) async {
    await tester.pumpWidget(_host(const WorkingLine(
      name: 'Builder',
      step: 7,
      maxSteps: 60,
      goal: 'Build Fleet Notes',
    )));
    await tester.pump();
    expect(find.text('Builder'), findsOneWidget);
    expect(find.text('is working'), findsOneWidget);
    expect(find.textContaining('step 7/60'), findsOneWidget);
    expect(find.textContaining('Build Fleet Notes'), findsOneWidget);
  });
}
