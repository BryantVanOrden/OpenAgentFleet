import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';

/// Who may do what with one bot.
///
/// This is the exception layer. The department role covers the normal case;
/// this covers the contractor who may drive exactly one machine, and the
/// auditor who may read one bot's transcripts and nothing else.
class BotAccessSheet extends ConsumerStatefulWidget {
  const BotAccessSheet({super.key, required this.instance});

  final Instance instance;

  static Future<bool?> show(BuildContext context, Instance instance) =>
      showModalBottomSheet<bool>(
        context: context,
        isScrollControlled: true,
        builder: (_) => BotAccessSheet(instance: instance),
      );

  @override
  ConsumerState<BotAccessSheet> createState() => _BotAccessSheetState();
}

class _BotAccessSheetState extends ConsumerState<BotAccessSheet> {
  List<BotGrant> _grants = const [];
  List<FleetUser> _users = const [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    try {
      final api = ref.read(apiProvider);
      final grants = await api.botGrants(widget.instance.id);
      List<FleetUser> users = const [];
      try {
        users = await api.users();
      } catch (_) {}
      if (!mounted) return;
      setState(() {
        _grants = grants;
        _users = users;
        _loading = false;
        _error = null;
      });
    } catch (err) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = '$err';
      });
    }
  }

  String _emailOf(String userId) => _users
      .where((u) => u.id == userId)
      .map((u) => u.email)
      .firstOrNull ??
      userId;

  Future<void> _save(String userId, List<String>? perms) async {
    try {
      await ref.read(apiProvider).setBotGrant(widget.instance.id, userId, perms);
      await _refresh();
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    }
  }

  Future<void> _editGrant({BotGrant? existing}) async {
    String? userId = existing?.userId;
    final chosen = {...?existing?.permissions};

    final result = await showDialog<String>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setLocal) => AlertDialog(
          title: Text(existing == null ? 'Give someone access' : 'Change access'),
          content: SingleChildScrollView(
            child: Column(
              mainAxisSize: MainAxisSize.min,
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                if (existing == null)
                  DropdownButtonFormField<String>(
                    initialValue: userId,
                    isExpanded: true,
                    decoration: const InputDecoration(labelText: 'Person'),
                    dropdownColor: Fleet.ink850,
                    items: [
                      for (final u in _users)
                        DropdownMenuItem(value: u.id, child: Text(u.email)),
                    ],
                    onChanged: (v) => setLocal(() => userId = v),
                  )
                else
                  Text(_emailOf(existing.userId),
                      style: const TextStyle(fontWeight: FontWeight.w600)),
                const SizedBox(height: 10),
                Text(
                  'This replaces what their department role would allow, for '
                  'this bot only. Leaving everything unticked hides the bot '
                  'from them entirely.',
                  style:
                      TextStyle(color: Fleet.ink400, fontSize: 11, height: 1.4),
                ),
                const SizedBox(height: 6),
                for (final p in BotGrant.perBot)
                  CheckboxListTile(
                    dense: true,
                    contentPadding: EdgeInsets.zero,
                    value: chosen.contains(p),
                    onChanged: (on) => setLocal(() {
                      if (on == true) {
                        chosen.add(p);
                        // Everything implies being able to see it; a grant
                        // without view would be unreachable in the UI.
                        chosen.add('view');
                      } else {
                        chosen.remove(p);
                      }
                    }),
                    title: Text(BotGrant.labels[p] ?? p,
                        style: const TextStyle(fontSize: 13)),
                  ),
              ],
            ),
          ),
          actions: [
            if (existing != null)
              TextButton(
                onPressed: () => Navigator.pop(ctx, 'remove'),
                child: Text('Use department default',
                    style: TextStyle(color: Fleet.ink300, fontSize: 12)),
              ),
            TextButton(
                onPressed: () => Navigator.pop(ctx),
                child: const Text('Cancel')),
            FilledButton(
                onPressed: () => Navigator.pop(ctx, 'save'),
                child: const Text('Save')),
          ],
        ),
      ),
    );

    if (result == null || userId == null) return;
    // Removing the grant restores the department default; saving an empty set
    // is a deliberate "nothing", which is how a bot is hidden.
    await _save(userId!, result == 'remove' ? null : chosen.toList());
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: EdgeInsets.only(
        left: 16,
        right: 16,
        top: 16,
        bottom: MediaQuery.of(context).viewInsets.bottom + 16,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Expanded(
                child: Text('Access to ${widget.instance.name}',
                    style: const TextStyle(
                        fontSize: 16, fontWeight: FontWeight.w700)),
              ),
              TextButton.icon(
                onPressed: _users.isEmpty ? null : () => _editGrant(),
                icon: const Icon(Icons.add, size: 16),
                label: const Text('Add', style: TextStyle(fontSize: 12)),
              ),
            ],
          ),
          Text(
            'Exceptions to the department role. Everyone else follows their '
            'role in ${widget.instance.orgId.isEmpty ? 'no department' : 'this bot\'s department'}.',
            style: TextStyle(color: Fleet.ink400, fontSize: 11, height: 1.4),
          ),
          const SizedBox(height: 12),
          if (_loading)
            const Padding(
              padding: EdgeInsets.all(24),
              child: Center(child: CircularProgressIndicator()),
            )
          else if (_grants.isEmpty)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 14),
              child: Text('No exceptions — everyone follows their role.',
                  style: TextStyle(color: Fleet.ink500, fontSize: 12)),
            )
          else
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 260),
              child: ListView(
                shrinkWrap: true,
                children: [for (final g in _grants) _tile(g)],
              ),
            ),
          InlineError(_error),
          const SizedBox(height: 8),
        ],
      ),
    );
  }

  Widget _tile(BotGrant g) {
    final summary = g.permissions.isEmpty
        ? 'No access — this bot is hidden from them'
        : g.permissions
            .where((p) => p != 'view')
            .map((p) => BotGrant.labels[p] ?? p)
            .join(', ');

    return Card(
      color: Fleet.ink850,
      margin: const EdgeInsets.only(bottom: 6),
      child: ListTile(
        dense: true,
        onTap: () => _editGrant(existing: g),
        leading: Icon(
          g.permissions.isEmpty ? Icons.visibility_off_outlined : Icons.key_outlined,
          size: 18,
          color: g.permissions.isEmpty ? Fleet.bad : Fleet.cool,
        ),
        title: Text(_emailOf(g.userId), style: const TextStyle(fontSize: 13)),
        subtitle: Text(summary.isEmpty ? 'Can see it only' : summary,
            maxLines: 2,
            overflow: TextOverflow.ellipsis,
            style: TextStyle(color: Fleet.ink400, fontSize: 10.5)),
      ),
    );
  }
}
