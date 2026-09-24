/// How the Work screen sorts and groups tickets, as plain Dart.
library;

import '../../core/models.dart';

/// One tab on the Work screen.
class WorkTab {
  const WorkTab(this.key, this.label, this.statuses);

  final String key;
  final String label;

  /// The ticket statuses that land in this tab.
  final List<String> statuses;
}

/// In the order work moves, with the backlog last: it is where tickets wait
/// for something that has not been arranged yet, which is rarely what you
/// open the screen to see.
const workTabs = [
  WorkTab('todo', 'To do', ['todo']),
  WorkTab('in_progress', 'In progress', ['in_progress']),
  WorkTab('in_review', 'In review', ['in_review']),
  WorkTab('blocked', 'Blocked', ['blocked']),
  WorkTab('done', 'Done', ['done', 'cancelled']),
  WorkTab('backlog', 'Backlog', ['backlog']),
];

/// The tab a status belongs in. A status this app does not know yet goes to
/// the backlog rather than vanishing.
String workTabFor(String status) {
  for (final t in workTabs) {
    if (t.statuses.contains(status)) return t.key;
  }
  return 'backlog';
}

/// Open work: highest priority first, then most recently touched. Finished
/// work: most recently finished first.
int compareTickets(Ticket a, Ticket b) {
  if (a.isTerminal && b.isTerminal) {
    final c = _newestFirst(a.doneAt ?? a.updatedAt, b.doneAt ?? b.updatedAt);
    if (c != 0) return c;
    return b.number.compareTo(a.number);
  }
  if (a.priority != b.priority) return b.priority.compareTo(a.priority);
  final c = _newestFirst(a.updatedAt, b.updatedAt);
  if (c != 0) return c;
  return b.number.compareTo(a.number);
}

int _newestFirst(DateTime? a, DateTime? b) {
  if (a == null && b == null) return 0;
  if (a == null) return 1;
  if (b == null) return -1;
  return b.compareTo(a);
}

/// Tickets by tab key, each list sorted. Every tab has an entry, empty or
/// not, so a tab's count is always there to read.
Map<String, List<Ticket>> groupTickets(Iterable<Ticket> tickets) {
  final out = {for (final t in workTabs) t.key: <Ticket>[]};
  for (final t in tickets) {
    out[workTabFor(t.status)]!.add(t);
  }
  for (final list in out.values) {
    list.sort(compareTickets);
  }
  return out;
}

/// A ticket's spend, short. Nothing for a ticket that has cost nothing, so a
/// list is not a column of "$0.00".
String formatCost(double usd) {
  if (usd <= 0) return '';
  if (usd < 0.01) return '<\$0.01';
  if (usd >= 100) return '\$${usd.toStringAsFixed(0)}';
  return '\$${usd.toStringAsFixed(2)}';
}

/// Ticket references typed as "T-3, t4 #5" for a blocked-by field, cleaned
/// up to what the server takes. Anything that is not a reference is left
/// out.
List<String> parseTicketRefs(String text) {
  final out = <String>[];
  for (final m in RegExp(r'(?:[Tt]-?|#)?(\d+)').allMatches(text)) {
    final ref = 'T-${m.group(1)}';
    if (!out.contains(ref)) out.add(ref);
  }
  return out;
}

/// Whether a verdict comment reads as a pass or a fail. Null when it says
/// neither plainly, which is rendered neutral rather than guessed.
bool? verdictPassed(String verdictOrBody) {
  final head = verdictOrBody.trim().split('\n').first.toLowerCase();
  final pass = RegExp(r'\bpass(ed|es)?\b').hasMatch(head);
  final fail = RegExp(r'\bfail(ed|s)?\b').hasMatch(head);
  if (pass == fail) return null;
  return pass;
}

/// The tab the Work screen opens on: the first, in the order work moves,
/// that has anything in it. A fleet whose every ticket is finished opens on
/// Done, not on an empty To do. Nothing anywhere stays on To do.
String firstBusyTab(Map<String, List<Ticket>> grouped) {
  for (final t in workTabs) {
    if ((grouped[t.key] ?? const []).isNotEmpty) return t.key;
  }
  return workTabs.first.key;
}

/// A ticket title as a list shows it. Titles are cut from what an agent
/// wrote, so they carry its markdown -- "I will create `notes/a.md`" --
/// which reads as noise in a one-line title.
String plainTitle(String title) {
  var s = title.replaceAll('`', '');
  s = s.replaceAllMapped(RegExp(r'(\*\*|__)(.+?)\1'), (m) => m[2]!);
  s = s.replaceAllMapped(RegExp(r'\*(\S(?:[^*]*\S)?)\*'), (m) => m[1]!);
  s = s.replaceAll(RegExp(r'^#+\s+'), '');
  return s.trim().isEmpty ? 'Untitled' : s.trim();
}

/// Whether [t] matches what was typed in the Work screen's search: its
/// reference ("T-12", "12"), words of its title, or who has it.
bool ticketMatches(Ticket t, String query) {
  final q = query.trim().toLowerCase();
  if (q.isEmpty) return true;
  final number = RegExp(r'^(?:t-?|#)?(\d+)$').firstMatch(q);
  if (number != null) return t.number.toString() == number.group(1);
  final hay = '${t.ref} ${t.title} ${t.assigneeName} ${t.stage}'.toLowerCase();
  return q.split(RegExp(r'\s+')).every(hay.contains);
}

