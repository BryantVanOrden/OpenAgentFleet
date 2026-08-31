import 'package:flutter/material.dart';

/// Syntax colouring for the work editor.
///
/// Written out rather than pulled from a package. The alternative is a
/// highlighter with a grammar set behind it, which is a lot of dependency for
/// something that colours nine languages in a text field, and this app already
/// has to build for Android, Linux and Windows without surprises.
///
/// It is a tokeniser, not a parser. It knows comments, strings, numbers and a
/// keyword list per language, and it is wrong about the same things every
/// tokeniser is wrong about — a keyword inside an identifier is left alone, but
/// a `#` in a shell string still ends the string before it starts a comment.
/// For reading a colleague's file and fixing a line of it, that is enough.
enum CodeLanguage { html, css, javascript, json, dart, go, python, shell, sql, yaml, markdown, none }

/// What language a document is in, from its name first and its content second.
CodeLanguage languageFor(String name, {String mime = '', String content = ''}) {
  final n = name.toLowerCase();
  final ext = n.contains('.') ? n.split('.').last : '';
  switch (ext) {
    case 'html':
    case 'htm':
    case 'xml':
    case 'svg':
      return CodeLanguage.html;
    case 'css':
      return CodeLanguage.css;
    case 'js':
    case 'mjs':
    case 'ts':
    case 'jsx':
    case 'tsx':
      return CodeLanguage.javascript;
    case 'json':
      return CodeLanguage.json;
    case 'dart':
      return CodeLanguage.dart;
    case 'go':
      return CodeLanguage.go;
    case 'py':
      return CodeLanguage.python;
    case 'sh':
    case 'bash':
    case 'zsh':
      return CodeLanguage.shell;
    case 'sql':
      return CodeLanguage.sql;
    case 'yaml':
    case 'yml':
      return CodeLanguage.yaml;
    case 'md':
    case 'markdown':
      return CodeLanguage.markdown;
  }
  if (mime.contains('html')) return CodeLanguage.html;
  if (mime.contains('json')) return CodeLanguage.json;
  if (mime.contains('markdown')) return CodeLanguage.markdown;

  // No extension is the normal case here: agents publish "rollr", not
  // "rollr.html". Guess from the first thing that looks decisive.
  final head = content.trimLeft();
  final lower = head.toLowerCase();
  if (lower.startsWith('<!doctype html') || lower.startsWith('<html') || lower.startsWith('<?xml')) {
    return CodeLanguage.html;
  }
  if (head.startsWith('{') || head.startsWith('[')) return CodeLanguage.json;
  if (head.startsWith('#!')) return CodeLanguage.shell;
  if (head.startsWith('# ') || head.startsWith('## ')) return CodeLanguage.markdown;
  return CodeLanguage.none;
}

/// A readable name for the language picker in the editor's title.
String languageLabel(CodeLanguage l) => switch (l) {
      CodeLanguage.html => 'HTML',
      CodeLanguage.css => 'CSS',
      CodeLanguage.javascript => 'JavaScript',
      CodeLanguage.json => 'JSON',
      CodeLanguage.dart => 'Dart',
      CodeLanguage.go => 'Go',
      CodeLanguage.python => 'Python',
      CodeLanguage.shell => 'Shell',
      CodeLanguage.sql => 'SQL',
      CodeLanguage.yaml => 'YAML',
      CodeLanguage.markdown => 'Markdown',
      CodeLanguage.none => 'Plain text',
    };

/// Colours that work on both themes.
class CodeTheme {
  const CodeTheme({
    required this.plain,
    required this.comment,
    required this.string,
    required this.number,
    required this.keyword,
    required this.name,
    required this.punctuation,
  });

  final Color plain, comment, string, number, keyword, name, punctuation;

  factory CodeTheme.of(BuildContext context) {
    final dark = Theme.of(context).brightness == Brightness.dark;
    return dark
        ? const CodeTheme(
            plain: Color(0xFFE2E8F0),
            comment: Color(0xFF6B7A90),
            string: Color(0xFF6EE7B7),
            number: Color(0xFFFFBE4D),
            keyword: Color(0xFFC4B5FD),
            name: Color(0xFF7DD3FC),
            punctuation: Color(0xFF94A3B8),
          )
        : const CodeTheme(
            plain: Color(0xFF1E293B),
            comment: Color(0xFF64748B),
            string: Color(0xFF047857),
            number: Color(0xFFB45309),
            keyword: Color(0xFF6D28D9),
            name: Color(0xFF1D4ED8),
            punctuation: Color(0xFF475569),
          );
  }
}

