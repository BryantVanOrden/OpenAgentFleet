import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/markdown/markdown_lite.dart';
import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../instance_view/task_detail_screen.dart';
import 'new_ticket_sheet.dart';
import 'ticket_widgets.dart';
import 'work_logic.dart';

/// One ticket: why it exists, who has it, what it waits on, what came of it,
/// and the thread where agents, the engine and you talk about it.
class TicketDetailScreen extends ConsumerStatefulWidget {
  const TicketDetailScreen({super.key, required this.idOrRef, this.initial});

  /// The ticket's id or its reference ("T-12").
  final String idOrRef;

  /// The row it was opened from, shown while the full page loads.
  final Ticket? initial;

  @override
  ConsumerState<TicketDetailScreen> createState() =>
      _TicketDetailScreenState();
}

class _TicketDetailScreenState extends ConsumerState<TicketDetailScreen>
    with WidgetsBindingObserver {
  final _composer = TextEditingController();
  bool _sending = false;
  bool _mutating = false;

  /// A long description -- the whole brief an agent was handed -- starts
  /// folded, so the ticket's state and people are on the first screen.
  bool _descriptionOpen = false;

  @override
  void initState() {
    super.initState();
    WidgetsBinding.instance.addObserver(this);
  }

  @override
  void dispose() {
    WidgetsBinding.instance.removeObserver(this);
    _composer.dispose();
    super.dispose();
  }

  @override
  void didChangeAppLifecycleState(AppLifecycleState state) {
    if (state == AppLifecycleState.resumed) _reload();
  }

  void _reload() => ref.invalidate(ticketDetailProvider(widget.idOrRef));

  void _open(String idOrRef) {
    Navigator.of(context)
        .push(MaterialPageRoute(
          builder: (_) => TicketDetailScreen(idOrRef: idOrRef),
        ))
        .then((_) {
      if (mounted) _reload();
    });
  }

  Future<void> _mutate(Future<void> Function() action) async {
    if (_mutating) return;
    final messenger = ScaffoldMessenger.of(context);
    setState(() => _mutating = true);
    try {
      await action();
      _reload();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    } finally {
      if (mounted) setState(() => _mutating = false);
    }
  }

  Future<void> _send(Ticket t) async {
    final body = _composer.text.trim();
    if (body.isEmpty || _sending) return;
    final messenger = ScaffoldMessenger.of(context);
    setState(() => _sending = true);
    try {
      await ref.read(apiProvider).commentTicket(t.id, body);
      _composer.clear();
      _reload();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    } finally {
      if (mounted) setState(() => _sending = false);
    }
  }

  Future<void> _reopen(Ticket t) async {
    final children =
        ref.read(ticketDetailProvider(widget.idOrRef)).valueOrNull?.children ??
            const <Ticket>[];
    final parts = reopenedParts(t, children);
    final String explain;
    if (!t.isUnassigned) {
      final who = t.assigneeName.isEmpty ? 'its assignee' : t.assigneeName;
      explain = 'It goes back to $who with what you write here, so say what '
          'is missing or wrong.';
    } else if (parts.isNotEmpty) {
      final who = [
        for (final p in parts)
          p.assigneeName.isEmpty ? p.ref : '${p.ref} (${p.assigneeName})',
      ].join(', ');
      explain = 'Nobody is assigned ${t.ref}, so its finished work goes back '
          'instead: $who. Each gets what you write here, and ${t.ref} waits '
          'for them again.';
    } else {
      explain = 'Nobody is assigned ${t.ref} and it has no finished work under '
          'it, so it goes back to To do with nobody to start it. Assign it '
          'to an agent afterwards.';
    }
    final controller = TextEditingController();
    final reason = await showDialog<String>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setLocal) => AlertDialog(
          title: Text('Reopen ${t.ref}?'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(
                explain,
                style:
                    TextStyle(color: Fleet.ink300, fontSize: 13, height: 1.4),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: controller,
                autofocus: true,
                minLines: 2,
                maxLines: 5,
                textCapitalization: TextCapitalization.sentences,
                onChanged: (_) => setLocal(() {}),
                decoration: const InputDecoration(
                  hintText: 'e.g. The tests were never run on Windows.',
                ),
              ),
            ],
          ),
          actions: [
            TextButton(
                onPressed: () => Navigator.pop(ctx),
                child: const Text('Cancel')),
            FilledButton(
              onPressed: controller.text.trim().isEmpty
                  ? null
                  : () => Navigator.pop(ctx, controller.text.trim()),
              child: Text(parts.length > 1
                  ? 'Reopen ${parts.length} parts'
                  : parts.length == 1
                      ? 'Reopen ${parts.first.ref}'
                      : 'Reopen'),
            ),
          ],
        ),
      ),
    );
    controller.dispose();
    if (reason == null || reason.isEmpty || !mounted) return;
    await _mutate(() => ref.read(apiProvider).reopenTicket(t.id, reason));
  }

  Future<void> _delete(Ticket t) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Delete ${t.ref}?'),
        content: Text(
          '"${t.title}" and its thread are removed. A run working on it is '
          'stopped. This cannot be undone.',
          style: TextStyle(color: Fleet.ink300),
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Fleet.bad),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    final messenger = ScaffoldMessenger.of(context);
    final navigator = Navigator.of(context);
    try {
      await ref.read(apiProvider).deleteTicket(t.id);
      navigator.pop();
      messenger.showSnackBar(SnackBar(content: Text('${t.ref} deleted')));
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  /// Cancelling takes the ticket's open parts, and any open review or
  /// verify of them, with it. Say so, and how many, before it happens; a
  /// ticket with nothing open under it is cancelled without asking.
  Future<bool> _confirmCancel(Ticket t) async {
    List<Ticket>? cascade;
    try {
      cascade = cancelCascade(t.id, await ref.read(apiProvider).tickets());
    } catch (_) {
      // Unknown is not none: ask, without a number.
      cascade = null;
    }
    if (!mounted) return false;
    if (cascade != null && cascade.isEmpty) return true;
    return await _askCancel(t, cascade) ?? false;
  }

  Future<bool?> _askCancel(Ticket t, List<Ticket>? cascade) {
    final what = cascade == null ? '' : describeCascade(cascade);
    return showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Cancel ${t.ref}?'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              cascade == null
                  ? 'Its open parts and any open review or verify of them are '
                      'cancelled with it, and their runs stopped.'
                  : 'Its $what ${cascade.length == 1 ? 'is' : 'are'} '
                      'cancelled with it, and any run on them stopped. '
                      'Finished parts stay finished.',
              style: TextStyle(color: Fleet.ink300, fontSize: 13, height: 1.4),
            ),
            if (cascade != null) ...[
              const SizedBox(height: 10),
              for (final c in cascade.take(6))
                Padding(
                  padding: const EdgeInsets.only(bottom: 4),
                  child: Text.rich(
                    TextSpan(children: [
                      TextSpan(
                        text: '${c.ref}  ',
                        style: TextStyle(
                            color: Fleet.ink300,
                            fontFamily: 'monospace',
                            fontSize: 12),
                      ),
                      TextSpan(text: plainTitle(c.title)),
                    ]),
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(color: Fleet.ink200, fontSize: 12.5),
                  ),
                ),
              if (cascade.length > 6)
                Text('and ${cascade.length - 6} more',
                    style: TextStyle(color: Fleet.ink300, fontSize: 12)),
            ],
          ],
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Keep it')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Fleet.bad),
            onPressed: () => Navigator.pop(ctx, true),
            child: Text(cascade == null || cascade.isEmpty
                ? 'Cancel ticket'
                : 'Cancel ${cascade.length + 1} tickets'),
          ),
        ],
      ),
    );
  }

  Future<void> _cancel(Ticket t) async {
    if (!await _confirmCancel(t) || !mounted) return;
    await _mutate(
        () => ref.read(apiProvider).patchTicket(t.id, status: 'cancelled'));
  }

  Future<void> _changeStatus(Ticket t) async {
    final picked = await showModalBottomSheet<String>(
      context: context,
      backgroundColor: Fleet.ink900,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
      ),
      builder: (ctx) => SafeArea(
        child: ListView(
          shrinkWrap: true,
          padding: const EdgeInsets.symmetric(vertical: 12),
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 4, 20, 8),
              child: Text('Move ${t.ref} to',
                  style: Theme.of(ctx).textTheme.titleMedium),
            ),
            // In progress is what a run taking the ticket does; the server
            // refuses it from here, so it is not offered.
            for (final s in Ticket.statuses)
              if (s != 'in_progress' || s == t.status)
                ListTile(
                  leading:
                      Icon(Icons.circle, size: 12, color: ticketStatusColor(s)),
                  title: Text(Ticket.statusLabel(s)),
                  subtitle: s == 'cancelled' && t.status != 'cancelled'
                      ? Text('Also cancels its open parts and checks',
                          style: TextStyle(color: Fleet.ink300, fontSize: 12))
                      : null,
                  trailing: s == t.status
                      ? Icon(Icons.check, color: Fleet.live)
                      : null,
                  onTap: s == t.status
                      ? () => Navigator.pop(ctx)
                      : () => Navigator.pop(ctx, s),
                ),
          ],
        ),
      ),
    );
    if (picked == null || picked == t.status || !mounted) return;
    if (picked == 'cancelled') return _cancel(t);
    await _mutate(
        () => ref.read(apiProvider).patchTicket(t.id, status: picked));
  }

  Future<void> _changePerson(Ticket t, String role) async {
    final instances =
        ref.read(instancesProvider).valueOrNull ?? const <Instance>[];
    final current = switch (role) {
      'reviewer' => t.reviewerId,
      'verifier' => t.verifierId,
      _ => t.assigneeId,
    };
    final picked = await pickAgent(
      context,
      title: switch (role) {
        'reviewer' => 'Who reviews ${t.ref}',
        'verifier' => 'Who verifies ${t.ref}',
        _ => 'Who works on ${t.ref}',
      },
      agents: instances,
      current: current,
    );
    if (picked == null || picked == current || !mounted) return;
    final api = ref.read(apiProvider);
    await _mutate(() => switch (role) {
          'reviewer' => api.patchTicket(t.id, reviewerId: picked),
          'verifier' => api.patchTicket(t.id, verifierId: picked),
          _ => api.patchTicket(t.id, assigneeId: picked),
        });
  }

  void _openFile(String name) =>
      openCatalogItem(context, ref.read(apiProvider), name);

  Future<void> _addChild(Ticket t) async {
    final created = await NewTicketSheet.show(context, parent: t);
    if (created == null || !mounted) return;
    _reload();
    _open(created.id);
  }

  @override
  Widget build(BuildContext context) {
    final detail = ref.watch(ticketDetailProvider(widget.idOrRef));
    final d = detail.valueOrNull;
    final t = d?.ticket ?? widget.initial;

    return Scaffold(
      appBar: AppBar(
        title: Text(t == null ? widget.idOrRef : t.ref,
            style: const TextStyle(fontFamily: 'monospace')),
        actions: [
          // Reopening is for work that stopped: finished, dropped, waiting on
          // a reviewer, or stuck. Work still running or not yet started has
          // nothing to reopen.
          if (t != null &&
              const {'done', 'cancelled', 'in_review', 'blocked'}
                  .contains(t.status))
            IconButton(
              tooltip: 'Reopen',
              icon: const Icon(Icons.replay_rounded),
              onPressed: _mutating ? null : () => _reopen(t),
            ),
          if (t != null)
            PopupMenuButton<String>(
              color: Fleet.ink850,
              tooltip: 'More',
              onSelected: (v) {
                if (v == 'child') {
                  _addChild(t);
                } else if (v == 'cancel') {
                  _cancel(t);
                } else if (v == 'delete') {
                  _delete(t);
                } else {
                  _reload();
                }
              },
              itemBuilder: (_) => [
                const PopupMenuItem(
                  value: 'child',
                  child: ListTile(
                    dense: true,
                    leading: Icon(Icons.subdirectory_arrow_right),
                    title: Text('Add a ticket under this'),
                  ),
                ),
                const PopupMenuItem(
                  value: 'refresh',
                  child: ListTile(
                    dense: true,
                    leading: Icon(Icons.refresh),
                    title: Text('Refresh'),
                  ),
                ),
                if (t.isOpen)
                  const PopupMenuItem(
                    value: 'cancel',
                    child: ListTile(
                      dense: true,
                      leading: Icon(Icons.cancel_outlined),
                      title: Text('Cancel ticket'),
                    ),
                  ),
                const PopupMenuDivider(),
                PopupMenuItem(
                  value: 'delete',
                  child: ListTile(
                    dense: true,
                    leading: Icon(Icons.delete_outline, color: Fleet.bad),
                    title: Text('Delete', style: TextStyle(color: Fleet.bad)),
                  ),
                ),
              ],
            ),
        ],
      ),
      body: d == null
          ? detail.hasError
              ? _LoadError(
                  message: '${detail.error}',
                  onRetry: _reload,
                )
              : const Center(child: CircularProgressIndicator())
          : LayoutBuilder(builder: (context, c) {
              final wide = c.maxWidth >= 900;
              final details = _details(d);
              final thread = _thread(d);
              if (wide) {
                return Row(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Expanded(
                      flex: 5,
                      child: ListView(
                        padding: const EdgeInsets.fromLTRB(20, 12, 20, 32),
                        children: details,
                      ),
                    ),
                    VerticalDivider(width: 1, color: Fleet.ink800),
                    Expanded(
                      flex: 4,
                      child: Column(
                        children: [
                          Expanded(
                            child: ListView(
                              padding: const EdgeInsets.fromLTRB(16, 12, 16, 16),
                              children: thread,
                            ),
                          ),
                          _composerBar(d.ticket),
                        ],
                      ),
                    ),
                  ],
                );
              }
              return Column(
                children: [
                  Expanded(
                    child: RefreshIndicator(
                      onRefresh: () => ref
                          .refresh(ticketDetailProvider(widget.idOrRef).future),
                      child: ListView(
                        physics: const AlwaysScrollableScrollPhysics(),
                        padding: const EdgeInsets.fromLTRB(16, 8, 16, 16),
                        children: [...details, const SizedBox(height: 8), ...thread],
                      ),
                    ),
                  ),
                  _composerBar(d.ticket),
                ],
              );
            }),
    );
  }

  List<Widget> _details(TicketDetail d) {
    final t = d.ticket;
    return [
      // Only when there is something to say: a request's own words are its
      // title, and a card with nothing under its heading is noise.
      if (d.ancestry.isNotEmpty ||
          (t.origin.isNotEmpty && t.origin != t.title))
        _whyItMatters(d),
      const SizedBox(height: 10),
      Wrap(
        spacing: 8,
        runSpacing: 6,
        crossAxisAlignment: WrapCrossAlignment.center,
        children: [
          ActionChip(
            avatar: Icon(Icons.circle, size: 10, color: ticketStatusColor(t.status)),
            label: Text(Ticket.statusLabel(t.status)),
            onPressed: _mutating ? null : () => _changeStatus(t),
            tooltip: 'Change status',
          ),
          if (t.isMeta) TicketKindPill(t.kind),
          if (t.verdict == 'pass' || t.verdict == 'fail') VerdictPill(t.verdict),
          if (t.stage.isNotEmpty)
            TicketPill(label: t.stage, color: Fleet.ink300),
          if (t.costUsd > 0)
            TicketPill(
              label: t.budgetUsd > 0
                  ? '${formatCost(t.costUsd)} of ${formatCost(t.budgetUsd)}'
                  : formatCost(t.costUsd),
              color: Fleet.ink300,
              icon: Icons.payments_outlined,
            ),
          if (t.rounds > 1)
            TicketPill(label: 'round ${t.rounds}', color: Fleet.cool),
          if (t.attempts > 0)
            TicketPill(
                label: 'retried ${t.attempts}x',
                color: Fleet.warn,
                icon: Icons.replay),
        ],
      ),
      const SizedBox(height: 12),
      SelectableText(
        plainTitle(t.title),
        style: const TextStyle(fontSize: 20, fontWeight: FontWeight.w700),
      ),
      if (t.isMeta && t.targetId.isNotEmpty) ...[
        const SizedBox(height: 8),
        _targetLink(d),
      ],
      if (t.status == 'blocked' && t.blockedReason.isNotEmpty) ...[
        const SizedBox(height: 10),
        _Callout(
          icon: Icons.report_gmailerrorred_outlined,
          color: Fleet.warn,
          title: 'Blocked',
          body: t.blockedReason,
        ),
      ],
      if (t.description.trim().isNotEmpty) ...[
        const SizedBox(height: 12),
        _description(t.description),
      ],
      const SizedBox(height: 16),
      _people(t),
      if (t.result.trim().isNotEmpty) ...[
        const SizedBox(height: 16),
        Builder(builder: (context) {
          final report = splitReportFiles(t.result);
          return _Callout(
            icon: Icons.task_alt_rounded,
            color: t.verdict == 'fail' ? Fleet.bad : Fleet.good,
            title: 'Closing report',
            body: report.body,
            markdown: true,
            footer: report.hasFiles
                ? SharedFilesList(files: report, onOpen: _openFile)
                : null,
          );
        }),
      ],
      if (d.blockers.isNotEmpty) ...[
        _SectionTitle('Waits on', count: d.blockers.length),
        for (final b in d.blockers)
          TicketRow(ticket: b, showStatus: true, onTap: () => _open(b.id)),
      ],
      // A review or a verify is a check on other work; nothing goes under it
      // in the ordinary run of things, so an empty section is noise there.
      if (!t.isMeta || d.children.isNotEmpty) ...[
        _SectionTitle(
          'Under this',
          count: d.children.length,
          trailing: TextButton.icon(
            onPressed: () => _addChild(t),
            icon: const Icon(Icons.add, size: 16),
            label: const Text('Add'),
          ),
        ),
        if (d.children.isEmpty)
          Text('No tickets under this one.',
              style: TextStyle(color: Fleet.ink300, fontSize: 12.5))
        else
          for (final c in d.children)
            TicketRow(ticket: c, showStatus: true, onTap: () => _open(c.id)),
      ],
      if (d.dependents.isNotEmpty) ...[
        _SectionTitle('Waiting on this', count: d.dependents.length),
        for (final c in d.dependents)
          TicketRow(ticket: c, showStatus: true, onTap: () => _open(c.id)),
      ],
      if (d.runs.isNotEmpty) ...[
        _SectionTitle('Runs', count: d.runs.length),
        for (final r in d.runs) _RunTile(task: r),
      ],
    ];
  }

  Widget _description(String text) {
    final body = SelectionArea(
      child: MarkdownLite(text,
          baseStyle:
              TextStyle(color: Fleet.ink200, fontSize: 14, height: 1.45)),
    );
    // Short enough to read at a glance: no fold.
    if (text.length < 360 && '\n'.allMatches(text).length < 6) return body;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        AnimatedSize(
          duration: const Duration(milliseconds: 180),
          alignment: Alignment.topCenter,
          child: _descriptionOpen
              ? body
              : ShaderMask(
                  shaderCallback: (r) => LinearGradient(
                    begin: Alignment.topCenter,
                    end: Alignment.bottomCenter,
                    colors: const [Colors.white, Colors.transparent],
                    stops: const [0.55, 1],
                  ).createShader(r),
                  blendMode: BlendMode.dstIn,
                  child: ConstrainedBox(
                    constraints: const BoxConstraints(maxHeight: 118),
                    child: ClipRect(
                      child: SingleChildScrollView(
                        physics: const NeverScrollableScrollPhysics(),
                        child: body,
                      ),
                    ),
                  ),
                ),
        ),
        TextButton.icon(
          style: TextButton.styleFrom(
            minimumSize: const Size(0, 44),
            padding: const EdgeInsets.symmetric(horizontal: 4),
          ),
          onPressed: () =>
              setState(() => _descriptionOpen = !_descriptionOpen),
          icon: Icon(_descriptionOpen ? Icons.expand_less : Icons.expand_more,
              size: 18),
          label: Text(_descriptionOpen ? 'Show less' : 'Show the whole brief'),
        ),
      ],
    );
  }

  /// What a review, verify or unblock ticket is about.
  Widget _targetLink(TicketDetail d) {
    final t = d.ticket;
    final target = [...d.ancestry, ...d.blockers, ...d.dependents]
        .where((x) => x.id == t.targetId)
        .firstOrNull;
    final verb = switch (t.kind) {
      'review' => 'Reviews',
      'verify' => 'Verifies',
      'unblock' => 'Unblocks',
      _ => 'About',
    };
    return Align(
      alignment: Alignment.centerLeft,
      child: ActionChip(
        avatar: Icon(ticketKindIcon(t.kind), size: 16, color: Fleet.cool),
        label: Text(
          target == null
              ? '$verb another ticket'
              : '$verb ${target.ref} · ${plainTitle(target.title)}',
          overflow: TextOverflow.ellipsis,
        ),
        tooltip: 'Open the ticket this is about',
        onPressed: () => _open(t.targetId),
      ),
    );
  }

  Widget _whyItMatters(TicketDetail d) {
    final t = d.ticket;
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Fleet.ink850,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: Fleet.ink800),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.flag_outlined, size: 14, color: Fleet.live),
              const SizedBox(width: 6),
              Text('WHY THIS MATTERS',
                  style: TextStyle(
                      color: Fleet.ink300,
                      fontSize: 10.5,
                      letterSpacing: 0.6,
                      fontWeight: FontWeight.w700)),
            ],
          ),
          if (t.origin.isNotEmpty && t.origin != t.title) ...[
            const SizedBox(height: 8),
            Text('"${t.origin}"',
                maxLines: 4,
                overflow: TextOverflow.ellipsis,
                style: TextStyle(
                    color: Fleet.ink200,
                    fontSize: 13,
                    fontStyle: FontStyle.italic,
                    height: 1.4)),
          ],
          if (d.ancestry.isNotEmpty) ...[
            const SizedBox(height: 8),
            Wrap(
              crossAxisAlignment: WrapCrossAlignment.center,
              runSpacing: 4,
              children: [
                for (var i = 0; i < d.ancestry.length; i++) ...[
                  if (i > 0)
                    Padding(
                      padding: const EdgeInsets.symmetric(horizontal: 4),
                      child: Icon(Icons.chevron_right,
                          size: 16, color: Fleet.ink500),
                    ),
                  InkWell(
                    borderRadius: BorderRadius.circular(6),
                    onTap: () => _open(d.ancestry[i].id),
                    child: Padding(
                      padding:
                          const EdgeInsets.symmetric(horizontal: 4, vertical: 3),
                      child: ConstrainedBox(
                        constraints: const BoxConstraints(maxWidth: 260),
                        child: Text.rich(
                          TextSpan(children: [
                            TextSpan(
                              text: '${d.ancestry[i].ref} ',
                              style: TextStyle(
                                  color: Fleet.cool,
                                  fontFamily: 'monospace',
                                  fontSize: 12),
                            ),
                            TextSpan(
                              text: d.ancestry[i].title,
                              style: TextStyle(
                                  color: Fleet.ink200, fontSize: 12.5),
                            ),
                          ]),
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                        ),
                      ),
                    ),
                  ),
                ],
                Padding(
                  padding: const EdgeInsets.symmetric(horizontal: 4),
                  child:
                      Icon(Icons.chevron_right, size: 16, color: Fleet.ink500),
                ),
                Text('this',
                    style: TextStyle(
                        color: Fleet.ink300,
                        fontSize: 12.5,
                        fontWeight: FontWeight.w600)),
              ],
            ),
          ],
        ],
      ),
    );
  }

  Widget _people(Ticket t) {
    Widget row(String role, String label, String id, String name) {
      final empty = id.isEmpty;
      return InkWell(
        borderRadius: BorderRadius.circular(10),
        onTap: _mutating ? null : () => _changePerson(t, role),
        child: Container(
          constraints: const BoxConstraints(minHeight: 48),
          padding: const EdgeInsets.symmetric(vertical: 6, horizontal: 4),
          child: Row(
            children: [
              SizedBox(
                width: 84,
                child: Text(label,
                    style: TextStyle(color: Fleet.ink300, fontSize: 12.5)),
              ),
              Expanded(
                child: Text(
                  empty
                      ? 'Nobody'
                      : name.isEmpty
                          ? 'An agent you cannot see'
                          : name,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                    color: empty ? Fleet.ink500 : Fleet.ink100,
                    fontSize: 13.5,
                    fontWeight: empty ? FontWeight.w400 : FontWeight.w600,
                  ),
                ),
              ),
              Icon(Icons.edit_outlined,
                  size: 16,
                  color: Fleet.ink400,
                  semanticLabel: 'Change ${label.toLowerCase()}'),
            ],
          ),
        ),
      );
    }

    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: Fleet.ink900,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: Fleet.ink700),
      ),
      child: Column(
        children: [
          row('assignee', 'Assignee', t.assigneeId, t.assigneeName),
          Divider(height: 1, color: Fleet.ink800),
          row('reviewer', 'Reviewer', t.reviewerId, t.reviewerName),
          Divider(height: 1, color: Fleet.ink800),
          row('verifier', 'Verifier', t.verifierId, t.verifierName),
        ],
      ),
    );
  }

  List<Widget> _thread(TicketDetail d) {
    return [
      _SectionTitle('Thread', count: d.comments.length),
      if (d.comments.isEmpty)
        Padding(
          padding: const EdgeInsets.symmetric(vertical: 12),
          child: Text(
            'No comments yet. What the agents report, the engine notes and '
            'what you write here all land in this thread.',
            style: TextStyle(color: Fleet.ink400, fontSize: 12.5),
          ),
        )
      else
        for (final c in d.comments)
          TicketCommentTile(
            key: ValueKey(c.id),
            comment: c,
            sameAsReport: c.kind == 'result' &&
                c.body.trim().isNotEmpty &&
                c.body.trim() == d.ticket.result.trim(),
            onOpenFile: _openFile,
          ),
    ];
  }

  Widget _composerBar(Ticket t) {
    return SafeArea(
      top: false,
      child: Container(
        padding: const EdgeInsets.fromLTRB(12, 8, 8, 8),
        decoration: BoxDecoration(
          color: Fleet.ink900,
          border: Border(top: BorderSide(color: Fleet.ink800)),
        ),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            Expanded(
              child: TextField(
                controller: _composer,
                enabled: !_sending,
                minLines: 1,
                maxLines: 5,
                textCapitalization: TextCapitalization.sentences,
                decoration: InputDecoration(
                  hintText: 'Comment on ${t.ref}',
                  isDense: true,
                ),
                onSubmitted: (_) => _send(t),
              ),
            ),
            const SizedBox(width: 6),
            IconButton.filled(
              tooltip: 'Send',
              onPressed: _sending ? null : () => _send(t),
              icon: _sending
                  ? const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2))
                  : const Icon(Icons.send_rounded),
            ),
          ],
        ),
      ),
    );
  }
}

