/// The org chart's geometry and the rules around it, as plain Dart.
///
/// Nothing here imports Flutter: the layout is a function from "who reports
/// to whom" to rectangles and connector polylines, which is what makes it
/// testable without pumping a widget, and what lets the painter and the card
/// positions agree by construction.
library;

/// The id of the box at the top of the chart: you. Agents with no manager, or
/// a manager that is not in the chart, hang from it.
const orgRootId = '';

/// One agent as the layout sees it.
class OrgLayoutInput {
  const OrgLayoutInput({
    required this.id,
    this.parentId = '',
    this.sortKey = '',
  });

  final String id;

  /// Its manager's id. Empty, unknown or itself all mean "reports to you".
  final String parentId;

  /// Siblings are ordered by this, then by id, so the chart does not shuffle
  /// every time the server returns rows in a different order.
  final String sortKey;
}

/// A box's top-left corner, its depth (0 is you) and its size.
class OrgBox {
  const OrgBox({
    required this.id,
    required this.x,
    required this.y,
    required this.width,
    required this.height,
    required this.depth,
  });

  final String id;
  final double x;
  final double y;
  final double width;
  final double height;
  final int depth;

  double get centerX => x + width / 2;
  double get bottom => y + height;
}

/// A point on a connector.
typedef OrgPoint = ({double x, double y});

/// An elbow from a manager down to one report: down from the manager's
/// bottom edge to the rail, along it, and down into the report's top edge.
class OrgConnector {
  const OrgConnector({
    required this.fromId,
    required this.toId,
    required this.points,
  });

  final String fromId;
  final String toId;

  /// Four points: manager bottom-centre, rail above the manager, rail above
  /// the report, report top-centre.
  final List<OrgPoint> points;
}

class OrgLayout {
  const OrgLayout({
    required this.boxes,
    required this.connectors,
    required this.width,
    required this.height,
    required this.children,
  });

  /// Every box by id, including [orgRootId].
  final Map<String, OrgBox> boxes;
  final List<OrgConnector> connectors;

  /// The size of the whole chart, margins included.
  final double width;
  final double height;

  /// The tree actually drawn: who hangs under whom after unknown managers and
  /// loops were resolved.
  final Map<String, List<String>> children;
}

/// Lays the chart out top-down with "You" at the root.
///
/// A simple tidy tree: every subtree is as wide as its widest row of boxes,
/// siblings sit side by side with [hGap] between, and a manager is centred
/// over its reports. Loops cannot reach the root by following managers, so
/// an agent caught in one is hung from you and the loop is cut where it was
/// entered -- the chart still shows every agent exactly once.
OrgLayout layoutOrgTree(
  List<OrgLayoutInput> nodes, {
  double cardWidth = 196,
  double cardHeight = 124,
  double rootWidth = 132,
  double rootHeight = 56,
  double hGap = 20,
  double vGap = 44,
  double margin = 24,
}) {
  final byId = <String, OrgLayoutInput>{};
  for (final n in nodes) {
    if (n.id.isEmpty || byId.containsKey(n.id)) continue;
    byId[n.id] = n;
  }

  int order(String a, String b) {
    final ka = byId[a]?.sortKey.toLowerCase() ?? '';
    final kb = byId[b]?.sortKey.toLowerCase() ?? '';
    final c = ka.compareTo(kb);
    return c != 0 ? c : a.compareTo(b);
  }

  String parentOf(OrgLayoutInput n) {
    final p = n.parentId;
    if (p.isEmpty || p == n.id || !byId.containsKey(p)) return orgRootId;
    return p;
  }

  final declared = <String, List<String>>{};
  for (final n in byId.values) {
    (declared[parentOf(n)] ??= []).add(n.id);
  }
  for (final list in declared.values) {
    list.sort(order);
  }

  final tree = <String, List<String>>{orgRootId: []};
  final visited = <String>{orgRootId};
  void attach(String parent, String id) {
    if (!visited.add(id)) return;
    tree[parent]!.add(id);
    tree[id] = [];
    for (final c in declared[id] ?? const <String>[]) {
      attach(id, c);
    }
  }

  for (final id in declared[orgRootId] ?? const <String>[]) {
    attach(orgRootId, id);
  }
  // Whatever is left is in a loop. Hang the first of each from you.
  final leftover = byId.keys.where((id) => !visited.contains(id)).toList()
    ..sort(order);
  for (final id in leftover) {
    attach(orgRootId, id);
  }

  double boxW(String id) => id == orgRootId ? rootWidth : cardWidth;
  double boxH(String id) => id == orgRootId ? rootHeight : cardHeight;

  final span = <String, double>{};
  double measure(String id) {
    final kids = tree[id]!;
    var w = 0.0;
    for (var i = 0; i < kids.length; i++) {
      if (i > 0) w += hGap;
      w += measure(kids[i]);
    }
    final s = w > boxW(id) ? w : boxW(id);
    span[id] = s;
    return s;
  }

  measure(orgRootId);

  double rowTop(int depth) => depth == 0
      ? margin
      : margin + rootHeight + vGap + (depth - 1) * (cardHeight + vGap);

  final boxes = <String, OrgBox>{};
  var maxBottom = 0.0;
  void place(String id, double left, int depth) {
    final s = span[id]!;
    final w = boxW(id);
    final box = OrgBox(
      id: id,
      x: left + (s - w) / 2,
      y: rowTop(depth),
      width: w,
      height: boxH(id),
      depth: depth,
    );
    boxes[id] = box;
    if (box.bottom > maxBottom) maxBottom = box.bottom;

    final kids = tree[id]!;
    var kidsWidth = 0.0;
    for (var i = 0; i < kids.length; i++) {
      if (i > 0) kidsWidth += hGap;
      kidsWidth += span[kids[i]]!;
    }
    var cursor = left + (s - kidsWidth) / 2;
    for (final k in kids) {
      place(k, cursor, depth + 1);
      cursor += span[k]! + hGap;
    }
  }

  place(orgRootId, margin, 0);

  final connectors = <OrgConnector>[];
  for (final entry in tree.entries) {
    final parent = boxes[entry.key]!;
    for (final kid in entry.value) {
      final child = boxes[kid]!;
      final railY = parent.bottom + vGap / 2;
      connectors.add(OrgConnector(
        fromId: parent.id,
        toId: kid,
        points: [
          (x: parent.centerX, y: parent.bottom),
          (x: parent.centerX, y: railY),
          (x: child.centerX, y: railY),
          (x: child.centerX, y: child.y),
        ],
      ));
    }
  }

  return OrgLayout(
    boxes: boxes,
    connectors: connectors,
    width: span[orgRootId]! + margin * 2,
    height: maxBottom + margin,
    children: tree,
  );
}

