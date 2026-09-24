import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';
import 'work_logic.dart';

/// A ticket written by hand.
///
/// Most tickets come from asking the fleet in the chat; this is for the one
/// you want to put in a particular agent's hands, or under a particular
/// ticket, yourself.
class NewTicketSheet extends ConsumerStatefulWidget {
  const NewTicketSheet({super.key, this.parent});

  /// Creates the ticket under this one.
  final Ticket? parent;

  static Future<Ticket?> show(BuildContext context, {Ticket? parent}) =>
      showModalBottomSheet<Ticket>(
        context: context,
        isScrollControlled: true,
        backgroundColor: Fleet.ink900,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
        ),
        builder: (_) => NewTicketSheet(parent: parent),
      );

  @override
  ConsumerState<NewTicketSheet> createState() => _NewTicketSheetState();
}

class _NewTicketSheetState extends ConsumerState<NewTicketSheet> {
  final _title = TextEditingController();
  final _description = TextEditingController();
  late final _parent = TextEditingController(text: widget.parent?.ref ?? '');
  final _blockedBy = TextEditingController();
  final _budget = TextEditingController();
  String _assignee = '';
  String _reviewer = '';
  String _verifier = '';
  String _status = 'todo';
  String _kind = 'work';
  bool _more = false;
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _title.dispose();
    _description.dispose();
    _parent.dispose();
    _blockedBy.dispose();
    _budget.dispose();
    super.dispose();
  }

  Future<void> _create() async {
    final title = _title.text.trim();
    if (title.isEmpty) return;
    final budgetText = _budget.text.trim();
    final budget = budgetText.isEmpty ? 0.0 : double.tryParse(budgetText);
    if (budget == null || budget < 0) {
      setState(() => _error = 'The budget is an amount in dollars, or blank.');
      return;
    }
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final t = await ref.read(apiProvider).createTicket(
            title: title,
            description: _description.text.trim(),
            kind: _kind,
            status: _status,
            assigneeId: _assignee,
            reviewerId: _reviewer,
            verifierId: _verifier,
            parentId: _parent.text.trim(),
            blockedBy: parseTicketRefs(_blockedBy.text),
            budgetUsd: budget,
          );
      if (mounted) Navigator.pop(context, t);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Widget _agentField(String label, String value, ValueChanged<String> onChanged,
      List<Instance> agents,
      {String? helper}) {
    return DropdownButtonFormField<String>(
      key: ValueKey('$label:${agents.length}'),
      initialValue: agents.any((a) => a.id == value) ? value : '',
      isExpanded: true,
      decoration: InputDecoration(labelText: label, helperText: helper),
      items: [
        const DropdownMenuItem(value: '', child: Text('Nobody')),
        for (final a in agents)
          DropdownMenuItem(
            value: a.id,
            child: Text(
              a.title.isEmpty ? a.name : '${a.name} · ${a.title}',
              overflow: TextOverflow.ellipsis,
            ),
          ),
      ],
      onChanged: _busy ? null : (v) => onChanged(v ?? ''),
    );
  }

  @override
  Widget build(BuildContext context) {
    final agents = [
      ...(ref.watch(instancesProvider).valueOrNull ?? const <Instance>[])
    ]..sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
    final inset = MediaQuery.viewInsetsOf(context).bottom;

    return Padding(
      padding: EdgeInsets.fromLTRB(20, 18, 20, 18 + inset),
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                const Icon(Icons.confirmation_number_outlined, size: 20),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(
                    widget.parent == null
                        ? 'New ticket'
                        : 'New ticket under ${widget.parent!.ref}',
                    style: Theme.of(context).textTheme.titleMedium,
                  ),
                ),
              ],
            ),
            const SizedBox(height: 16),
            TextField(
              controller: _title,
              autofocus: true,
              enabled: !_busy,
              textCapitalization: TextCapitalization.sentences,
              decoration: const InputDecoration(
                labelText: 'What needs doing',
                hintText: 'e.g. Add a dark mode to the settings page',
              ),
              onChanged: (_) => setState(() {}),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _description,
              enabled: !_busy,
              minLines: 3,
              maxLines: 8,
              textCapitalization: TextCapitalization.sentences,
              decoration: const InputDecoration(
                labelText: 'Details (optional)',
                alignLabelWithHint: true,
                hintText: 'What done looks like, links, constraints.',
              ),
            ),
            const SizedBox(height: 12),
            _agentField('Assignee', _assignee,
                (v) => setState(() => _assignee = v), agents,
                helper: agents.isEmpty
                    ? 'No agents yet. The ticket waits for you to assign it.'
                    : 'Starts as soon as the assignee is free.'),
            const SizedBox(height: 12),
            SegmentedButton<String>(
              segments: const [
                ButtonSegment(value: 'todo', label: Text('Ready to start')),
                ButtonSegment(value: 'backlog', label: Text('Backlog')),
              ],
              selected: {_status},
              onSelectionChanged:
                  _busy ? null : (s) => setState(() => _status = s.first),
            ),
            const SizedBox(height: 6),
            Align(
              alignment: Alignment.centerLeft,
              child: TextButton.icon(
                onPressed: () => setState(() => _more = !_more),
                icon: Icon(_more ? Icons.expand_less : Icons.expand_more),
                label: Text(_more ? 'Fewer options' : 'Reviewer, parent, budget…'),
              ),
            ),
            if (_more) ...[
              _agentField('Reviewer', _reviewer,
                  (v) => setState(() => _reviewer = v), agents,
                  helper: 'Checks the work before it counts as done.'),
              const SizedBox(height: 12),
              _agentField('Verifier', _verifier,
                  (v) => setState(() => _verifier = v), agents,
                  helper: 'Checks the whole tree when it comes to rest.'),
              const SizedBox(height: 12),
              DropdownButtonFormField<String>(
                initialValue: _kind,
                decoration: const InputDecoration(labelText: 'Kind'),
                items: const [
                  DropdownMenuItem(value: 'work', child: Text('Work')),
                  DropdownMenuItem(value: 'review', child: Text('Review')),
                  DropdownMenuItem(value: 'verify', child: Text('Verify')),
                  DropdownMenuItem(value: 'unblock', child: Text('Unblock')),
                ],
                onChanged:
                    _busy ? null : (v) => setState(() => _kind = v ?? 'work'),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _parent,
                enabled: !_busy && widget.parent == null,
                autocorrect: false,
                decoration: const InputDecoration(
                  labelText: 'Under ticket',
                  hintText: 'T-12',
                  helperText: 'The ticket this one exists for. Blank for a new '
                      'top-level ticket.',
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _blockedBy,
                enabled: !_busy,
                autocorrect: false,
                decoration: const InputDecoration(
                  labelText: 'Waits on',
                  hintText: 'T-3, T-5',
                  helperText: 'It will not start until these are done.',
                ),
              ),
              const SizedBox(height: 12),
              TextField(
                controller: _budget,
                enabled: !_busy,
                keyboardType:
                    const TextInputType.numberWithOptions(decimal: true),
                decoration: const InputDecoration(
                  labelText: 'Budget for this ticket',
                  prefixText: '\$ ',
                  helperText: 'A run that spends it is stopped and the ticket '
                      'blocked. Blank for none.',
                ),
              ),
            ],
            InlineError(_error),
            const SizedBox(height: 16),
            FilledButton(
              onPressed:
                  _busy || _title.text.trim().isEmpty ? null : _create,
              child: _busy
                  ? const SizedBox(
                      width: 18,
                      height: 18,
                      child: CircularProgressIndicator(strokeWidth: 2))
                  : const Text('Create ticket'),
            ),
          ],
        ),
      ),
    );
  }
}