/// One entry in a ticket's thread, styled by what kind of entry it is.
class TicketCommentTile extends StatefulWidget {
  const TicketCommentTile({
    super.key,
    required this.comment,
    this.sameAsReport = false,
    this.onOpenFile,
  });

  final TicketComment comment;

  /// A result that says exactly what the closing report above says. It
  /// starts folded: the same page of text twice is a long way to scroll.
  final bool sameAsReport;

  /// Opens a file of the work catalog by name, for what a run published or
  /// shared. Null leaves the names as text.
  final void Function(String name)? onOpenFile;

  @override
  State<TicketCommentTile> createState() => _TicketCommentTileState();
}

class _TicketCommentTileState extends State<TicketCommentTile> {
  /// Review briefs are long -- the whole brief a reviewer was handed -- so
  /// they start folded, as does a result the closing report already shows.
  late bool _open =
      widget.comment.kind != 'review_brief' && !widget.sameAsReport;

  bool get _foldable =>
      widget.comment.kind == 'review_brief' || widget.sameAsReport;

  @override
  Widget build(BuildContext context) {
    final c = widget.comment;
    final when = c.createdAt == null ? '' : humanAgo(c.createdAt!);
    final who = c.authorName.isNotEmpty
        ? c.authorName
        : c.kind == 'system'
            ? 'Fleet'
            : 'Someone';

    if (c.kind == 'system') {
      return Padding(
        padding: const EdgeInsets.symmetric(vertical: 6),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.only(top: 1),
              child: Icon(Icons.info_outline, size: 14, color: Fleet.ink400),
            ),
            const SizedBox(width: 8),
            // Markdown so the engine's "T-11 asks Checker to check it" links
            // to T-11 like any other message.
            Expanded(
              child: MarkdownLite(
                c.body,
                baseStyle: TextStyle(
                    color: Fleet.ink300, fontSize: 12.5, height: 1.35),
              ),
            ),
            if (when.isNotEmpty) ...[
              const SizedBox(width: 8),
              Text(when, style: TextStyle(color: Fleet.ink300, fontSize: 11)),
            ],
          ],
        ),
      );
    }

    final published = c.kind == 'published' ? parsePublished(c.body) : null;
    if (published != null) return _published(c, published, who, when);

    final passed = c.kind == 'verdict' ? verdictPassed(c.body) : null;
    final reopened = c.kind == 'verdict' && c.body.startsWith('Reopened by ');
    final (Color color, IconData icon, String label) = switch (c.kind) {
      'result' => (Fleet.good, Icons.task_alt_rounded, 'Result'),
      'verdict' when reopened => (Fleet.warn, Icons.replay_rounded, 'Reopened'),
      'verdict' => (
          passed == null
              ? Fleet.cool
              : passed
                  ? Fleet.good
                  : Fleet.bad,
          passed == false ? Icons.gpp_bad_outlined : Icons.gavel_rounded,
          passed == null
              ? 'Verdict'
              : passed
                  ? 'Verdict: pass'
                  : 'Verdict: fail',
        ),
      'published' => (Fleet.cool, Icons.publish_rounded, 'Published'),
      'retry' => (Fleet.warn, Icons.replay_rounded, 'Retrying'),
      'review_brief' => (
          Fleet.ink300,
          Icons.rate_review_outlined,
          'Review brief'
        ),
      _ => (
          c.byPerson ? Fleet.live : Fleet.ink300,
          c.byPerson ? Icons.person_rounded : Icons.smart_toy_outlined,
          '',
        ),
    };
    final plain = c.kind == 'comment' || label.isEmpty;
    final files = c.kind == 'result' ? splitReportFiles(c.body) : null;
    final body = files?.body ?? c.body;

    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 5),
      child: Container(
        decoration: BoxDecoration(
          color: plain
              ? (c.byPerson
                  ? Fleet.live.withValues(alpha: 0.08)
                  : Fleet.ink850)
              : color.withValues(alpha: 0.07),
          borderRadius: BorderRadius.circular(12),
          border: plain
              ? Border.all(color: Fleet.ink800)
              : Border(left: BorderSide(color: color, width: 3)),
        ),
        padding: const EdgeInsets.fromLTRB(12, 4, 8, 10),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            InkWell(
              borderRadius: BorderRadius.circular(8),
              onTap: _foldable ? () => setState(() => _open = !_open) : null,
              child: ConstrainedBox(
                constraints: BoxConstraints(minHeight: _foldable ? 44 : 30),
                child: Row(
                  children: [
                    Icon(icon, size: 14, color: color),
                    const SizedBox(width: 6),
                    if (!plain) ...[
                      Text(label,
                          style: TextStyle(
                              color: color,
                              fontSize: 12,
                              fontWeight: FontWeight.w700)),
                      Text('  ·  ',
                          style:
                              TextStyle(color: Fleet.ink300, fontSize: 12)),
                    ],
                    Expanded(
                      child: Text(who,
                          maxLines: 1,
                          overflow: TextOverflow.ellipsis,
                          style: TextStyle(
                              color: Fleet.ink200,
                              fontSize: 12,
                              fontWeight: FontWeight.w600)),
                    ),
                    if (when.isNotEmpty)
                      Text(when,
                          style:
                              TextStyle(color: Fleet.ink300, fontSize: 11)),
                    if (_foldable)
                      Padding(
                        padding: const EdgeInsets.only(left: 4),
                        child: Icon(
                            _open ? Icons.expand_less : Icons.expand_more,
                            size: 20,
                            color: Fleet.ink300,
                            semanticLabel: _open ? 'Fold' : 'Unfold'),
                      ),
                  ],
                ),
              ),
            ),
            if (!_open && widget.sameAsReport)
              Text('The closing report above, as the agent sent it.',
                  style: TextStyle(color: Fleet.ink300, fontSize: 12)),
            if (_open) ...[
              const SizedBox(height: 2),
              SelectionArea(
                child: MarkdownLite(
                  body,
                  baseStyle: TextStyle(
                      color: Fleet.ink100, fontSize: 13.5, height: 1.45),
                ),
              ),
              if (files != null && files.hasFiles) ...[
                const SizedBox(height: 8),
                SharedFilesList(
                    files: files, onOpen: widget.onOpenFile ?? (_) {}),
              ],
            ],
          ],
        ),
      ),
    );
  }

  /// A file a run put in the work catalog: the file itself, to open.
  Widget _published(
      TicketComment c, PublishedItem item, String who, String when) {
    final detail = [
      if (item.version > 0) 'version ${item.version}',
      if (item.bytes > 0) formatBytes(item.bytes),
      'by $who',
      if (when.isNotEmpty) when,
    ].join(' · ');
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 5),
      child: Container(
        decoration: BoxDecoration(
          color: Fleet.cool.withValues(alpha: 0.07),
          borderRadius: BorderRadius.circular(12),
          border: Border.all(color: Fleet.cool.withValues(alpha: 0.35)),
        ),
        padding: const EdgeInsets.fromLTRB(4, 8, 4, 2),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Padding(
              padding: const EdgeInsets.symmetric(horizontal: 8),
              child: Row(
                children: [
                  Icon(Icons.publish_rounded, size: 14, color: Fleet.cool),
                  const SizedBox(width: 6),
                  Text(
                    item.kind == 'app'
                        ? 'Published an app'
                        : 'Published to the work catalog',
                    style: TextStyle(
                        color: Fleet.cool,
                        fontSize: 12,
                        fontWeight: FontWeight.w700),
                  ),
                ],
              ),
            ),
            CatalogFileTile(
              name: item.name,
              detail: detail,
              onTap: widget.onOpenFile == null
                  ? null
                  : () => widget.onOpenFile!(item.name),
            ),
          ],
        ),
      ),
    );
  }
}

