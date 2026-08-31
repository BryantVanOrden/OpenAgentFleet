import 'package:flutter/material.dart';
import 'package:flutter_test/flutter_test.dart';

import 'package:agentfleet_companion/features/vault/code_highlight.dart';

void main() {
  group('languageFor', () {
    test('reads the extension when there is one', () {
      expect(languageFor('rollr.html'), CodeLanguage.html);
      expect(languageFor('main.go'), CodeLanguage.go);
      expect(languageFor('notes.md'), CodeLanguage.markdown);
      expect(languageFor('a.b.json'), CodeLanguage.json);
    });

    // Agents publish "rollr", not "rollr.html", so the content has to decide.
    test('falls back to the content when the name says nothing', () {
      expect(languageFor('rollr', content: '<!DOCTYPE html>\n<html>'),
          CodeLanguage.html);
      expect(languageFor('config', content: '{"a": 1}'), CodeLanguage.json);
      expect(languageFor('deploy', content: '#!/bin/bash\n'), CodeLanguage.shell);
      expect(languageFor('report', content: '# Findings\n'), CodeLanguage.markdown);
      expect(languageFor('scratch', content: 'just words'), CodeLanguage.none);
    });

    test('uses the mime type when it has one', () {
      expect(languageFor('thing', mime: 'text/html'), CodeLanguage.html);
    });
  });

  group('highlight', () {
    const theme = CodeTheme(
      plain: Color(0xFF000001),
      comment: Color(0xFF000002),
      string: Color(0xFF000003),
      number: Color(0xFF000004),
      keyword: Color(0xFF000005),
      name: Color(0xFF000006),
      punctuation: Color(0xFF000007),
    );

    String rendered(List<dynamic> spans) =>
        spans.map((s) => s.text as String? ?? '').join();

    test('keeps every character of the source', () {
      const src = '''
// a comment
const x = "hello";
function go(n) { return n * 2; }
''';
      final spans = highlight(src, CodeLanguage.javascript, theme);
      expect(rendered(spans), src);
    });

    test('an unterminated string does not swallow the file', () {
      const src = 'const a = "oops\nconst b = 2;\n';
      final spans = highlight(src, CodeLanguage.javascript, theme);
      expect(rendered(spans), src);
      // The second line must still be tokenised, not treated as string.
      expect(spans.length, greaterThan(3));
    });

    test('markup keeps every character too', () {
      const src = '<!-- hi -->\n<div class="a">text</div>\n';
      final spans = highlight(src, CodeLanguage.html, theme);
      expect(rendered(spans), src);
    });

    test('markdown round-trips with and without a trailing newline', () {
      const withNl = '# Title\n\n- one\n';
      const withoutNl = '# Title\n\n- one';
      expect(rendered(highlight(withNl, CodeLanguage.markdown, theme)), withNl);
      expect(
          rendered(highlight(withoutNl, CodeLanguage.markdown, theme)), withoutNl);
    });

    test('plain text is left alone', () {
      const src = 'nothing to colour here';
      final spans = highlight(src, CodeLanguage.none, theme);
      expect(rendered(spans), src);
      expect(spans, hasLength(1));
    });

    test('empty input does not throw', () {
      expect(highlight('', CodeLanguage.go, theme), hasLength(1));
    });
  });
}
