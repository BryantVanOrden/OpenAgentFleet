import 'package:agentfleet_companion/core/models.dart';
import 'package:agentfleet_companion/core/state.dart';
import 'package:agentfleet_companion/features/work/work_logic.dart';
import 'package:flutter_test/flutter_test.dart';

Ticket t(int n,
        {String status = 'todo',
        int priority = 0,
        String? updated,
        String? done}) =>
    Ticket(
      id: 'id$n',
      number: n,
      ref: 'T-$n',
      title: 't$n',
      status: status,
      priority: priority,
      updatedAt: updated == null ? null : DateTime.parse(updated),
      doneAt: done == null ? null : DateTime.parse(done),
    );

void main() {
  group('groupTickets', () {
    test('every tab is present, even empty', () {
      final g = groupTickets(const []);
      expect(g.keys, workTabs.map((w) => w.key));
      expect(g.values.every((l) => l.isEmpty), isTrue);
    });

    test('statuses land in their tabs; cancelled goes with done', () {
      final g = groupTickets([
        t(1),
        t(2, status: 'in_progress'),
        t(3, status: 'in_review'),
        t(4, status: 'blocked'),
        t(5, status: 'done'),
        t(6, status: 'cancelled'),
        t(7, status: 'backlog'),
        t(8, status: 'something_new'),
      ]);
      expect(g['todo']!.map((x) => x.number), [1]);
      expect(g['in_progress']!.map((x) => x.number), [2]);
      expect(g['in_review']!.map((x) => x.number), [3]);
      expect(g['blocked']!.map((x) => x.number), [4]);
      expect(g['done']!.map((x) => x.number).toSet(), {5, 6});
      // An unknown status is kept, in the backlog.
      expect(g['backlog']!.map((x) => x.number).toSet(), {7, 8});
    });

    test('open work: priority, then most recently touched, then newest', () {
      final g = groupTickets([
        t(1, updated: '2026-09-20T10:00:00Z'),
        t(2, updated: '2026-09-21T10:00:00Z'),
        t(3, priority: 1, updated: '2026-09-01T10:00:00Z'),
        t(4),
        t(5),
      ]);
      expect(g['todo']!.map((x) => x.number), [3, 2, 1, 5, 4]);
    });

    test('finished work: most recently finished first', () {
      final g = groupTickets([
        t(1, status: 'done', done: '2026-09-20T10:00:00Z'),
        t(2, status: 'done', done: '2026-09-22T10:00:00Z'),
        t(3, status: 'cancelled', updated: '2026-09-21T10:00:00Z'),
      ]);
      expect(g['done']!.map((x) => x.number), [2, 3, 1]);
    });
  });

  test('formatCost', () {
    expect(formatCost(0), '');
    expect(formatCost(0.004), '<\$0.01');
    expect(formatCost(0.42), '\$0.42');
    expect(formatCost(12.345), '\$12.35');
    expect(formatCost(250), '\$250');
  });

  test('parseTicketRefs reads what people type', () {
    expect(parseTicketRefs('T-3, t4 #5 6, T-3'), ['T-3', 'T-4', 'T-5', 'T-6']);
    expect(parseTicketRefs(''), isEmpty);
    expect(parseTicketRefs('none here'), isEmpty);
  });

  test('verdictPassed', () {
    expect(verdictPassed('pass'), isTrue);
    expect(verdictPassed('Verdict: PASS\nall good'), isTrue);
    expect(verdictPassed('fail: tests missing'), isFalse);
    expect(verdictPassed('Failed on Windows'), isFalse);
    expect(verdictPassed('looked at it'), isNull);
    // Says both on the first line: not guessed.
    expect(verdictPassed('pass, then fail'), isNull);
  });

  group('ticketEventTouches', () {
    final page = TicketDetail(
      ticket: const Ticket(id: 'me', number: 1, title: 'x', taskId: 'live'),
      children: const [Ticket(id: 'kid', number: 2, title: 'k')],
      runs: [
        Task(
          id: 'old',
          instanceId: 'i',
          goal: '',
          state: 'succeeded',
          step: 0,
          maxSteps: 0,
          createdAt: DateTime(2026),
        ),
      ],
    );

    FleetEvent ev(String type, [Object? payload, String? task]) =>
        FleetEvent(type: type, payload: payload, taskId: task);

    test('the ticket itself, a child, a new child, a new dependent', () {
      expect(ticketEventTouches(ev('ticket', {'id': 'me'}), page), isTrue);
      expect(ticketEventTouches(ev('ticket', {'id': 'kid'}), page), isTrue);
      expect(
          ticketEventTouches(
              ev('ticket', {'id': 'new', 'parent_id': 'me'}), page),
          isTrue);
      expect(
          ticketEventTouches(
              ev('ticket', {
                'id': 'dep',
                'blocked_by': ['me']
              }),
              page),
          isTrue);
      expect(ticketEventTouches(ev('ticket', {'id': 'other'}), page), isFalse);
    });

    test('comments on this ticket only', () {
      expect(ticketEventTouches(ev('ticket.comment', {'ticket_id': 'me'}), page),
          isTrue);
      expect(
          ticketEventTouches(ev('ticket.comment', {'ticket_id': 'kid'}), page),
          isFalse);
    });

    test('runs of this ticket', () {
      expect(ticketEventTouches(ev('task.state', null, 'live'), page), isTrue);
      expect(ticketEventTouches(ev('task.state', null, 'old'), page), isTrue);
      expect(ticketEventTouches(ev('task.state', null, 'x'), page), isFalse);
      expect(ticketEventTouches(ev('task.step', null, 'live'), page), isFalse);
    });

    test('a malformed payload is ignored', () {
      expect(ticketEventTouches(ev('ticket', 'nope'), page), isFalse);
      expect(ticketEventTouches(ev('ticket.comment'), page), isFalse);
    });
  });
}