/// What an agent's closing report says, with the lines an external run
/// appends about its files taken out and read as lists.
///
/// An agent on a PC ends its report with "Shared with the fleet
/// (read_work): a.md, b.md." and, for what it could not send, "Also
/// changed, not shared (binary or too large): x.png." (external.go).
class ReportFiles {
  const ReportFiles(this.body, this.shared, this.unshared);

  /// The report without those lines.
  final String body;

  /// Published to the work catalog under these names.
  final List<String> shared;

  /// Changed on the PC but not sent.
  final List<String> unshared;

  bool get hasFiles => shared.isNotEmpty || unshared.isNotEmpty;
}

const _sharedPrefix = 'Shared with the fleet (read_work):';
const _unsharedPrefix = 'Also changed, not shared (binary or too large):';

ReportFiles splitReportFiles(String text) {
  final shared = <String>[];
  final unshared = <String>[];
  final kept = <String>[];
  List<String> names(String rest) {
    var s = rest.trim();
    if (s.endsWith('.')) s = s.substring(0, s.length - 1);
    return [
      for (final n in s.split(', '))
        if (n.trim().isNotEmpty) n.trim(),
    ];
  }

  for (final line in text.split('\n')) {
    final l = line.trim();
    if (l.startsWith(_sharedPrefix)) {
      shared.addAll(names(l.substring(_sharedPrefix.length)));
    } else if (l.startsWith(_unsharedPrefix)) {
      unshared.addAll(names(l.substring(_unsharedPrefix.length)));
    } else {
      kept.add(line);
    }
  }
  if (shared.isEmpty && unshared.isEmpty) return ReportFiles(text, shared, unshared);
  return ReportFiles(kept.join('\n').trimRight(), shared, unshared);
}

/// What a "published" comment says was put in the catalog:
/// `Published file "notes/names.md" (version 1, 923 bytes)` (engine.go).
typedef PublishedItem = ({String kind, String name, int version, int bytes});

PublishedItem? parsePublished(String body) {
  final m = RegExp(
    r'^Published (\w+) "((?:[^"\\]|\\.)*)"(?: \(version (\d+), (\d+) bytes\))?',
  ).firstMatch(body.trim());
  if (m == null) return null;
  // Go's %q: a quote or backslash in the name arrives escaped.
  final name = m[2]!.replaceAllMapped(RegExp(r'\\(.)'), (x) => x[1]!);
  return (
    kind: m[1]!,
    name: name,
    version: int.tryParse(m[3] ?? '') ?? 0,
    bytes: int.tryParse(m[4] ?? '') ?? 0,
  );
}

/// A byte count, short.
String formatBytes(int n) {
  if (n < 1024) return '$n bytes';
  if (n < 1024 * 1024) return '${(n / 1024).toStringAsFixed(n < 10240 ? 1 : 0)} KB';
  return '${(n / (1024 * 1024)).toStringAsFixed(1)} MB';
}

/// The open tickets cancelling [rootId] cancels along with it: every open
/// ticket under it, and any open review or verify of anything in that tree.
/// The same walk the server does (`cancelUnderLocked` in tickets/ops.go),
/// so the confirmation can say how many before it happens. [all] should
/// include finished tickets: an open part can sit under a finished one.
List<Ticket> cancelCascade(String rootId, Iterable<Ticket> all) {
  final list = all.toList();
  final kids = <String, List<Ticket>>{};
  for (final t in list) {
    if (t.parentId.isNotEmpty) (kids[t.parentId] ??= []).add(t);
  }
  final inTree = <String>{rootId};
  final out = <Ticket>[];
  final stack = [rootId];
  while (stack.isNotEmpty) {
    final id = stack.removeLast();
    for (final k in kids[id] ?? const <Ticket>[]) {
      if (!inTree.add(k.id)) continue;
      if (k.isOpen) out.add(k);
      stack.add(k.id);
    }
  }
  for (final t in list) {
    if (t.isOpen &&
        !inTree.contains(t.id) &&
        (t.kind == 'review' || t.kind == 'verify') &&
        inTree.contains(t.targetId)) {
      out.add(t);
    }
  }
  return out;
}

/// What reopening [t] sends back. A request nobody is assigned is reopened
/// through its finished work parts (`reopenLocked`); anything else goes back
/// itself, so this is empty for it.
List<Ticket> reopenedParts(Ticket t, Iterable<Ticket> children) {
  if (!t.isUnassigned) return const [];
  return [
    for (final c in children)
      if (c.kind == 'work' && c.status == 'done') c,
  ];
}

/// "3 open parts and 1 check" for a cancel confirmation.
String describeCascade(List<Ticket> cascade) {
  final checks =
      cascade.where((t) => t.kind == 'review' || t.kind == 'verify').length;
  final parts = cascade.length - checks;
  String n(int k, String one, String many) => '$k ${k == 1 ? one : many}';
  return [
    if (parts > 0) n(parts, 'open part', 'open parts'),
    if (checks > 0) n(checks, 'open check', 'open checks'),
  ].join(' and ');
}
