import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import 'new_ticket_sheet.dart';
import 'ticket_detail_screen.dart';
import 'ticket_widgets.dart';
import 'work_logic.dart';

/// Every ticket in the fleet, by where it is in its life.
///
/// A request you make in the chat becomes a root ticket with a ticket per
/// part under it; this is where you see them move. Live: the socket's
/// `ticket` events refresh it, and so does coming back to it.
class WorkScreen extends ConsumerStatefulWidget {
  const WorkScreen({super.key, this.initialAssignee = ''});

  /// Opens already filtered to one agent's tickets.
  final String initialAssignee;

  @override
  ConsumerState<WorkScreen> createState() => _WorkScreenState();
}

class _WorkScreenState extends ConsumerState<WorkScreen>
    with SingleTickerProviderStateMixin, WidgetsBindingObserver {
  late final TabController _tabs =
      TabController(length: workTabs.length, vsync: this);
  late String _assignee = widget.initialAssignee;
  bool _rootsOnly = false;

  TicketQuery get _query => (assignee: _assignee, rootsOnly: _rootsOnly);

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _tabs.dispose();
    super.dispose();
  }

  /// The socket drops while the app is in the background; what changed then
  /// arrived as events nobody heard.
  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) {
      ref.invalidate(ticketsProvider(_query));
    }
  }

  Future<void> _open(Ticket t) async {
    await Navigator.of(context).push(MaterialPageRoute(
      builder: (_) => TicketDetailScreen(idOrRef: t.id, initial: t),
    ));
    if (mounted) ref.invalidate(ticketsProvider(_query));
  }

  Future<void> _newTicket() async {
    final created = await NewTicketSheet.show(context);
    if (created == null || !mounted) return;
    ref.invalidate(ticketsProvider(_query));
    await _open(created);
  }

  @override
  Widget build(BuildContext context) {
    final tickets = ref.watch(ticketsProvider(_query));
    final instances =
        ref.watch(instancesProvider).valueOrNull ?? const <Instance>[];
    final grouped = groupTickets(tickets.valueOrNull ?? const []);

    return Scaffold(
      appBar: AppBar(
        title: const Text('Work'),
        actions: [
          IconButton(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: () => ref.invalidate(ticketsProvider(_query)),
          ),
        ],
        bottom: TabBar(
          controller: _tabs,
          isScrollable: true,
          tabAlignment: TabAlignment.start,
          indicatorColor: Fleet.live,
          labelColor: Fleet.ink100,
          unselectedLabelColor: Fleet.ink400,
          tabs: [
            for (final tab in workTabs)
              Tab(
                child: Row(
                  mainAxisSize: MainAxisSize.min,
                  children: [
                    Text(tab.label),
                    if (grouped[tab.key]!.isNotEmpty) ...[
                      const SizedBox(width: 6),
                      _Count(grouped[tab.key]!.length,
                          alert: tab.key == 'blocked'),
                    ],
                  ],
                ),
              ),
          ],
        ),
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _newTicket,
        icon: const Icon(Icons.add),
        label: const Text('New ticket'),
      ),
      body: Column(
        children: [
          _filters(instances),
          Expanded(
            child: tickets.when(
              skipLoadingOnRefresh: true,
              loading: () => const Center(child: CircularProgressIndicator()),
              error: (err, _) => _Empty(
                icon: Icons.cloud_off_outlined,
                title: 'Could not load tickets',
                body: '$err',
                onRetry: () => ref.invalidate(ticketsProvider(_query)),
              ),
              data: (_) => TabBarView(
                controller: _tabs,
                children: [
                  for (final tab in workTabs)
                    _TicketList(
                      tab: tab,
                      tickets: grouped[tab.key]!,
                      filtered: _assignee.isNotEmpty || _rootsOnly,
                      onRefresh: () =>
                          ref.refresh(ticketsProvider(_query).future),
                      onOpen: _open,
                    ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }

  Widget _filters(List<Instance> instances) {
    final who = instances.where((i) => i.id == _assignee).firstOrNull;
    return Container(
      alignment: Alignment.centerLeft,
      padding: const EdgeInsets.fromLTRB(12, 8, 12, 4),
      child: SingleChildScrollView(
        scrollDirection: Axis.horizontal,
        child: Row(
          children: [
            InputChip(
              avatar: Icon(Icons.person_outline, size: 16, color: Fleet.ink300),
              label: Text(_assignee.isEmpty
                  ? 'Anyone'
                  : (who?.name ?? 'One agent')),
              selected: _assignee.isNotEmpty,
              showCheckmark: false,
              onPressed: () async {
                final picked = await pickAgent(
                  context,
                  title: 'Show tickets assigned to',
                  agents: instances,
                  current: _assignee,
                  allowNobody: false,
                );
                if (picked != null && mounted) {
                  setState(() => _assignee = picked);
                }
              },
              onDeleted: _assignee.isEmpty
                  ? null
                  : () => setState(() => _assignee = ''),
            ),
            const SizedBox(width: 8),
            FilterChip(
              label: const Text('Top level only'),
              selected: _rootsOnly,
              onSelected: (v) => setState(() => _rootsOnly = v),
            ),
          ],
        ),
      ),
    );
  }
}

class _Count extends StatelessWidget {
  const _Count(this.n, {this.alert = false});
  final int n;
  final bool alert;

  @override
  Widget build(BuildContext context) {
    final color = alert ? Fleet.warn : Fleet.ink400;
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 1),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.16),
        borderRadius: BorderRadius.circular(999),
      ),
      child: Text('$n',
          style: TextStyle(
              fontSize: 11, color: alert ? Fleet.warn : Fleet.ink200)),
    );
  }
}

class _TicketList extends StatelessWidget {
  const _TicketList({
    required this.tab,
    required this.tickets,
    required this.filtered,
    required this.onRefresh,
    required this.onOpen,
  });

  final WorkTab tab;
  final List<Ticket> tickets;
  final bool filtered;
  final Future<void> Function() onRefresh;
  final void Function(Ticket) onOpen;

  String get _emptyText => filtered
      ? 'Nothing here with these filters.'
      : switch (tab.key) {
          'todo' =>
            'Nothing waiting to start. Ask the fleet for something in the '
                'chat, or add a ticket.',
          'in_progress' => 'No agent is working on a ticket right now.',
          'in_review' => 'Nothing is waiting for a reviewer.',
          'blocked' => 'Nothing is stuck.',
          'done' => 'Nothing finished yet.',
          'backlog' =>
            'The backlog is empty. Tickets land here when they wait on '
                'something that has not been arranged yet.',
          _ => 'Nothing here.',
        };

  @override
  Widget build(BuildContext context) {
    return RefreshIndicator(
      onRefresh: onRefresh,
      child: tickets.isEmpty
          ? ListView(
              physics: const AlwaysScrollableScrollPhysics(),
              children: [
                const SizedBox(height: 80),
                Icon(
                  tab.key == 'blocked'
                      ? Icons.check_circle_outline
                      : Icons.inbox_outlined,
                  size: 42,
                  color: Fleet.ink500,
                ),
                const SizedBox(height: 12),
                Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 40),
                  child: Text(_emptyText,
                      textAlign: TextAlign.center,
                      style: TextStyle(color: Fleet.ink400, fontSize: 13)),
                ),
              ],
            )
          : Center(
              child: ConstrainedBox(
                // On a tablet a full-width row is a long way to read across.
                constraints: const BoxConstraints(maxWidth: 820),
                child: ListView.builder(
                  physics: const AlwaysScrollableScrollPhysics(),
                  padding: const EdgeInsets.fromLTRB(12, 8, 12, 96),
                  itemCount: tickets.length,
                  itemBuilder: (_, i) => TicketRow(
                    ticket: tickets[i],
                    showStatus: tab.key == 'done',
                    onTap: () => onOpen(tickets[i]),
                  ),
                ),
              ),
            ),
    );
  }
}

class _Empty extends StatelessWidget {
  const _Empty({
    required this.icon,
    required this.title,
    required this.body,
    required this.onRetry,
  });

  final IconData icon;
  final String title;
  final String body;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 44, color: Fleet.bad),
            const SizedBox(height: 12),
            Text(title, style: const TextStyle(fontSize: 16)),
            const SizedBox(height: 6),
            Text(body,
                textAlign: TextAlign.center,
                style: TextStyle(color: Fleet.ink400, fontSize: 13)),
            const SizedBox(height: 16),
            OutlinedButton(onPressed: onRetry, child: const Text('Retry')),
          ],
        ),
      ),
    );
  }
}
