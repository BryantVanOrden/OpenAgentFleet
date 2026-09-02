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
}
