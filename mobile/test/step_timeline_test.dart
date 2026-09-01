import 'package:agentfleet_companion/core/models.dart';
import 'package:agentfleet_companion/core/theme/theme.dart';
import 'package:agentfleet_companion/features/instance_view/task_detail_screen.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

StepRecord _step({
  int step = 3,
  String action = 'click',
  String thought = '',
  String target = '',
  String text = '',
  String key = '',
  List<num> coordinates = const [],
  int mark = 0,
  String outcome = 'ok',
  int durationMs = 1200,
}) =>
    StepRecord(
      id: 's$step',
      taskId: 't1',
      step: step,
      action: AgentAction(
        action: action,
        thought: thought,
        target: target,
        text: text,
        key: key,
        coordinates: coordinates,
        mark: mark,
      ),
      outcome: outcome,
      durationMs: durationMs,
      promptTokens: 100,
      outputTokens: 20,
    );

Widget _wrap(Widget child) => MaterialApp(
      theme: buildTheme(),
      home: Scaffold(body: child),
    );

void main() {
  group('StepRecord', () {
    test('parses the wire shape the console reads', () {
      final s = StepRecord.fromJson({
        'id': 'st1',
        'task_id': 't1',
        'step': 4,
        'action': {
          'action': 'type',
          'thought': 'The field is focused, so type the query.',
          'text': 'quarterly report',
          'mark': 7,
        },
        'observation_key': 'obs/abc.webp',
        'outcome': 'typed 16 characters',
        'duration_ms': 2500,
        'prompt_tokens': 900,
        'output_tokens': 40,
      });
      expect(s.step, 4);
      expect(s.action.action, 'type');
      expect(s.action.mark, 7);
      expect(s.action.detail, 'quarterly report');
      expect(s.observationKey, 'obs/abc.webp');
      expect(s.failed, isFalse);
    });

    // The console tints a step red on /fail|error|refused|timed out/i; the
    // app must agree on which steps look alarming.
    test('failure heuristic matches the console regex', () {
      expect(_step(outcome: 'click FAILED: no such element').failed, isTrue);
      expect(_step(outcome: 'request timed out').failed, isTrue);
      expect(_step(outcome: 'the model refused').failed, isTrue);
      expect(_step(outcome: 'clicked "Submit"').failed, isFalse);
    });

    // Detail precedence is target, then text, then key, then coordinates —
    // the console's ?? chain.
    test('action detail follows the console precedence', () {
      expect(
          const AgentAction(target: 'Submit', text: 'x', key: 'Enter').detail,
          'Submit');
      expect(const AgentAction(key: 'Enter').detail, 'Enter');
      expect(const AgentAction(coordinates: [10, 20]).detail, '10,20');
      expect(const AgentAction().detail, '');
    });
  });

  group('StepTimelineTile', () {
    testWidgets('renders step number, action, mark, thought and duration',
        (tester) async {
      await tester.pumpWidget(_wrap(StepTimelineTile(
        step: _step(
          step: 3,
          action: 'click',
          thought: 'The save button is visible.',
          target: 'Save',
          mark: 5,
          durationMs: 1234,
        ),
      )));

      expect(find.text('#3'), findsOneWidget);
      expect(find.text('click'), findsOneWidget);
      expect(find.text('Mark [5]'), findsOneWidget);
      expect(find.text('Save'), findsOneWidget);
      expect(find.text('“The save button is visible.”'), findsOneWidget);
      expect(find.text('1.2s'), findsOneWidget);
    });

    testWidgets('a step without a frame says so instead of a broken image',
        (tester) async {
      await tester.pumpWidget(_wrap(StepTimelineTile(step: _step())));
      expect(find.text('no frame'), findsOneWidget);
      expect(find.byType(Image), findsNothing);
    });

    testWidgets('a failed outcome is tinted red, a normal one is not',
        (tester) async {
      await tester.pumpWidget(_wrap(Column(children: [
        StepTimelineTile(step: _step(step: 1, outcome: 'click failed: gone')),
        StepTimelineTile(step: _step(step: 2, outcome: 'clicked "Save"')),
      ])));

      final failed =
          tester.widget<Text>(find.text('click failed: gone'));
      final ok = tester.widget<Text>(find.text('clicked "Save"'));
      expect(failed.style?.color, Fleet.bad);
      expect(ok.style?.color, Fleet.ink400);
    });
  });
}