/// Everyone under [id], at any depth, following declared managers. Guards
/// against loops already in the data.
Set<String> reportsUnder(List<OrgLayoutInput> nodes, String id) {
  final kids = <String, List<String>>{};
  for (final n in nodes) {
    (kids[n.parentId] ??= []).add(n.id);
  }
  final out = <String>{};
  final stack = [...?kids[id]];
  while (stack.isNotEmpty) {
    final c = stack.removeLast();
    if (c == id || !out.add(c)) continue;
    stack.addAll(kids[c] ?? const []);
  }
  return out;
}

/// Whether making [agentId] report to [managerId] would close a loop. The
/// server refuses it too; checking here lets a drag be rejected before it is
/// dropped. Reporting to you ([orgRootId]) never does.
bool wouldCreateCycle(
    List<OrgLayoutInput> nodes, String agentId, String managerId) {
  if (managerId == orgRootId) return false;
  if (managerId == agentId) return true;
  return reportsUnder(nodes, agentId).contains(managerId);
}

/// What an agent is doing, as the chart says it.
enum OrgActivity { working, idle, held, offline }

/// The one-line status on a card. A hold outranks everything: an agent at its
/// budget ceiling is not given work however idle or online it is.
({OrgActivity activity, String label}) orgStatus({
  required bool online,
  required bool busy,
  required String hold,
  String ticketRef = '',
}) {
  if (hold == 'budget') {
    return (activity: OrgActivity.held, label: 'Held at budget');
  }
  if (hold.isNotEmpty) return (activity: OrgActivity.held, label: 'Held');
  if (!online) return (activity: OrgActivity.offline, label: 'Offline');
  if (busy) {
    return (
      activity: OrgActivity.working,
      label: ticketRef.isEmpty ? 'Working' : 'Working on $ticketRef',
    );
  }
  return (activity: OrgActivity.idle, label: 'Idle');
}

/// Up to two initials for an avatar: "Code Reviewer" is CR, "gui-scout" is
/// GS, "Builder" is B.
String initialsOf(String name) {
  final words = name
      .trim()
      .split(RegExp(r'[\s_\-.]+'))
      .where((w) => w.isNotEmpty)
      .toList();
  if (words.isEmpty) return '?';
  if (words.length == 1) return words.first.substring(0, 1).toUpperCase();
  return (words[0].substring(0, 1) + words[1].substring(0, 1)).toUpperCase();
}