class _RunTile extends StatelessWidget {
  const _RunTile({required this.task});
  final Task task;

  @override
  Widget build(BuildContext context) {
    return Card(
      margin: const EdgeInsets.only(bottom: 8),
      child: ListTile(
        dense: true,
        onTap: () => Navigator.of(context).push(MaterialPageRoute(
          builder: (_) => TaskDetailScreen(task: task),
        )),
        title: Text(
          task.maxSteps > 0
              ? 'Step ${task.step} of ${task.maxSteps}'
              : 'Run ${task.id.substring(0, task.id.length.clamp(0, 8))}',
          style: const TextStyle(fontSize: 13),
        ),
        subtitle: Text(
          [
            humanAgo(task.createdAt),
            if (task.error.isNotEmpty) task.error,
          ].join(' · '),
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
          style: TextStyle(
              color: task.error.isNotEmpty ? Fleet.bad : Fleet.ink400,
              fontSize: 11.5),
        ),
        trailing: StateChip(state: task.state, live: task.state == 'running'),
      ),
    );
  }
}

class _SectionTitle extends StatelessWidget {
  const _SectionTitle(this.text, {this.count = 0, this.trailing});
  final String text;
  final int count;
  final Widget? trailing;

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: const EdgeInsets.only(top: 20, bottom: 8),
      child: Row(
        children: [
          Text(text.toUpperCase(),
              style: TextStyle(
                  color: Fleet.ink300,
                  fontSize: 11,
                  letterSpacing: 0.6,
                  fontWeight: FontWeight.w700)),
          if (count > 0) ...[
            const SizedBox(width: 6),
            Text('$count',
                style: TextStyle(color: Fleet.ink400, fontSize: 11)),
          ],
          const Spacer(),
          if (trailing != null) trailing!,
        ],
      ),
    );
  }
}

