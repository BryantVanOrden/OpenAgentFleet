import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/agent_kind_badge.dart';
import '../../core/widgets/sheet_messenger.dart';
import '../instance_view/instance_screen.dart';
import '../work/ticket_detail_screen.dart';
import '../work/work_screen.dart';
import 'org_layout.dart';

/// An agent's place in the org: its title, what it is useful for, who it
/// reports to, how far it is trusted, and -- for an admin -- its budget.
class AgentProfileSheet extends ConsumerStatefulWidget {
  const AgentProfileSheet({
    super.key,
    required this.agentId,
    this.showOpenAgent = true,
  });

  final String agentId;

  /// Off when the sheet is opened from the agent's own screen.
  final bool showOpenAgent;

  static Future<void> show(BuildContext context, String agentId,
          {bool showOpenAgent = true}) =>
      showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        backgroundColor: Fleet.ink900,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
        ),
        builder: (_) => SheetMessenger(
          child: AgentProfileSheet(
              agentId: agentId, showOpenAgent: showOpenAgent),
        ),
      );

  @override
  ConsumerState<AgentProfileSheet> createState() => _AgentProfileSheetState();
}

class _AgentProfileSheetState extends ConsumerState<AgentProfileSheet> {
  final _title = TextEditingController();
  final _capabilities = TextEditingController();
  final _budget = TextEditingController();
  final _warn = TextEditingController();
  String _reportsTo = '';
  String _trust = 'standard';

  /// What the fields were filled from, so Save sends only what changed: a
  /// non-admin editing a title must not send a budget the server refuses.
  ({
    String title,
    String capabilities,
    String reportsTo,
    String trust,
    double budget,
    int warn,
  })? _initial;

  bool _busy = false;

  @override
  void dispose() {
    _title.dispose();
    _capabilities.dispose();
    _budget.dispose();
    _warn.dispose();
    super.dispose();
  }

  void _fill(Instance? inst, OrgNode? node) {
    if (_initial != null) return;
    final title = inst?.title ?? node?.title ?? '';
    final caps = inst?.capabilities ?? node?.capabilities ?? '';
    // The chart's reports_to is blank when the manager is hidden from you;
    // the instance's is the real one. Prefer the one the chart draws, so the
    // picker never shows a manager it has no row for.
    final reports = node?.reportsTo ?? inst?.reportsTo ?? '';
    final trust = inst?.trust ?? node?.trust ?? 'standard';
    final budget = inst?.budgetMonthUsd ?? node?.budgetMonthUsd ?? 0;
    final warn = inst?.budgetWarnPct ?? 0;
    _title.text = title;
    _capabilities.text = caps;
    _reportsTo = reports;
    _trust = trust;
    _budget.text = budget > 0 ? budget.toStringAsFixed(2) : '';
    _warn.text = warn > 0 ? '$warn' : '';
    _initial = (
      title: title,
      capabilities: caps,
      reportsTo: reports,
      trust: trust,
      budget: budget,
      warn: warn,
    );
  }

