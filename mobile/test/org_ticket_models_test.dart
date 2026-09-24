import 'package:agentfleet_companion/core/models.dart';
import 'package:flutter_test/flutter_test.dart';

void main() {
  group('Instance', () {
    test('an orchestrator from before the org chart reads as a desktop', () {
      final i = Instance.fromJson({
        'id': 'i1',
        'name': 'Builder',
        'state': 'running',
        'created_at': '2026-09-01T00:00:00Z',
      });
      expect(i.kind, AgentKind.desktop);
      expect(i.isExternal, isFalse);
      expect(i.reportsTo, '');
      expect(i.title, '');
      expect(i.capabilities, '');
      expect(i.connection.isEmpty, isTrue);
      expect(i.budgetMonthUsd, 0);
      expect(i.budgetWarnPct, 0);
      expect(i.trust, 'standard');
      expect(i.hold, '');
      expect(i.isHeld, isFalse);
    });

    test('an empty kind is a desktop too', () {
      final i = Instance.fromJson({'id': 'i1', 'kind': '', 'trust': ''});
      expect(i.kind, AgentKind.desktop);
      expect(i.trust, 'standard');
    });

    test('an external agent carries its place and connection', () {
      final i = Instance.fromJson({
        'id': 'i2',
        'name': 'Coder',
        'kind': 'claude_code',
        'reports_to': 'i1',
        'title': 'Engineer',
        'capabilities': 'Refactors',
        'connection': {
          'device_id': 'd1',
          'cwd': 'C:/work/app',
          'model': 'sonnet',
          'args': ['--verbose'],
          'token_ref': 'agent/i2/token',
          'autonomy': 'full',
          'timeout_sec': 600,
        },
        'budget_month_usd': 10,
        'budget_warn_pct': 75,
        'trust': 'low',
        'hold': 'budget',
      });
      expect(i.kind, AgentKind.claudeCode);
      expect(i.isExternal, isTrue);
      expect(i.reportsTo, 'i1');
      expect(i.title, 'Engineer');
      expect(i.capabilities, 'Refactors');
      expect(i.connection.deviceId, 'd1');
      expect(i.connection.cwd, 'C:/work/app');
      expect(i.connection.model, 'sonnet');
      expect(i.connection.args, ['--verbose']);
      expect(i.connection.hasToken, isTrue);
      expect(i.connection.autonomy, 'full');
      expect(i.connection.timeoutSec, 600);
      expect(i.budgetMonthUsd, 10.0);
      expect(i.budgetWarnPct, 75);
      expect(i.lowTrust, isTrue);
      expect(i.isHeld, isTrue);
    });
  });

  group('AgentConnection', () {
    test('never sends the token ref back, and omits what is empty', () {
      const c = AgentConnection(
          url: 'wss://gw:18789', agentId: 'main', tokenRef: 'agent/x/token');
      expect(c.toJson(), {'url': 'wss://gw:18789', 'agent_id': 'main'});
    });

    test('summary says where the agent lives', () {
      expect(const AgentConnection(deviceId: 'd', cwd: 'C:/a').summary(deviceName: 'Desk'),
          'C:/a on Desk');
      expect(const AgentConnection(url: 'https://hooks.example/x').summary(),
          'hooks.example');
      expect(const AgentConnection().summary(), 'not connected yet');
      expect(
          const AgentConnection(deviceId: 'd', cwd: r'C:\Users\me\work\cc-work\')
              .summary(deviceName: 'Dev PC', short: true),
          'cc-work on Dev PC');
      expect(const AgentConnection(deviceId: 'd').summary(short: true),
          'on a PC');
    });
  });

  group('AgentKind', () {
    test('labels and placement', () {
      expect(AgentKind.label('claude_code'), 'Claude Code');
      expect(AgentKind.label('openclaw'), 'OpenClaw');
      expect(AgentKind.label(''), 'Desktop');
      expect(AgentKind.label('something_new'), 'something_new');
      expect(AgentKind.onDevice('codex'), isTrue);
      expect(AgentKind.onDevice('webhook'), isFalse);
      expect(AgentKind.isExternal('desktop'), isFalse);
      expect(AgentKind.isExternal('hermes'), isTrue);
    });
  });

  group('OafDevice runtimes', () {
    test('missing runtimes means unknown, which can run anything', () {
      final d = OafDevice.fromJson({'id': 'd1', 'name': 'PC'});
      expect(d.runtimes, isEmpty);
      expect(d.canRun('claude_code'), isTrue);
    });

    test('a reported list is honoured', () {
      final d = OafDevice.fromJson({
        'id': 'd1',
        'runtimes': ['claude_code', 'codex'],
      });
      expect(d.canRun('codex'), isTrue);
      expect(d.canRun('hermes'), isFalse);
    });
  });

  group('Ticket', () {
    test('a TicketView parses every field', () {
      final t = Ticket.fromJson({
        'id': 't1',
        'ref': 'T-12',
        'number': 12,
        'title': 'Build it',
        'description': 'All of it',
        'kind': 'review',
        'status': 'in_review',
        'priority': 2,
        'parent_id': 'p',
        'target_id': 'w',
        'assignee_id': 'a',
        'assignee_name': 'Builder',
        'reviewer_id': 'r',
        'reviewer_name': 'Checker',
        'verifier_id': 'v',
        'verifier_name': 'Boss',
        'thread': 'broadcast',
        'origin': 'make a game',
        'stage': 'build',
        'task_id': 'task',
        'attempts': 1,
        'rounds': 2,
        'wakes': 3,
        'verdict': 'fail',
        'result': 'report',
        'blocked_reason': '',
        'budget_usd': 5,
        'cost_usd': 0.42,
        'blocked_by': ['b1', 'b2'],
        'created_at': '2026-09-20T10:00:00Z',
        'updated_at': '2026-09-20T11:00:00Z',
        'started_at': '2026-09-20T10:05:00Z',
      });
      expect(t.ref, 'T-12');
      expect(t.kind, 'review');
      expect(t.isMeta, isTrue);
      expect(t.status, 'in_review');
      expect(t.assigneeName, 'Builder');
      expect(t.reviewerName, 'Checker');
      expect(t.verifierName, 'Boss');
      expect(t.rounds, 2);
      expect(t.verdict, 'fail');
      expect(t.costUsd, 0.42);
      expect(t.budgetUsd, 5.0);
      expect(t.blockedBy, ['b1', 'b2']);
      expect(t.startedAt, isNotNull);
      expect(t.doneAt, isNull);
      expect(t.isRoot, isFalse);
      expect(t.isOpen, isTrue);
    });

    test('the bare socket payload has no ref or names; ref comes from number',
        () {
      final t = Ticket.fromJson({
        'id': 't2',
        'number': 7,
        'title': 'x',
        'kind': 'work',
        'status': 'done',
        'blocked_by': null,
      });
      expect(t.ref, 'T-7');
      expect(t.assigneeName, '');
      expect(t.blockedBy, isEmpty);
      expect(t.isTerminal, isTrue);
    });

    test('missing kind and status fall back to work and todo', () {
      final t = Ticket.fromJson({'id': 't3'});
      expect(t.kind, 'work');
      expect(t.status, 'todo');
      expect(t.ref, '');
      expect(t.isRoot, isTrue);
    });

    test('status labels', () {
      expect(Ticket.statusLabel('in_progress'), 'In progress');
      expect(Ticket.statusLabel('todo'), 'To do');
      expect(Ticket.statusLabel('weird_one'), 'weird one');
    });
  });

  group('TicketDetail', () {
    test('parses the whole page and tolerates null lists', () {
      final d = TicketDetail.fromJson({
        'ticket': {'id': 't1', 'number': 1, 'title': 'root'},
        'ancestry': [],
        'children': [
          {'id': 'c1', 'number': 2, 'title': 'child', 'parent_id': 't1'},
        ],
        'blockers': null,
        'dependents': null,
        'comments': [
          {
            'id': 'm1',
            'ticket_id': 't1',
            'author_name': 'Checker',
            'kind': 'verdict',
            'body': 'pass',
            'created_at': '2026-09-20T10:00:00Z',
          },
          {'id': 'm2', 'ticket_id': 't1', 'author_user_id': 'u', 'body': 'hi'},
        ],
        'runs': [
          {'id': 'task1', 'instance_id': 'i1', 'state': 'succeeded'},
          {'instance_id': 'no id, skipped'},
        ],
      });
      expect(d.ticket.ref, 'T-1');
      expect(d.children.single.ref, 'T-2');
      expect(d.blockers, isEmpty);
      expect(d.dependents, isEmpty);
      expect(d.comments, hasLength(2));
      expect(d.comments.first.kind, 'verdict');
      expect(d.comments.last.kind, 'comment');
      expect(d.comments.last.byPerson, isTrue);
      expect(d.runs.single.id, 'task1');
      expect(d.relatedIds, {'t1', 'c1'});
    });

    test('an empty body still yields a page', () {
      final d = TicketDetail.fromJson(const {});
      expect(d.ticket.id, '');
      expect(d.comments, isEmpty);
    });
  });

  group('OrgChart', () {
    test('parses nodes and kinds', () {
      final c = OrgChart.fromJson({
        'nodes': [
          {
            'id': 'a',
            'name': 'Builder',
            'kind': 'desktop',
            'state': 'running',
            'trust': 'standard',
            'busy': true,
            'ticket_ref': 'T-12',
            'ticket_title': 'Build',
            'spend_month_usd': 3.1,
            'budget_month_usd': 10,
            'open_tickets': 2,
            'online': true,
          },
          {'id': 'b', 'name': 'Old'},
        ],
        'kinds': [
          {'kind': 'desktop', 'label': 'Desktop', 'on_device': false},
          {'kind': 'claude_code', 'label': 'Claude Code', 'on_device': true},
          {'kind': 'codex'},
        ],
      });
      final a = c.nodes.first;
      expect(a.busy, isTrue);
      expect(a.ticketRef, 'T-12');
      expect(a.hasBudget, isTrue);
      expect(a.budgetFraction, closeTo(0.31, 1e-9));
      expect(a.openTickets, 2);
      final b = c.nodes.last;
      expect(b.kind, AgentKind.desktop);
      expect(b.trust, 'standard');
      expect(b.online, isFalse);
      expect(b.hasBudget, isFalse);
      expect(b.budgetFraction, 0);
      expect(c.kinds.map((k) => k.kind), ['desktop', 'claude_code', 'codex']);
      expect(c.kinds[1].onDevice, isTrue);
      expect(c.kinds[2].label, 'Codex');
      expect(c.kinds[2].onDevice, isTrue);
    });

    test('a server with no kinds list', () {
      final c = OrgChart.fromJson({'nodes': null});
      expect(c.nodes, isEmpty);
      expect(c.kinds, isEmpty);
    });
  });
}
