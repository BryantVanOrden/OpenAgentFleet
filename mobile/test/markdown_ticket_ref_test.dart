import 'package:agentfleet_companion/core/markdown/markdown_lite.dart';
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

/// Every TextSpan under [tester]'s RichText widgets, flattened.
List<TextSpan> spans(WidgetTester tester) {
  final out = <TextSpan>[];
  for (final rt in tester.widgetList<RichText>(find.byType(RichText))) {
    rt.text.visitChildren((s) {
      if (s is TextSpan) out.add(s);
      return true;
    });
  }
  return out;
}

void main() {
  tearDown(() => MarkdownLite.onTicketRef = null);

  Future<void> pump(WidgetTester tester, String text) => tester.pumpWidget(
        MaterialApp(home: Scaffold(body: MarkdownLite(text))),
      );

  testWidgets('T-12 becomes a tappable reference when a handler is set',
      (tester) async {
    final opened = <String>[];
    MarkdownLite.onTicketRef = (_, ref) => opened.add(ref);
    await pump(tester, 'Finished **T-12**, see T-3 too.');

    final refs = spans(tester).where((s) => s.recognizer != null).toList();
    expect(refs.map((s) => s.text), ['T-12', 'T-3']);
    for (final s in refs) {
      (s.recognizer! as TapGestureRecognizer).onTap!();
    }
    expect(opened, ['T-12', 'T-3']);
  });

  testWidgets('without a handler the text is unchanged and not tappable',
      (tester) async {
    await pump(tester, 'Working on T-12 now');
    expect(find.textContaining('Working on T-12 now', findRichText: true),
        findsOneWidget);
    expect(spans(tester).where((s) => s.recognizer != null), isEmpty);
  });

  testWidgets('code stays code, and lookalikes are not references',
      (tester) async {
    MarkdownLite.onTicketRef = (_, __) {};
    await pump(tester, 'Run `git log T-12` on AT-5 or T-x');
    expect(spans(tester).where((s) => s.recognizer != null), isEmpty);
  });
}