  Future<void> _save(bool isAdmin) async {
    final init = _initial;
    if (init == null) return;
    final messenger = ScaffoldMessenger.of(context);
    final navigator = Navigator.of(context);

    final title = _title.text.trim();
    final caps = _capabilities.text.trim();
    final budgetText = _budget.text.trim();
    final warnText = _warn.text.trim();
    final budget = budgetText.isEmpty ? 0.0 : double.tryParse(budgetText);
    final warn = warnText.isEmpty ? 0 : int.tryParse(warnText);
    if (isAdmin && (budget == null || budget < 0)) {
      messenger.showSnackBar(const SnackBar(
          content: Text('The budget is an amount in dollars, or blank for none.')));
      return;
    }
    if (isAdmin && (warn == null || warn < 0 || warn > 100)) {
      messenger.showSnackBar(const SnackBar(
          content: Text('The warning is a percentage between 1 and 100.')));
      return;
    }

    setState(() => _busy = true);
    try {
      await ref.read(apiProvider).setInstanceProfile(
            widget.agentId,
            title: title != init.title ? title : null,
            capabilities: caps != init.capabilities ? caps : null,
            reportsTo: _reportsTo != init.reportsTo ? _reportsTo : null,
            trust: _trust != init.trust ? _trust : null,
            budgetMonthUsd:
                isAdmin && budget != init.budget ? budget : null,
            // 0 means "server default" and is not a value the server takes.
            budgetWarnPct:
                isAdmin && warn != init.warn && warn! > 0 ? warn : null,
          );
      ref.invalidate(orgProvider);
      ref.invalidate(instancesProvider);
      if (mounted) navigator.pop();
    } catch (err) {
      // A loop in the reporting line, a budget from a non-admin: the server
      // says which in a sentence, and this sheet has its own messenger so it
      // shows above the sheet rather than behind it.
      messenger.showSnackBar(SnackBar(
        content: Text('$err'),
        backgroundColor: Fleet.ink800,
      ));
      ref.invalidate(orgProvider);
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final chart = ref.watch(orgProvider).valueOrNull;
    final instances =
        ref.watch(instancesProvider).valueOrNull ?? const <Instance>[];
    final isAdmin = ref.watch(meProvider).valueOrNull?.isAdmin ?? false;
    final node =
        chart?.nodes.where((n) => n.id == widget.agentId).firstOrNull;
    final inst = instances.where((i) => i.id == widget.agentId).firstOrNull;

    if (node == null && inst == null) {
      return const Center(child: CircularProgressIndicator());
    }
    _fill(inst, node);

    final name = node?.name ?? inst?.name ?? '';
    final kind = node?.kind ?? inst?.kind ?? AgentKind.desktop;
    final nodes = chart?.nodes ?? const <OrgNode>[];
    final inputs = [
      for (final n in nodes)
        OrgLayoutInput(id: n.id, parentId: n.reportsTo, sortKey: n.name),
    ];
    final under = reportsUnder(inputs, widget.agentId);
    final managers = nodes.where((n) => n.id != widget.agentId).toList()
      ..sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
    // A manager the chart does not have (hidden from you, or just deleted)
    // still has to be a valid dropdown value, or the dropdown asserts -- and
    // quietly swapping it for "You" would move the agent on the next Save.
    final unknownManager = _reportsTo.isNotEmpty &&
        !managers.any((m) => m.id == _reportsTo);
    final inset = MediaQuery.viewInsetsOf(context).bottom;
    final status = node == null
        ? null
        : orgStatus(
            online: node.online,
            busy: node.busy,
            hold: node.hold,
            ticketRef: node.ticketRef);

    return ListView(
      padding: EdgeInsets.fromLTRB(20, 12, 20, 24 + inset),
      children: [
        Center(
          child: Container(
            width: 36,
            height: 4,
            decoration: BoxDecoration(
              color: Fleet.ink600,
              borderRadius: BorderRadius.circular(999),
            ),
          ),
        ),
        const SizedBox(height: 14),
        Row(
          children: [
            Expanded(
              child: Text(name,
                  overflow: TextOverflow.ellipsis,
                  style: Theme.of(context).textTheme.titleLarge),
            ),
            AgentKindBadge(kind),
          ],
        ),
        if (status != null) ...[
          const SizedBox(height: 6),
          Wrap(
            spacing: 8,
            runSpacing: 4,
            crossAxisAlignment: WrapCrossAlignment.center,
            children: [
              Text(status.label,
                  style: TextStyle(color: Fleet.ink300, fontSize: 12.5)),
              if (node!.busy && node.ticketRef.isNotEmpty)
                ActionChip(
                  visualDensity: VisualDensity.compact,
                  avatar: const Icon(Icons.open_in_new, size: 14),
                  label: Text(node.ticketTitle.isEmpty
                      ? node.ticketRef
                      : '${node.ticketRef} · ${node.ticketTitle}'),
                  onPressed: () => Navigator.of(context).push(MaterialPageRoute(
                    builder: (_) => TicketDetailScreen(idOrRef: node.ticketRef),
                  )),
                ),
              if (node.openTickets > 0)
                ActionChip(
                  visualDensity: VisualDensity.compact,
                  avatar: const Icon(Icons.confirmation_number_outlined,
                      size: 14),
                  label: Text('${node.openTickets} open tickets'),
                  onPressed: () => Navigator.of(context).push(MaterialPageRoute(
                    builder: (_) => WorkScreen(initialAssignee: node.id),
                  )),
                ),
            ],
          ),
        ],
        if (inst != null && inst.isExternal) ...[
          const SizedBox(height: 6),
          Text(
              inst.connection.summary(
                  deviceName: AgentKind.onDevice(inst.kind)
                      ? (ref
                              .watch(oafDevicesProvider)
                              .valueOrNull
                              ?.where((d) => d.id == inst.connection.deviceId)
                              .firstOrNull
                              ?.name ??
                          '')
                      : ''),
              maxLines: 3,
              overflow: TextOverflow.ellipsis,
              style: TextStyle(
                  color: Fleet.ink400, fontSize: 11.5, fontFamily: 'monospace')),
        ],
        const SizedBox(height: 18),
        TextField(
          controller: _title,
          enabled: !_busy,
          textCapitalization: TextCapitalization.words,
          decoration: const InputDecoration(
            labelText: 'Title',
            hintText: 'e.g. Engineer, Reviewer, Research lead',
          ),
        ),
        const SizedBox(height: 14),
        TextField(
          controller: _capabilities,
          enabled: !_busy,
          minLines: 3,
          maxLines: 6,
          textCapitalization: TextCapitalization.sentences,
          decoration: const InputDecoration(
            labelText: "When I'm useful",
            alignLabelWithHint: true,
            hintText: 'What colleagues should ask this agent for. They read '
                'this when deciding who to hand work to.',
          ),
        ),
        const SizedBox(height: 14),
        DropdownButtonFormField<String>(
          // Rebuilt when the set of agents changes, so the field never holds
          // a value its items no longer contain.
          key: ValueKey('reports:${managers.map((m) => m.id).join(',')}'),
          initialValue: _reportsTo,
          isExpanded: true,
          decoration: const InputDecoration(
            labelText: 'Reports to',
            helperText: 'Blocked work goes up this line to be unblocked.',
          ),
          items: [
            const DropdownMenuItem(value: '', child: Text('You')),
            if (unknownManager)
              DropdownMenuItem(
                value: _reportsTo,
                child: const Text('An agent you cannot see'),
              ),
            for (final m in managers)
              DropdownMenuItem(
                value: m.id,
                child: Text(
                  under.contains(m.id)
                      ? '${m.name}  (reports to $name)'
                      : m.title.isEmpty
                          ? m.name
                          : '${m.name} · ${m.title}',
                  overflow: TextOverflow.ellipsis,
                ),
              ),
          ],
          onChanged:
              _busy ? null : (v) => setState(() => _reportsTo = v ?? ''),
        ),
        const SizedBox(height: 18),
        Text('Trust',
            style: TextStyle(
                color: Fleet.ink300,
                fontSize: 12,
                fontWeight: FontWeight.w600)),
        const SizedBox(height: 8),
        SegmentedButton<String>(
          segments: const [
            ButtonSegment(
                value: 'standard',
                label: Text('Standard'),
                icon: Icon(Icons.verified_user_outlined)),
            ButtonSegment(
                value: 'low',
                label: Text('Low'),
                icon: Icon(Icons.shield_outlined)),
          ],
          selected: {_trust},
          onSelectionChanged:
              _busy ? null : (s) => setState(() => _trust = s.first),
        ),
        const SizedBox(height: 6),
        Text(
          _trust == 'low'
              ? 'For an agent that reads hostile input -- web pages, outside '
                  'tickets, untrusted code. What it writes reaches colleagues '
                  'fenced as data, and it cannot hand work to others, reopen '
                  'work, or write to the shared vault.'
              : 'Its messages, results and hand-offs reach colleagues as '
                  'ordinary instructions.',
          style: TextStyle(color: Fleet.ink400, fontSize: 11.5, height: 1.4),
        ),
        const SizedBox(height: 18),
        Text('Monthly budget',
            style: TextStyle(
                color: Fleet.ink300,
                fontSize: 12,
                fontWeight: FontWeight.w600)),
        const SizedBox(height: 8),
        if (isAdmin)
          Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Expanded(
                flex: 3,
                child: TextField(
                  controller: _budget,
                  enabled: !_busy,
                  keyboardType:
                      const TextInputType.numberWithOptions(decimal: true),
                  decoration: const InputDecoration(
                    labelText: 'Ceiling',
                    prefixText: '\$ ',
                    hintText: 'none',
                    helperText: 'Blank for no ceiling',
                  ),
                ),
              ),
              const SizedBox(width: 10),
              Expanded(
                flex: 2,
                child: TextField(
                  controller: _warn,
                  enabled: !_busy,
                  keyboardType: TextInputType.number,
                  decoration: const InputDecoration(
                    labelText: 'Warn at',
                    suffixText: '%',
                    hintText: '80',
                  ),
                ),
              ),
            ],
          )
        else
          Text(
            (node?.budgetMonthUsd ?? inst?.budgetMonthUsd ?? 0) > 0
                ? '\$${(node?.budgetMonthUsd ?? inst!.budgetMonthUsd).toStringAsFixed(2)} a month. Budgets are set by an admin.'
                : 'No ceiling. Budgets are set by an admin.',
            style: TextStyle(color: Fleet.ink400, fontSize: 12),
          ),
        if (node != null && node.hasBudget) ...[
          const SizedBox(height: 8),
          Text(
            'Spent \$${node.spendMonthUsd.toStringAsFixed(2)} so far this month.'
            '${node.hold == 'budget' ? ' Held: its runs are stopped until the ceiling is raised or the month turns.' : ''}',
            style: TextStyle(
                color: node.hold == 'budget' ? Fleet.warn : Fleet.ink400,
                fontSize: 11.5),
          ),
        ],
        const SizedBox(height: 24),
        Row(
          children: [
            if (widget.showOpenAgent)
              Expanded(
                child: OutlinedButton.icon(
                  onPressed: _busy
                      ? null
                      : () {
                          final navigator = Navigator.of(context);
                          navigator.pop();
                          navigator.push(MaterialPageRoute(
                            builder: (_) =>
                                InstanceScreen(instanceId: widget.agentId),
                          ));
                        },
                  icon: const Icon(Icons.open_in_new, size: 18),
                  label: const Text('Open agent'),
                ),
              ),
            if (widget.showOpenAgent) const SizedBox(width: 10),
            Expanded(
              child: FilledButton(
                onPressed: _busy ? null : () => _save(isAdmin),
                child: _busy
                    ? const SizedBox(
                        width: 18,
                        height: 18,
                        child: CircularProgressIndicator(strokeWidth: 2))
                    : const Text('Save'),
              ),
            ),
          ],
        ),
      ],
    );
  }
}