class _Callout extends StatelessWidget {
  const _Callout({
    required this.icon,
    required this.color,
    required this.title,
    required this.body,
    this.markdown = false,
    this.footer,
  });

  final IconData icon;
  final Color color;
  final String title;
  final String body;
  final bool markdown;
  final Widget? footer;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: double.infinity,
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: color.withValues(alpha: 0.35)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(icon, size: 15, color: color),
              const SizedBox(width: 6),
              Text(title,
                  style: TextStyle(
                      color: color,
                      fontSize: 12.5,
                      fontWeight: FontWeight.w700)),
            ],
          ),
          const SizedBox(height: 6),
          SelectionArea(
            child: markdown
                ? MarkdownLite(body,
                    baseStyle: TextStyle(
                        color: Fleet.ink100, fontSize: 13.5, height: 1.45))
                : Text(body,
                    style: TextStyle(
                        color: Fleet.ink100, fontSize: 13.5, height: 1.45)),
          ),
          if (footer != null) ...[
            const SizedBox(height: 10),
            footer!,
          ],
        ],
      ),
    );
  }
}

class _LoadError extends StatelessWidget {
  const _LoadError({required this.message, required this.onRetry});
  final String message;
  final VoidCallback onRetry;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(Icons.search_off_rounded, size: 44, color: Fleet.ink500),
            const SizedBox(height: 12),
            Text(message,
                textAlign: TextAlign.center,
                style: TextStyle(color: Fleet.ink300, fontSize: 13)),
            const SizedBox(height: 16),
            OutlinedButton(onPressed: onRetry, child: const Text('Retry')),
          ],
        ),
      ),
    );
  }
}
