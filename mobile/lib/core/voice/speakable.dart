/// Turns chat text into something a TTS voice can say without embarrassing
/// itself.
///
/// Agents answer in markdown, and a voice model reading "**Done** — see
/// `deploy.sh`, v1.0.1" verbatim says "asterisk asterisk done asterisk
/// asterisk" and then mangles the version. Two passes fix that: the first
/// removes markdown STRUCTURE while keeping the words (a code block becomes
/// the words "code block" — nobody wants forty lines of bash read aloud), the
/// second verbalises numbers, because small TTS models are far better at
/// "one point zero point one" than at "1.0.1".
library;

String speakable(String text) => _verbaliseNumbers(_stripMarkdown(text));

// ------------------------------------------------------------- markdown ---

String _stripMarkdown(String text) {
  var s = text.replaceAll('\r\n', '\n');

  // Fenced blocks first, before anything inside them can match other rules.
  s = s.replaceAll(RegExp(r'```[^\n]*\n[\s\S]*?```'), ' code block. ');
  s = s.replaceAll(RegExp(r'```[\s\S]*?```'), ' code block. ');

  // Images speak their alt text; links speak their label; bare URLs speak
  // their host — "h t t p s colon slash slash" helps no one.
  s = s.replaceAllMapped(RegExp(r'!\[([^\]]*)\]\([^)]*\)'), (m) => m[1] ?? '');
  s = s.replaceAllMapped(RegExp(r'\[([^\]]*)\]\([^)]*\)'), (m) => m[1] ?? '');
  s = s.replaceAllMapped(
    RegExp(r'https?://([^/\s)>\]]+)[^\s)>\]]*'),
    (m) => m[1] ?? '',
  );

  // Inline code keeps its content: "run `make up`" should say "run make up".
  s = s.replaceAllMapped(RegExp(r'`([^`]*)`'), (m) => m[1] ?? '');

  final lines = <String>[];
  for (var line in s.split('\n')) {
    final t = line.trimLeft();
    // Horizontal rules and table separator rows carry no words.
    if (RegExp(r'^(-{3,}|\*{3,}|_{3,})$').hasMatch(t.trim())) continue;
    if (RegExp(r'^\|?[\s:|-]+\|[\s:|-]*$').hasMatch(t.trim())) continue;
    line = line.replaceFirst(RegExp(r'^\s{0,3}#{1,6}\s+'), '');
    line = line.replaceFirst(RegExp(r'^\s{0,3}>\s?'), '');
    line = line.replaceFirst(RegExp(r'^\s*[-*+]\s+'), '');
    line = line.replaceFirst(RegExp(r'^\s*\d{1,3}[.)]\s+'), '');
    line = line.replaceAll('|', ' ');
    lines.add(line);
  }
  s = lines.join('\n');

  // Emphasis markers. Single underscores stay: stripping them would turn
  // snake_case identifiers into nonsense words.
  s = s.replaceAll(RegExp(r'\*{1,3}'), '');
  s = s.replaceAll('~~', '');
  s = s.replaceAll(RegExp(r'__'), '');

  // Anything shaped like an HTML tag.
  s = s.replaceAll(RegExp(r'</?[a-zA-Z][^>]*>'), ' ');

  return s.replaceAll(RegExp(r'[ \t]+'), ' ').replaceAll(RegExp(r'\n{3,}'), '\n\n').trim();
}

// -------------------------------------------------------------- numbers ---

