import 'package:agentfleet_companion/core/models.dart';
import 'package:agentfleet_companion/features/work/work_logic.dart';
import 'package:flutter_test/flutter_test.dart';

Ticket tk(
  String id, {
  String parent = '',
  String status = 'todo',
  String kind = 'work',
  String target = '',
  String assignee = 'agent',
  String assigneeName = '',
  int number = 0,
  String title = '',
}) =>
    Ticket(
      id: id,
      number: number,
      ref: number > 0 ? 'T-$number' : id,
      title: title.isEmpty ? id : title,
      parentId: parent,
      status: status,
      kind: kind,
      targetId: target,
      assigneeId: assignee,
      assigneeName: assigneeName,
    );

void main() {
  group('splitReportFiles', () {
    test('takes the shared and unshared lines out of the report', () {
      const report = 'I wrote the notes.\n\n'
          'Shared with the fleet (read_work): publish_slogans.py, notes/slogans.md.\n'
          'Also changed, not shared (binary or too large): shot.png, big.bin.';
      final r = splitReportFiles(report);
      expect(r.body, 'I wrote the notes.');
      expect(r.shared, ['publish_slogans.py', 'notes/slogans.md']);
      expect(r.unshared, ['shot.png', 'big.bin']);
      expect(r.hasFiles, isTrue);
    });

    test('a report without them is left exactly as it was', () {
      const report = 'Done.\n\n- one\n- two\n';
      final r = splitReportFiles(report);
      expect(r.body, report);
      expect(r.hasFiles, isFalse);
    });

    test('only the unshared line', () {
      final r = splitReportFiles(
          'ok\nAlso changed, not shared (binary or too large): a.png.');
      expect(r.shared, isEmpty);
      expect(r.unshared, ['a.png']);
      expect(r.body, 'ok');
    });

    test('a name that is itself a dotfile keeps its dot', () {
      final r =
          splitReportFiles('Shared with the fleet (read_work): .env.example.');
      expect(r.shared, ['.env.example']);
    });
  });

  group('parsePublished', () {
    test('reads the engine\'s sentence', () {
      final p = parsePublished(
          'Published file "notes/names.md" (version 1, 923 bytes)');
      expect(p, isNotNull);
      expect(p!.kind, 'file');
      expect(p.name, 'notes/names.md');
      expect(p.version, 1);
      expect(p.bytes, 923);
    });

    test('unescapes a quoted name', () {
      final p = parsePublished(r'Published app "say \"hi\".html" (version 3, 10 bytes)');
      expect(p!.name, 'say "hi".html');
      expect(p.kind, 'app');
    });

    test('anything else is not a published file', () {
      expect(parsePublished('Checked it, looks fine.'), isNull);
    });

    test('formatBytes', () {
      expect(formatBytes(923), '923 bytes');
      expect(formatBytes(2048), '2.0 KB');
      expect(formatBytes(300 * 1024), '300 KB');
      expect(formatBytes(3 * 1024 * 1024), '3.0 MB');
    });
  });

  group('cancelCascade', () {
    test('open descendants at any depth, not finished ones, not itself', () {
      final all = [
        tk('root', assignee: ''),
        tk('a', parent: 'root', status: 'done'),
        tk('a1', parent: 'a', status: 'in_progress'),
        tk('b', parent: 'root', status: 'todo'),
        tk('c', parent: 'root', status: 'cancelled'),
        tk('other', status: 'todo'),
      ];
      final ids = cancelCascade('root', all).map((t) => t.id).toSet();
      expect(ids, {'a1', 'b'});
    });

    test('open reviews and verifies of anything in the tree come too', () {
      final all = [
        tk('root', assignee: ''),
        tk('a', parent: 'root', status: 'in_review'),
        // A review filed elsewhere but about a part of this tree.
        tk('rev', kind: 'review', target: 'a', status: 'todo'),
        // A verify of the root itself.
        tk('ver', kind: 'verify', target: 'root', status: 'in_progress'),
        // Finished checks stay as they are.
        tk('ver0', kind: 'verify', target: 'root', status: 'done'),
        // An unblock ticket is not a check.
        tk('unb', kind: 'unblock', target: 'a', status: 'todo'),
      ];
      final ids = cancelCascade('root', all).map((t) => t.id).toSet();
      expect(ids, {'a', 'rev', 'ver'});
    });

    test('a leaf with nothing open around it cascades to nothing', () {
      expect(cancelCascade('x', [tk('x')]), isEmpty);
    });

    test('describeCascade counts parts and checks apart', () {
      final c = [
        tk('a'),
        tk('b'),
        tk('v', kind: 'verify'),
      ];
      expect(describeCascade(c), '2 open parts and 1 open check');
      expect(describeCascade([tk('a')]), '1 open part');
      expect(describeCascade([tk('r', kind: 'review'), tk('v', kind: 'verify')]),
          '2 open checks');
    });
  });

  group('reopenedParts', () {
    test('a request reopens its finished work parts', () {
      final request = tk('req', assignee: '');
      final kids = [
        tk('w1', parent: 'req', status: 'done'),
        tk('w2', parent: 'req', status: 'in_progress'),
        tk('v', parent: 'req', status: 'done', kind: 'verify'),
      ];
      expect(reopenedParts(request, kids).map((t) => t.id), ['w1']);
    });

    test('an assigned ticket goes back itself', () {
      final t = tk('x', assignee: 'agent');
      expect(
          reopenedParts(t, [tk('w1', parent: 'x', status: 'done')]), isEmpty);
    });

    test('a ticket a person holds is not a request', () {
      const t = Ticket(id: 'x', number: 1, title: 'x', assigneeUserId: 'u1');
      expect(t.isUnassigned, isFalse);
    });
  });

  group('Work screen helpers', () {
    test('firstBusyTab opens where the work is', () {
      expect(firstBusyTab(groupTickets([])), 'todo');
      expect(
          firstBusyTab(groupTickets([tk('a', status: 'done')])), 'done');
      expect(
          firstBusyTab(groupTickets(
              [tk('a', status: 'done'), tk('b', status: 'blocked')])),
          'blocked');
    });

    test('plainTitle drops the markdown agents leave in titles', () {
      expect(plainTitle('I will create `notes/slogans.md` now'),
          'I will create notes/slogans.md now');
      expect(plainTitle('**Bold** and *soft*'), 'Bold and soft');
      expect(plainTitle('## Heading'), 'Heading');
      expect(plainTitle('2 * 3 * 4'), '2 * 3 * 4');
      expect(plainTitle(''), 'Untitled');
    });

    test('ticketMatches by reference, words and assignee', () {
      final t = tk('x',
          number: 16,
          title: 'Write notes/names.md with five ideas',
          assigneeName: 'Claude');
      expect(ticketMatches(t, ''), isTrue);
      expect(ticketMatches(t, 'T-16'), isTrue);
      expect(ticketMatches(t, '16'), isTrue);
      expect(ticketMatches(t, 't16'), isTrue);
      expect(ticketMatches(t, '1'), isFalse);
      expect(ticketMatches(t, 'names claude'), isTrue);
      expect(ticketMatches(t, 'names checker'), isFalse);
    });
  });
}
