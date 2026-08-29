import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import 'conversation_screen.dart';
import 'new_conversation_sheet.dart';

/// Fleet comms: every conversation in the fleet.
///
/// This is the same peer bus agents use for message_peer and delegate_task, so
/// what you see is the actual traffic rather than a summary of it. It lives
/// under Fleet, not Vault: a conversation between agents is not a credential
/// store, and filing it there made it invisible.
///
/// Threads are real objects you create and delete. Put two agents in a thread
/// and they can work something out between themselves while you read along;
/// close it when they are done.
class CommsScreen extends ConsumerStatefulWidget {
  const CommsScreen({super.key});

  @override
  ConsumerState<CommsScreen> createState() => _CommsScreenState();
}

class _CommsScreenState extends ConsumerState<CommsScreen> {
  List<Conversation> _conversations = const [];
  bool _loading = true;
  String? _error;
  Timer? _poll;

  @override
  void initState() {
    super.initState();
    _refresh();
    // The peer bus has no websocket topic of its own, so this polls. Five
    // seconds keeps the list feeling live without hammering the API.
    _poll = Timer.periodic(const Duration(seconds: 5), (_) => _refresh());
  }

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  Future<void> _refresh() async {
    try {
      final list = await ref.read(apiProvider).conversations();
      if (!mounted) return;
      setState(() {
        _conversations = list;
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

  /// A readable name for a thread, falling back to who is in it.
  String _titleOf(Conversation c, List<Instance> instances) {
    if (c.title.isNotEmpty) return c.title;
    if (c.isBroadcast) return 'Everyone';

    final names = c.members.map((m) {
      if (m == Conversation.operatorId) return 'You';
      final match = instances.where((i) => i.id == m).firstOrNull;
      return match?.name ?? m.substring(0, m.length.clamp(0, 8));
    }).toList();
    return names.isEmpty ? 'Conversation' : names.join('  ·  ');
  }

  Future<void> _newConversation() async {
    final created = await showModalBottomSheet<Conversation>(
      context: context,
      isScrollControlled: true,
      builder: (_) => const NewConversationSheet(),
    );
    if (created == null || !mounted) return;
    await _refresh();
    if (!mounted) return;
    _open(created);
  }

  void _open(Conversation c) {
    final instances = ref.read(instancesProvider).valueOrNull ?? const [];
    Navigator.of(context)
        .push(MaterialPageRoute(
          builder: (_) => ConversationScreen(
            conversation: c,
            title: _titleOf(c, instances),
          ),
        ))
        .then((_) => _refresh());
  }

  Future<void> _rename(Conversation c) async {
    final controller = TextEditingController(text: c.title);
    final name = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Name this conversation'),
        content: TextField(
          controller: controller,
          autofocus: true,
          textCapitalization: TextCapitalization.sentences,
          decoration: const InputDecoration(hintText: 'e.g. Release checks'),
          onSubmitted: (v) => Navigator.pop(ctx, v),
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx), child: const Text('Cancel')),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, controller.text),
            child: const Text('Save'),
          ),
        ],
      ),
    );
    if (name == null || name.trim().isEmpty || !mounted) return;

    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref
          .read(apiProvider)
          .updateConversation(c.id, title: name.trim());
      await _refresh();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  Future<void> _togglePin(Conversation c) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(apiProvider).updateConversation(c.id, pinned: !c.pinned);
      await _refresh();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  Future<void> _delete(Conversation c) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Delete this conversation?'),
        content: const Text(
          'The thread is removed from this list. What was said in it is kept on '
          'the server — closing a thread should not destroy the record of what '
          'your agents agreed.',
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
    if (confirmed != true || !mounted) return;

    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(apiProvider).deleteConversation(c.id);
      await _refresh();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  @override
  Widget build(BuildContext context) {
    final instances = ref.watch(instancesProvider).valueOrNull ?? const [];

    return Scaffold(
      appBar: AppBar(
        title: const Text('Fleet comms'),
        bottom: PreferredSize(
          preferredSize: const Size.fromHeight(28),
          child: Padding(
            padding: const EdgeInsets.only(left: 16, bottom: 8, right: 16),
            child: Align(
              alignment: Alignment.centerLeft,
              child: Text(
                'Every conversation in the fleet. Open one to read along or '
                'join in.',
                style: TextStyle(color: Fleet.ink400, fontSize: 11),
              ),
            ),
          ),
        ),
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _newConversation,
        icon: const Icon(Icons.add_comment_outlined),
        label: const Text('New chat'),
      ),
      body: _buildBody(instances),
    );
  }

  Widget _buildBody(List<Instance> instances) {
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_error != null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Text('Could not load comms: $_error',
              textAlign: TextAlign.center,
              style: TextStyle(color: Fleet.bad, fontSize: 12)),
        ),
      );
    }

    return RefreshIndicator(
      onRefresh: _refresh,
      child: ListView.builder(
        padding: const EdgeInsets.fromLTRB(12, 12, 12, 88),
        itemCount: _conversations.length,
        itemBuilder: (_, i) => _tile(_conversations[i], instances),
      ),
    );
  }

  Widget _tile(Conversation c, List<Instance> instances) {
    final icon = switch (c.kind) {
      'direct' => Icons.person_outline,
      'pair' => Icons.swap_horiz_rounded,
      _ => c.isBroadcast ? Icons.campaign_outlined : Icons.groups_outlined,
    };

    final subtitle = switch (c.kind) {
      'pair' => 'Two agents — you are watching',
      'direct' => 'You and one agent',
      _ => c.isBroadcast ? 'Everyone in the fleet' : 'Group',
    };

    return Card(
      color: Fleet.ink850,
      margin: const EdgeInsets.only(bottom: 8),
      child: ListTile(
        leading: Icon(icon, size: 20, color: Fleet.ink300),
        title: Row(
          children: [
            if (c.pinned) ...[
              Icon(Icons.push_pin, size: 12, color: Fleet.warn),
              const SizedBox(width: 5),
            ],
            Flexible(
              child: Text(_titleOf(c, instances),
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                      fontSize: 14, fontWeight: FontWeight.w600)),
            ),
          ],
        ),
        subtitle: Text(
          '$subtitle · ${c.messageCount} message'
          '${c.messageCount == 1 ? '' : 's'}',
          style: TextStyle(color: Fleet.ink400, fontSize: 11),
        ),
        trailing: c.isBroadcast
            // The broadcast channel is where an unaddressed message lands, so
            // there is nowhere for its traffic to go if it were removed.
            ? Icon(Icons.lock_outline, size: 15, color: Fleet.ink600)
            : PopupMenuButton<String>(
                color: Fleet.ink850,
                icon: Icon(Icons.more_vert, size: 19, color: Fleet.ink400),
                onSelected: (a) => switch (a) {
                  'rename' => _rename(c),
                  'pin' => _togglePin(c),
                  _ => _delete(c),
                },
                itemBuilder: (_) => [
                  const PopupMenuItem(
                    value: 'rename',
                    child: ListTile(
                        dense: true,
                        leading: Icon(Icons.edit_outlined),
                        title: Text('Rename')),
                  ),
                  PopupMenuItem(
                    value: 'pin',
                    child: ListTile(
                      dense: true,
                      leading: Icon(
                          c.pinned ? Icons.push_pin_outlined : Icons.push_pin),
                      title: Text(c.pinned ? 'Unpin' : 'Pin to top'),
                    ),
                  ),
                  PopupMenuItem(
                    value: 'delete',
                    child: ListTile(
                      dense: true,
                      leading: Icon(Icons.delete_outline, color: Fleet.bad),
                      title:
                          Text('Delete', style: TextStyle(color: Fleet.bad)),
                    ),
                  ),
                ],
              ),
        onTap: () => _open(c),
      ),
    );
  }
}
