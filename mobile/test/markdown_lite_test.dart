import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:agentfleet_companion/core/markdown/markdown_lite.dart';

Widget _host(String md) => MaterialApp(
      home: Scaffold(body: SingleChildScrollView(child: MarkdownLite(md))),
    );

// The renderer's contract: markdown structure becomes widgets, never leaked
// syntax, and anything unmodelled degrades to its words.
void main() {
  testWidgets('bold and italic render without their markers', (tester) async {
    await tester.pumpWidget(_host('**Done** and *noted*'));
    final rich = tester.widget<Text>(find.byType(Text).first);
    final span = rich.textSpan!;
    final flat = span.toPlainText();
    expect(flat, 'Done and noted');
    var sawBold = false;
    span.visitChildren((s) {
      if (s is TextSpan && s.text == 'Done') {
        sawBold = s.style?.fontWeight == FontWeight.w700;
      }
      return true;
    });
    expect(sawBold, isTrue, reason: 'the bold run should carry w700');
  });

  testWidgets('a fenced block renders in its own container', (tester) async {
    await tester.pumpWidget(_host('before\n```sh\nmake up\n```'));
    expect(find.text('make up'), findsOneWidget);
    expect(find.textContaining('```'), findsNothing);
  });

  testWidgets('lists get bullets and numbers', (tester) async {
    await tester.pumpWidget(_host('- alpha\n- beta\n\n1. one\n2. two'));
    expect(find.text('•'), findsNWidgets(2));
    expect(find.text('1.'), findsOneWidget);
    expect(find.text('2.'), findsOneWidget);
  });

  testWidgets('headings and quotes lose their furniture', (tester) async {
    await tester.pumpWidget(_host('## Result\n> careful now'));
    expect(find.textContaining('#'), findsNothing);
    final all = tester
        .widgetList<Text>(find.byType(Text))
        .map((t) => t.textSpan?.toPlainText() ?? t.data ?? '')
        .join('\n');
    expect(all, contains('Result'));
    expect(all, contains('careful now'));
    expect(all, isNot(contains('>')));
  });

  testWidgets('plain text with no markdown still renders', (tester) async {
    await tester.pumpWidget(_host('just words, no syntax'));
    expect(find.textContaining('just words'), findsOneWidget);
  });

  testWidgets('a list item with bold and code is one line of text, not three',
      (tester) async {
    await tester.pumpWidget(_host(
        '1. **Pawsit** — playful blend\n2. I will create `notes/a.md` today'));
    final texts = tester
        .widgetList<Text>(find.byType(Text))
        .map((t) => t.textSpan?.toPlainText() ?? t.data ?? '')
        .where((s) => s.trim().isNotEmpty && s != '1.' && s != '2.')
        .toList();
    expect(texts, hasLength(2));
    expect(texts[0], 'Pawsit — playful blend');
    expect(texts[1], contains('notes/a.md'));
    expect(texts[1], endsWith('today'));
  });

  testWidgets('a nested list still nests under its item', (tester) async {
    await tester.pumpWidget(_host('- **Builder**\n  - Claude\n- Checker'));
    final all = tester
        .widgetList<Text>(find.byType(Text))
        .map((t) => t.textSpan?.toPlainText() ?? t.data ?? '')
        .toList();
    expect(all, containsAll(['Builder', 'Claude', 'Checker']));
    expect(find.text('•'), findsNWidgets(3));
  });
}