const _keywords = <CodeLanguage, Set<String>>{
  CodeLanguage.javascript: {
    'const', 'let', 'var', 'function', 'return', 'if', 'else', 'for', 'while',
    'break', 'continue', 'new', 'class', 'extends', 'this', 'null', 'undefined',
    'true', 'false', 'async', 'await', 'try', 'catch', 'finally', 'throw',
    'typeof', 'instanceof', 'of', 'in', 'switch', 'case', 'default', 'do',
    'import', 'export', 'from', 'delete', 'void', 'yield', 'static', 'get', 'set',
  },
  CodeLanguage.dart: {
    'abstract', 'as', 'assert', 'async', 'await', 'break', 'case', 'catch',
    'class', 'const', 'continue', 'covariant', 'default', 'deferred', 'do',
    'dynamic', 'else', 'enum', 'export', 'extends', 'extension', 'external',
    'factory', 'false', 'final', 'finally', 'for', 'get', 'if', 'implements',
    'import', 'in', 'is', 'late', 'library', 'mixin', 'new', 'null', 'on',
    'operator', 'part', 'required', 'rethrow', 'return', 'sealed', 'set',
    'show', 'static', 'super', 'switch', 'sync', 'this', 'throw', 'true',
    'try', 'typedef', 'var', 'void', 'while', 'with', 'yield',
  },
  CodeLanguage.go: {
    'break', 'case', 'chan', 'const', 'continue', 'default', 'defer', 'else',
    'fallthrough', 'for', 'func', 'go', 'goto', 'if', 'import', 'interface',
    'map', 'package', 'range', 'return', 'select', 'struct', 'switch', 'type',
    'var', 'nil', 'true', 'false', 'error', 'string', 'int', 'bool', 'byte',
    'rune', 'make', 'new', 'len', 'cap', 'append', 'copy', 'delete', 'panic',
    'recover',
  },
  CodeLanguage.python: {
    'and', 'as', 'assert', 'async', 'await', 'break', 'class', 'continue',
    'def', 'del', 'elif', 'else', 'except', 'False', 'finally', 'for', 'from',
    'global', 'if', 'import', 'in', 'is', 'lambda', 'None', 'nonlocal', 'not',
    'or', 'pass', 'raise', 'return', 'True', 'try', 'while', 'with', 'yield',
    'self', 'print', 'len', 'range',
  },
  CodeLanguage.shell: {
    'if', 'then', 'else', 'elif', 'fi', 'for', 'while', 'do', 'done', 'case',
    'esac', 'function', 'return', 'export', 'local', 'readonly', 'set', 'unset',
    'echo', 'cd', 'exit', 'source', 'in',
  },
  CodeLanguage.sql: {
    'select', 'from', 'where', 'insert', 'into', 'values', 'update', 'set',
    'delete', 'create', 'table', 'drop', 'alter', 'add', 'column', 'index',
    'join', 'left', 'right', 'inner', 'outer', 'on', 'group', 'by', 'order',
    'having', 'limit', 'offset', 'and', 'or', 'not', 'null', 'as', 'distinct',
    'union', 'all', 'primary', 'key', 'foreign', 'references', 'default',
  },
  CodeLanguage.css: {
    'important', 'media', 'keyframes', 'import', 'supports', 'font-face', 'root',
  },
  CodeLanguage.json: {'true', 'false', 'null'},
  CodeLanguage.yaml: {'true', 'false', 'null', 'yes', 'no'},
};

bool _isWordChar(int c) =>
    (c >= 0x61 && c <= 0x7A) || // a-z
    (c >= 0x41 && c <= 0x5A) || // A-Z
    (c >= 0x30 && c <= 0x39) || // 0-9
    c == 0x5F || // _
    c == 0x24 || // $
    c == 0x2D; // - (css property names, yaml keys)

bool _isDigit(int c) => c >= 0x30 && c <= 0x39;

