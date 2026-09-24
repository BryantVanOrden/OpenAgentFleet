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
