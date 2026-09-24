/// Checks for the add-agent form, as plain Dart. They mirror what the server
/// validates (`validateConnection` in org_api.go) so a mistake is caught
/// while the field is still in front of you, not after a round trip.
library;

import '../../core/models.dart';

/// Whether [path] is one of [roots] or inside one. Case-insensitive and
/// either slash, because the roots were reported by a PC that may well be
/// Windows.
bool underAnyRoot(String path, List<String> roots) {
  String norm(String p) {
    var s = p.trim().replaceAll('\\', '/');
    while (s.length > 1 && s.endsWith('/')) {
      s = s.substring(0, s.length - 1);
    }
    return s.toLowerCase();
  }

  final p = norm(path);
  if (p.isEmpty) return false;
  for (final r in roots) {
    final root = norm(r);
    if (root.isEmpty) continue;
    if (p == root || p.startsWith(root.endsWith('/') ? root : '$root/')) {
      return true;
    }
  }
  return false;
}

/// A folder under [root]: the root itself when [sub] is blank, otherwise the
/// two joined with the root's own separator. A [sub] that tries to climb out
/// with `..` is refused by returning null.
String? folderUnder(String root, String sub) {
  final s = sub.trim().replaceAll(RegExp(r'^[\\/]+'), '');
  if (s.isEmpty) return root;
  if (s.split(RegExp(r'[\\/]')).contains('..')) return null;
  final sep = root.contains('\\') && !root.contains('/') ? '\\' : '/';
  final base = root.endsWith('/') || root.endsWith('\\')
      ? root.substring(0, root.length - 1)
      : root;
  return '$base$sep${sep == '\\' ? s.replaceAll('/', '\\') : s.replaceAll('\\', '/')}';
}

/// What is wrong with a gateway or webhook address, or null when it will do.
String? checkAgentUrl(String kind, String url) {
  final u = Uri.tryParse(url.trim());
  if (url.trim().isEmpty || u == null || u.host.isEmpty) {
    return kind == AgentKind.openClaw
        ? 'The gateway address, e.g. wss://gateway.example:18789'
        : 'The address to POST to, e.g. https://example.com/agent';
  }
  if (kind == AgentKind.openClaw && u.scheme != 'ws' && u.scheme != 'wss') {
    return 'An OpenClaw gateway address starts with ws:// or wss://';
  }
  if (kind == AgentKind.webhook && u.scheme != 'http' && u.scheme != 'https') {
    return 'A webhook address starts with http:// or https://';
  }
  return null;
}

/// PCs that could run an agent of [kind]: phones cannot run a CLI, and a PC
/// that listed what it has installed must list this.
List<OafDevice> devicesFor(String kind, List<OafDevice> devices) => devices
    .where((d) => d.kind != 'phone' && d.canRun(kind))
    .toList();

/// The commands that attach a PC, for the empty state's copy button.
const fleetctlHostCommands =
    'pip install open-agent-fleet\nfleetctl host --root <folder>';
