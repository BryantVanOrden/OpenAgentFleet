/// The deliberately small YAML subset archetype packages are written in — the
/// same one the console and the Python SDK emit and read, so a package written
/// by any client loads on the others. Nested structures are inline JSON, which
/// is valid YAML because YAML is a superset of JSON — and keeps this honest:
/// it never writes something it could not read back.
library;

import 'dart:convert';

String manifestToYaml(Map<String, dynamic> data) {
  final lines = <String>[];
  for (final entry in data.entries) {
    final key = entry.key;
    final value = entry.value;
    if (value == null) {
      lines.add('$key: null');
    } else if (value is List) {
      if (value.isEmpty) {
        lines.add('$key: []');
      } else if (value.every((v) => v is! List && v is! Map)) {
        lines.add('$key:');
        for (final v in value) {
          lines.add('  - ${_scalar(v)}');
        }
      } else {
        lines.add('$key: ${jsonEncode(value)}');
      }
    } else if (value is Map) {
      if (value.isEmpty) {
        lines.add('$key: {}');
      } else {
        lines.add('$key:');
        value.forEach((k, v) => lines.add('  $k: ${_scalar(v)}'));
      }
    } else {
      lines.add('$key: ${_scalar(value)}');
    }
  }
  return '${lines.join('\n')}\n';
}

String _scalar(dynamic v) {
  if (v is bool) return v ? 'true' : 'false';
  if (v is num) return '$v';
  if (v == null) return 'null';
  // Always quoted, via JSON: a system prompt runs to paragraphs and contains
  // colons, hashes and newlines, every one of which changes the meaning of an
  // unquoted YAML scalar.
  return jsonEncode('$v');
}

Map<String, dynamic> manifestFromYaml(String raw) {
  final out = <String, dynamic>{};
  String? key;
  List<dynamic>? seq;
  Map<String, dynamic>? map;

  void flush() {
    if (key == null) return;
    if (seq != null) {
      out[key!] = seq;
    } else if (map != null) {
      out[key!] = map;
    }
    key = null;
    seq = null;
    map = null;
  }

  for (final line in raw.split(RegExp(r'\r?\n'))) {
    if (line.trim().isEmpty || line.trimLeft().startsWith('#')) continue;

    final listMatch = RegExp(r'^\s*-\s+(.*)$').firstMatch(line);
    if (listMatch != null && key != null) {
      (seq ??= []).add(_unscalar(listMatch.group(1)!.trim()));
      continue;
    }

    if (RegExp(r'^\s{2,}\S').hasMatch(line) && key != null && seq == null) {
      final idx = line.indexOf(':');
      if (idx > 0) {
        (map ??= {})[line.substring(0, idx).trim()] =
            _unscalar(line.substring(idx + 1).trim());
        continue;
      }
    }

    flush();
    final idx = line.indexOf(':');
    if (idx < 0) continue;
    final k = line.substring(0, idx).trim();
    final v = line.substring(idx + 1).trim();
    if (v.isEmpty) {
      key = k;
      continue;
    }
    out[k] = _unscalar(v);
  }
  flush();
  return out;
}

dynamic _unscalar(String v) {
  if (v == '[]') return [];
  if (v == '{}') return {};
  if (v == 'true') return true;
  if (v == 'false') return false;
  if (v == 'null') return null;
  if (RegExp(r'^[\[{"]').hasMatch(v)) {
    try {
      return jsonDecode(v);
    } catch (_) {
      return v;
    }
  }
  final n = num.tryParse(v);
  return n ?? v;
}

/// A pasted package: JSON or the YAML above — this tool family has written
/// both, so refusing one would reject packages it produced itself.
Map<String, dynamic> parseManifest(String text) {
  final trimmed = text.trimLeft();
  if (trimmed.startsWith('{')) {
    return (jsonDecode(trimmed) as Map).cast<String, dynamic>();
  }
  return manifestFromYaml(text);
}
