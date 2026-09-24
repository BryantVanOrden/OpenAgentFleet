import 'package:flutter/material.dart';

import '../../core/models.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/agent_kind_badge.dart';
import 'work_logic.dart';

/// The colour a ticket status wears everywhere.
Color ticketStatusColor(String status) => switch (status) {
      'in_progress' => Fleet.good,
      'in_review' => Fleet.cool,
      'blocked' => Fleet.warn,
      'done' => Fleet.good,
      'cancelled' => Fleet.ink400,
      'backlog' => Fleet.ink400,
      _ => Fleet.ink300,
    };

IconData ticketKindIcon(String kind) => switch (kind) {
      'review' => Icons.rate_review_outlined,
      'verify' => Icons.fact_check_outlined,
      'unblock' => Icons.lock_open_outlined,
      _ => Icons.construction_outlined,
    };

/// A small rounded label. Used for status, kind and verdict, so the three
/// read as one family.
class TicketPill extends StatelessWidget {
  const TicketPill({
    super.key,
    required this.label,
    required this.color,
    this.icon,
  });

  final String label;
  final Color color;
  final IconData? icon;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.13),
        borderRadius: BorderRadius.circular(999),
        border: Border.all(color: color.withValues(alpha: 0.35)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          if (icon != null) ...[
            Icon(icon, size: 11, color: color),
            const SizedBox(width: 3),
          ],
          Text(label,
              style: TextStyle(
                  color: color, fontSize: 10.5, fontWeight: FontWeight.w600)),
        ],
      ),
    );
  }
}

class TicketStatusPill extends StatelessWidget {
  const TicketStatusPill(this.status, {super.key});
  final String status;

  @override
  Widget build(BuildContext context) => TicketPill(
        label: Ticket.statusLabel(status),
        color: ticketStatusColor(status),
      );
}

/// Only for review, verify and unblock tickets: work about other work.
class TicketKindPill extends StatelessWidget {
  const TicketKindPill(this.kind, {super.key});
  final String kind;

  @override
  Widget build(BuildContext context) => TicketPill(
        label: switch (kind) {
          'review' => 'Review',
          'verify' => 'Verify',
          'unblock' => 'Unblock',
          _ => 'Work',
        },
        color: switch (kind) {
          'review' => Fleet.cool,
          'verify' => Fleet.live,
          'unblock' => Fleet.warn,
          _ => Fleet.ink300,
        },
        icon: ticketKindIcon(kind),
      );
}

class VerdictPill extends StatelessWidget {
  const VerdictPill(this.verdict, {super.key});
  final String verdict;

  @override
  Widget build(BuildContext context) {
    final pass = verdict == 'pass';
    return TicketPill(
      label: pass ? 'Pass' : 'Fail',
      color: pass ? Fleet.good : Fleet.bad,
      icon: pass ? Icons.check_rounded : Icons.close_rounded,
    );
  }
}

/// One ticket in a list.
class TicketRow extends StatelessWidget {
  const TicketRow({
    super.key,
    required this.ticket,
    required this.onTap,
    this.showStatus = false,
  });

  final Ticket ticket;
  final VoidCallback onTap;

  /// For lists that mix statuses (children, blockers); the Work tabs already
  /// say it.
  final bool showStatus;

