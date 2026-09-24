import 'package:agentfleet_companion/features/org/org_layout.dart';
import 'package:flutter_test/flutter_test.dart';

OrgLayoutInput n(String id, [String parent = '', String? name]) =>
    OrgLayoutInput(id: id, parentId: parent, sortKey: name ?? id);

void main() {
  const w = 100.0, h = 50.0, rw = 60.0, rh = 20.0, hg = 10.0, vg = 30.0, m = 5.0;

  OrgLayout lay(List<OrgLayoutInput> nodes) => layoutOrgTree(
        nodes,
        cardWidth: w,
        cardHeight: h,
        rootWidth: rw,
        rootHeight: rh,
        hGap: hg,
        vGap: vg,
        margin: m,
      );

  group('layoutOrgTree', () {
    test('an empty fleet is just you', () {
      final l = lay(const []);
      expect(l.boxes.keys, [orgRootId]);
      expect(l.connectors, isEmpty);
      final you = l.boxes[orgRootId]!;
      expect(you.x, m);
      expect(you.y, m);
      expect(you.width, rw);
      expect(l.width, rw + 2 * m);
      expect(l.height, m + rh + m);
    });

    test('three agents reporting to you sit in one row, you centred above',
        () {
      final l = lay([n('b'), n('a'), n('c')]);
      // Sorted by name, left to right.
      final row = ['a', 'b', 'c'].map((id) => l.boxes[id]!).toList();
      expect(row.map((b) => b.x), [m, m + w + hg, m + 2 * (w + hg)]);
      for (final b in row) {
        expect(b.y, m + rh + vg);
        expect(b.depth, 1);
      }
      final span = 3 * w + 2 * hg;
      expect(l.width, span + 2 * m);
      expect(l.boxes[orgRootId]!.centerX, closeTo(m + span / 2, 1e-9));
      expect(l.height, m + rh + vg + h + m);
    });

    test('a manager is centred over its reports and a lone chain stacks', () {
      final l = lay([
        n('lead'),
        n('x', 'lead'),
        n('y', 'lead'),
        n('solo'),
        n('deep', 'x'),
      ]);
      final lead = l.boxes['lead']!;
      final x = l.boxes['x']!;
      final y = l.boxes['y']!;
      expect(lead.depth, 1);
      expect(x.depth, 2);
      expect(l.boxes['deep']!.depth, 3);
      expect(lead.centerX, closeTo((x.centerX + y.centerX) / 2, 1e-9));
      // A report sits directly under a manager with one report.
      expect(l.boxes['deep']!.centerX, closeTo(x.centerX, 1e-9));
      // Row 2 starts one card and one gap below row 1.
      expect(x.y, lead.y + h + vg);
      // Nothing overlaps within a row.
      final byDepth = <int, List<OrgBox>>{};
      for (final b in l.boxes.values) {
        (byDepth[b.depth] ??= []).add(b);
      }
      for (final row in byDepth.values) {
        row.sort((a, b) => a.x.compareTo(b.x));
        for (var i = 1; i < row.length; i++) {
          expect(row[i].x, greaterThanOrEqualTo(row[i - 1].x + row[i - 1].width));
        }
      }
    });

    test('an unknown or self manager reports to you', () {
      final l = lay([n('a', 'ghost'), n('b', 'b')]);
      expect(l.children[orgRootId], ['a', 'b']);
      expect(l.boxes['a']!.depth, 1);
      expect(l.boxes['b']!.depth, 1);
    });

    test('a loop is cut and every agent still appears exactly once', () {
      final l = lay([n('a', 'b'), n('b', 'a'), n('c', 'a')]);
      expect(l.boxes.length, 4);
      expect(l.children[orgRootId], ['a']);
      expect(l.children['a'], containsAll(['b', 'c']));
      expect(l.connectors.length, 3);
    });

    test('duplicate and empty ids are ignored', () {
      final l = lay([n('a'), n('a', 'x'), n('')]);
      expect(l.boxes.keys.toSet(), {orgRootId, 'a'});
    });

    test('connectors are elbows from manager bottom to report top', () {
      final l = lay([n('a'), n('b')]);
      final you = l.boxes[orgRootId]!;
      final a = l.boxes['a']!;
      final c = l.connectors.firstWhere((c) => c.toId == 'a');
      expect(c.fromId, orgRootId);
      expect(c.points, hasLength(4));
      expect(c.points[0], (x: you.centerX, y: you.bottom));
      expect(c.points[1], (x: you.centerX, y: you.bottom + vg / 2));
      expect(c.points[2], (x: a.centerX, y: you.bottom + vg / 2));
      expect(c.points[3], (x: a.centerX, y: a.y));
    });

    test('siblings are ordered by name, case-insensitively, then id', () {
      final l = lay([n('1', '', 'zed'), n('2', '', 'Alpha'), n('3', '', 'beta')]);
      expect(l.children[orgRootId], ['2', '3', '1']);
    });
  });

  group('reporting lines', () {
    final nodes = [n('a'), n('b', 'a'), n('c', 'b'), n('d')];

    test('reportsUnder follows every level', () {
      expect(reportsUnder(nodes, 'a'), {'b', 'c'});
      expect(reportsUnder(nodes, 'c'), isEmpty);
    });

    test('moving someone under their own report is a loop', () {
      expect(wouldCreateCycle(nodes, 'a', 'c'), isTrue);
      expect(wouldCreateCycle(nodes, 'a', 'b'), isTrue);
      expect(wouldCreateCycle(nodes, 'a', 'a'), isTrue);
    });

    test('moving across the chart or to you is not', () {
      expect(wouldCreateCycle(nodes, 'c', 'd'), isFalse);
      expect(wouldCreateCycle(nodes, 'd', 'c'), isFalse);
      expect(wouldCreateCycle(nodes, 'a', orgRootId), isFalse);
    });

    test('a loop already in the data does not hang the check', () {
      final looped = [n('a', 'b'), n('b', 'a')];
      expect(reportsUnder(looped, 'a'), {'b'});
      expect(wouldCreateCycle(looped, 'a', 'b'), isTrue);
    });
  });

  group('orgStatus', () {
    test('working names the ticket', () {
      final s = orgStatus(online: true, busy: true, hold: '', ticketRef: 'T-12');
      expect(s.activity, OrgActivity.working);
      expect(s.label, 'Working on T-12');
    });

    test('a budget hold outranks being busy and online', () {
      final s = orgStatus(online: true, busy: true, hold: 'budget');
      expect(s.activity, OrgActivity.held);
      expect(s.label, 'Held at budget');
    });

    test('offline outranks idle and busy', () {
      expect(orgStatus(online: false, busy: true, hold: '').activity,
          OrgActivity.offline);
    });

    test('online and not busy is idle', () {
      final s = orgStatus(online: true, busy: false, hold: '');
      expect(s.activity, OrgActivity.idle);
      expect(s.label, 'Idle');
    });
  });

  test('initialsOf', () {
    expect(initialsOf('Builder'), 'B');
    expect(initialsOf('gui-scout'), 'GS');
    expect(initialsOf('code reviewer bot'), 'CR');
    expect(initialsOf('  '), '?');
  });
}