String _verbaliseNumbers(String s) {
  // Money before anything eats the digits. "$5" → "five dollars",
  // "$5.20" → "five point two zero dollars".
  s = s.replaceAllMapped(
    RegExp(r'\$(\d[\d,]*)(\.\d+)?'),
    (m) {
      final whole = _intWords(m[1]!.replaceAll(',', ''));
      final frac = m[2];
      if (frac == null) return '$whole dollars';
      return '$whole point ${_digitWords(frac.substring(1))} dollars';
    },
  );

  // Percentages.
  s = s.replaceAllMapped(
    RegExp(r'(\d[\d,]*(?:\.\d+)?)\s?%'),
    (m) => '${_numberWords(m[1]!)} percent',
  );

  // Clock times: "3:30" → "three thirty", "3:00" → "three o'clock".
  s = s.replaceAllMapped(
    RegExp(r'\b(\d{1,2}):(\d{2})\b'),
    (m) {
      final h = _intWords(m[1]!);
      final mm = m[2]!;
      if (mm == '00') return "$h o'clock";
      if (mm.startsWith('0')) return '$h oh ${_intWords(mm.substring(1))}';
      return '$h ${_intWords(mm)}';
    },
  );

  // Dotted versions ("1.0.1", "2.10.3") read digit-group by digit-group.
  // Lookarounds, not \b: "v1.0.1" has no word boundary between the v and the
  // 1, and missing the match here let the decimal rule below shred it.
  s = s.replaceAllMapped(
    RegExp(r'(?<![\d.])(\d+(?:\.\d+){2,})(?![\d.])'),
    (m) => m[1]!.split('.').map(_intWords).join(' point '),
  );

  // Ranges between small numbers: "steps 3-5" → "steps three to five".
  s = s.replaceAllMapped(
    RegExp(r'\b(\d{1,4})-(\d{1,4})\b'),
    (m) => '${_intWords(m[1]!)} to ${_intWords(m[2]!)}',
  );

  // Decimals, then plain integers (comma thousands included).
  s = s.replaceAllMapped(
    RegExp(r'\b(\d[\d,]*)\.(\d+)\b'),
    (m) =>
        '${_intWords(m[1]!.replaceAll(',', ''))} point ${_digitWords(m[2]!)}',
  );
  s = s.replaceAllMapped(
    RegExp(r'(?<![\w.])(\d[\d,]*)(?![\w.%])'),
    (m) => _intWords(m[1]!.replaceAll(',', '')),
  );

  return s.replaceAll(RegExp(r'[ \t]+'), ' ').trim();
}

const _ones = [
  'zero', 'one', 'two', 'three', 'four', 'five', 'six', 'seven', 'eight',
  'nine', 'ten', 'eleven', 'twelve', 'thirteen', 'fourteen', 'fifteen',
  'sixteen', 'seventeen', 'eighteen', 'nineteen',
];
const _tens = [
  '', '', 'twenty', 'thirty', 'forty', 'fifty', 'sixty', 'seventy', 'eighty',
  'ninety',
];
const _scales = ['', ' thousand', ' million', ' billion', ' trillion'];

/// English words for a non-negative integer given as digits. Anything past
/// the trillions is read digit by digit — "nine hundred ninety-nine
/// quadrillion" helps nobody debug a task id.
String _intWords(String digits) {
  digits = digits.replaceFirst(RegExp(r'^0+(?=\d)'), '');
  if (digits.length > 15) return _digitWords(digits);
  var n = int.parse(digits);
  if (n == 0) return 'zero';

  final parts = <String>[];
  var scale = 0;
  while (n > 0) {
    final chunk = n % 1000;
    if (chunk > 0) parts.insert(0, '${_chunkWords(chunk)}${_scales[scale]}');
    n ~/= 1000;
    scale++;
  }
  return parts.join(' ');
}

String _chunkWords(int n) {
  final out = <String>[];
  if (n >= 100) {
    out.add('${_ones[n ~/ 100]} hundred');
    n %= 100;
  }
  if (n >= 20) {
    final t = _tens[n ~/ 10];
    n %= 10;
    out.add(n > 0 ? '$t-${_ones[n]}' : t);
  } else if (n > 0) {
    out.add(_ones[n]);
  }
  return out.join(' ');
}

String _digitWords(String digits) =>
    digits.split('').map((d) => _ones[int.parse(d)]).join(' ');

/// A number that may carry a decimal point, already comma-free or not.
String _numberWords(String raw) {
  raw = raw.replaceAll(',', '');
  final dot = raw.indexOf('.');
  if (dot < 0) return _intWords(raw);
  return '${_intWords(raw.substring(0, dot))} point ${_digitWords(raw.substring(dot + 1))}';
}