  @override
  Widget build(BuildContext context) {
    final t = ticket;
    final cost = formatCost(t.costUsd);
    return Card(
      margin: const EdgeInsets.only(bottom: 8),
      child: InkWell(
        borderRadius: BorderRadius.circular(14),
        onTap: onTap,
        child: Padding(
          padding: const EdgeInsets.fromLTRB(14, 11, 14, 11),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Row(
                children: [
                  Text(t.ref,
                      style: TextStyle(
                          color: Fleet.ink400,
                          fontSize: 11.5,
                          fontFamily: 'monospace',
                          fontWeight: FontWeight.w600)),
                  if (t.isMeta) ...[
                    const SizedBox(width: 8),
                    TicketKindPill(t.kind),
                  ],
                  if (showStatus) ...[
                    const SizedBox(width: 8),
                    TicketStatusPill(t.status),
                  ],
                  const Spacer(),
                  if (t.verdict == 'pass' || t.verdict == 'fail') ...[
                    VerdictPill(t.verdict),
                    const SizedBox(width: 8),
                  ],
                  if (cost.isNotEmpty)
                    Text(cost,
                        style: TextStyle(
                          color: Fleet.ink300,
                          fontSize: 11.5,
                          fontFeatures: const [FontFeature.tabularFigures()],
                        )),
                ],
              ),
              const SizedBox(height: 5),
              Text(
                t.title.isEmpty ? 'Untitled' : t.title,
                maxLines: 2,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                    color: t.status == 'cancelled' ? Fleet.ink400 : Fleet.ink100,
                    decoration: t.status == 'cancelled'
                        ? TextDecoration.lineThrough
                        : null,
                    fontSize: 14,
                    fontWeight: FontWeight.w600),
              ),
              const SizedBox(height: 6),
              Wrap(
                spacing: 12,
                runSpacing: 4,
                crossAxisAlignment: WrapCrossAlignment.center,
                children: [
                  _Meta(
                    icon: Icons.person_outline,
                    text: t.assigneeName.isNotEmpty
                        ? t.assigneeName
                        : t.assigneeId.isEmpty
                            ? 'Nobody yet'
                            : 'Someone you cannot see',
                    dim: t.assigneeId.isEmpty,
                  ),
                  if (t.blockedBy.isNotEmpty)
                    _Meta(
                      icon: Icons.link_rounded,
                      text: 'waits on ${t.blockedBy.length}',
                    ),
                  if (t.status == 'blocked' && t.blockedReason.isNotEmpty)
                    _Meta(
                      icon: Icons.report_gmailerrorred_outlined,
                      text: t.blockedReason,
                      color: Fleet.warn,
                    ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }
}

class _Meta extends StatelessWidget {
  const _Meta({
    required this.icon,
    required this.text,
    this.dim = false,
    this.color,
  });

  final IconData icon;
  final String text;
  final bool dim;
  final Color? color;

  @override
  Widget build(BuildContext context) {
    final c = color ?? (dim ? Fleet.ink500 : Fleet.ink300);
    return ConstrainedBox(
      constraints: const BoxConstraints(maxWidth: 260),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(icon, size: 13, color: c),
          const SizedBox(width: 4),
          Flexible(
            child: Text(text,
                maxLines: 1,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(color: c, fontSize: 11.5)),
          ),
        ],
      ),
    );
  }
}

/// A picker over the fleet's agents, for assignee, reviewer and verifier.
/// Returns the chosen id, '' for nobody, or null when dismissed.
Future<String?> pickAgent(
  BuildContext context, {
  required String title,
  required List<Instance> agents,
  String current = '',
  bool allowNobody = true,
}) {
  final sorted = [...agents]
    ..sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
  return showModalBottomSheet<String>(
    context: context,
    backgroundColor: Fleet.ink900,
    isScrollControlled: true,
    shape: const RoundedRectangleBorder(
      borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
    ),
    builder: (ctx) => ConstrainedBox(
      constraints: BoxConstraints(
          maxHeight: MediaQuery.sizeOf(ctx).height * 0.7),
      child: SafeArea(
        child: ListView(
          shrinkWrap: true,
          padding: const EdgeInsets.symmetric(vertical: 12),
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 4, 20, 8),
              child: Text(title,
                  style: Theme.of(ctx).textTheme.titleMedium),
            ),
            if (allowNobody)
              ListTile(
                leading: Icon(Icons.person_off_outlined, color: Fleet.ink400),
                title: const Text('Nobody'),
                trailing: current.isEmpty
                    ? Icon(Icons.check, color: Fleet.live)
                    : null,
                onTap: () => Navigator.pop(ctx, ''),
              ),
            if (sorted.isEmpty)
              Padding(
                padding: const EdgeInsets.all(20),
                child: Text('No agents in the fleet yet.',
                    style: TextStyle(color: Fleet.ink400)),
              ),
            for (final a in sorted)
              ListTile(
                leading: Icon(agentKindIcon(a.kind),
                    color: agentKindColor(a.kind)),
                title: Text(a.name),
                subtitle: Text(
                  [
                    if (a.title.isNotEmpty) a.title,
                    AgentKind.label(a.kind),
                  ].join(' · '),
                  style: TextStyle(color: Fleet.ink400, fontSize: 12),
                ),
                trailing: current == a.id
                    ? Icon(Icons.check, color: Fleet.live)
                    : null,
                onTap: () => Navigator.pop(ctx, a.id),
              ),
          ],
        ),
      ),
    ),
  );
}