/// Splits [source] into coloured runs.
///
/// One pass, left to right. Every branch consumes at least one character, so
/// this cannot spin on malformed input — which matters, because it runs on
/// every keystroke in the editor.
List<TextSpan> highlight(String source, CodeLanguage language, CodeTheme theme) {
  if (language == CodeLanguage.none || source.isEmpty) {
    return [TextSpan(text: source, style: TextStyle(color: theme.plain))];
  }
  if (language == CodeLanguage.markdown) return _markdown(source, theme);
  if (language == CodeLanguage.html) return _markup(source, theme);

  final words = _keywords[language] ?? const <String>{};
  final lineComment = switch (language) {
    CodeLanguage.python || CodeLanguage.shell || CodeLanguage.yaml => '#',
    CodeLanguage.sql => '--',
    CodeLanguage.css || CodeLanguage.json => '',
    _ => '//',
  };
  final blockComments = language != CodeLanguage.python &&
      language != CodeLanguage.shell &&
      language != CodeLanguage.yaml;

  final out = <TextSpan>[];
  final buf = StringBuffer();
  void flush() {
    if (buf.isEmpty) return;
    out.add(TextSpan(text: buf.toString(), style: TextStyle(color: theme.plain)));
    buf.clear();
  }

  var i = 0;
  while (i < source.length) {
    final rest = source.length - i;

    if (lineComment.isNotEmpty && rest >= lineComment.length &&
        source.startsWith(lineComment, i)) {
      flush();
      var end = source.indexOf('\n', i);
      if (end < 0) end = source.length;
      out.add(TextSpan(
          text: source.substring(i, end), style: TextStyle(color: theme.comment)));
      i = end;
      continue;
    }
    if (blockComments && rest >= 2 && source.startsWith('/*', i)) {
      flush();
      var end = source.indexOf('*/', i + 2);
      end = end < 0 ? source.length : end + 2;
      out.add(TextSpan(
          text: source.substring(i, end), style: TextStyle(color: theme.comment)));
      i = end;
      continue;
    }

    final ch = source[i];
    if (ch == '"' || ch == "'" || ch == '`') {
      flush();
      final end = _endOfString(source, i, ch);
      out.add(TextSpan(
          text: source.substring(i, end), style: TextStyle(color: theme.string)));
      i = end;
      continue;
    }

    final code = source.codeUnitAt(i);
    if (_isDigit(code)) {
      flush();
      var j = i;
      while (j < source.length &&
          (_isDigit(source.codeUnitAt(j)) ||
              source[j] == '.' ||
              source[j] == 'x' ||
              (source[j].toLowerCase().codeUnitAt(0) >= 0x61 &&
                  source[j].toLowerCase().codeUnitAt(0) <= 0x66))) {
        j++;
      }
      out.add(TextSpan(
          text: source.substring(i, j), style: TextStyle(color: theme.number)));
      i = j;
      continue;
    }

    if (_isWordChar(code) && !_isDigit(code)) {
      var j = i;
      while (j < source.length && _isWordChar(source.codeUnitAt(j))) {
        j++;
      }
      final word = source.substring(i, j);
      if (words.contains(word)) {
        flush();
        out.add(TextSpan(text: word, style: TextStyle(color: theme.keyword)));
      } else if (j < source.length && source[j] == '(') {
        flush();
        out.add(TextSpan(text: word, style: TextStyle(color: theme.name)));
      } else {
        buf.write(word);
      }
      i = j;
      continue;
    }

    buf.write(ch);
    i++;
  }
  flush();
  return out;
}

/// Where a quoted run ends, respecting backslash escapes.
int _endOfString(String s, int start, String quote) {
  var i = start + 1;
  while (i < s.length) {
    if (s[i] == r'\' && i + 1 < s.length) {
      i += 2;
      continue;
    }
    if (s[i] == quote) return i + 1;
    // An unterminated string should not swallow the rest of the file while
    // somebody is still typing it.
    if (s[i] == '\n' && quote != '`') return i;
    i++;
  }
  return s.length;
}

/// HTML and XML: tags, attribute names, attribute values, comments.
List<TextSpan> _markup(String source, CodeTheme theme) {
  final out = <TextSpan>[];
  var i = 0;
  while (i < source.length) {
    if (source.startsWith('<!--', i)) {
      var end = source.indexOf('-->', i + 4);
      end = end < 0 ? source.length : end + 3;
      out.add(TextSpan(
          text: source.substring(i, end), style: TextStyle(color: theme.comment)));
      i = end;
      continue;
    }
    if (source[i] == '<') {
      var end = source.indexOf('>', i);
      end = end < 0 ? source.length : end + 1;
      out.addAll(_tag(source.substring(i, end), theme));
      i = end;
      continue;
    }
    var end = source.indexOf('<', i);
    if (end < 0) end = source.length;
    out.add(TextSpan(
        text: source.substring(i, end), style: TextStyle(color: theme.plain)));
    i = end;
  }
  return out;
}

List<TextSpan> _tag(String tag, CodeTheme theme) {
  final out = <TextSpan>[];
  var i = 0;
  // The opening punctuation and the element name.
  while (i < tag.length && (tag[i] == '<' || tag[i] == '/' || tag[i] == '!')) {
    i++;
  }
  out.add(TextSpan(
      text: tag.substring(0, i), style: TextStyle(color: theme.punctuation)));
  var j = i;
  while (j < tag.length && _isWordChar(tag.codeUnitAt(j))) {
    j++;
  }
  if (j > i) {
    out.add(TextSpan(
        text: tag.substring(i, j), style: TextStyle(color: theme.keyword)));
  }
  i = j;

  while (i < tag.length) {
    final ch = tag[i];
    if (ch == '"' || ch == "'") {
      final end = _endOfString(tag, i, ch);
      out.add(TextSpan(
          text: tag.substring(i, end), style: TextStyle(color: theme.string)));
      i = end;
      continue;
    }
    if (_isWordChar(tag.codeUnitAt(i))) {
      var k = i;
      while (k < tag.length && _isWordChar(tag.codeUnitAt(k))) {
        k++;
      }
      out.add(TextSpan(
          text: tag.substring(i, k), style: TextStyle(color: theme.name)));
      i = k;
      continue;
    }
    out.add(TextSpan(text: ch, style: TextStyle(color: theme.punctuation)));
    i++;
  }
  return out;
}

/// Markdown: headings, fenced code, list markers.
///
/// Rebuilt line by line rather than by splitting and re-joining. `split` on a
/// string that ends in a newline yields a trailing empty piece, so appending a
/// newline to every piece added one that was never in the source -- which a
/// round-trip test caught and reading the code did not.
List<TextSpan> _markdown(String source, CodeTheme theme) {
  final out = <TextSpan>[];
  final lines = source.split('\n');
  var inFence = false;

  for (var i = 0; i < lines.length; i++) {
    final line = lines[i];
    final text = i == lines.length - 1 ? line : '$line\n';
    if (text.isEmpty) continue;

    final trimmed = line.trimLeft();
    if (trimmed.startsWith('```')) {
      inFence = !inFence;
      out.add(TextSpan(text: text, style: TextStyle(color: theme.comment)));
      continue;
    }
    if (inFence) {
      out.add(TextSpan(text: text, style: TextStyle(color: theme.string)));
      continue;
    }
    if (line.startsWith('#')) {
      out.add(TextSpan(
          text: text,
          style: TextStyle(color: theme.keyword, fontWeight: FontWeight.w700)));
      continue;
    }
    if (trimmed.startsWith('- ') ||
        trimmed.startsWith('* ') ||
        trimmed.startsWith('> ')) {
      out.add(TextSpan(text: text, style: TextStyle(color: theme.name)));
      continue;
    }
    out.add(TextSpan(text: text, style: TextStyle(color: theme.plain)));
  }
  if (out.isEmpty) {
    out.add(TextSpan(text: source, style: TextStyle(color: theme.plain)));
  }
  return out;
}

/// A controller that colours what is typed into it.
///
/// The alternative — a highlighted view stacked behind a transparent field —
/// drifts as soon as the two disagree about wrapping. Overriding
/// [buildTextSpan] means there is only ever one piece of text.
class CodeEditingController extends TextEditingController {
  CodeEditingController({required String text, required this.language})
      : super(text: text);

  CodeLanguage language;

  @override
  TextSpan buildTextSpan({
    required BuildContext context,
    TextStyle? style,
    required bool withComposing,
  }) {
    final spans = highlight(text, language, CodeTheme.of(context));
    return TextSpan(style: style, children: spans);
  }
}
