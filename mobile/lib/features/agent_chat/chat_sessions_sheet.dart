import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';

/// Manage the chats you have with one bot.
///
/// Pick one, start another, name them, pin the ones you come back to, delete
/// the ones that went nowhere. Each chat is its own context: what an agent
/// sees when it answers is only the chat you are in.
class ChatSessionsSheet extends ConsumerStatefulWidget {
  const ChatSessionsSheet({
    super.key,
    required this.instanceId,
    required this.instanceName,
    required this.activeChatId,
  });

  final String instanceId;
  final String instanceName;
  final String activeChatId;

  /// Returns the chat to switch to, or null if nothing changed.
  static Future<ChatSession?> show(
    BuildContext context, {
    required String instanceId,
    required String instanceName,
    required String activeChatId,
  }) =>
      showModalBottomSheet<ChatSession>(
        context: context,
        isScrollControlled: true,
        builder: (_) => ChatSessionsSheet(
          instanceId: instanceId,
          instanceName: instanceName,
          activeChatId: activeChatId,
        ),
      );

  @override
  ConsumerState<ChatSessionsSheet> createState() => _ChatSessionsSheetState();
}

class _ChatSessionsSheetState extends ConsumerState<ChatSessionsSheet> {
  List<ChatSession> _sessions = const [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    try {
      final list = await ref.read(apiProvider).chatSessions(widget.instanceId);
      if (!mounted) return;
      setState(() {
        _sessions = list;
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

  Future<void> _newChat() async {
    final messenger = ScaffoldMessenger.of(context);
    final navigator = Navigator.of(context);
    try {
      final created =
          await ref.read(apiProvider).createChatSession(widget.instanceId);
      navigator.pop(created);
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  Future<void> _rename(ChatSession c) async {
    final controller = TextEditingController(text: c.title);
    final name = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Name this chat'),
        content: TextField(
          controller: controller,
          autofocus: true,
          textCapitalization: TextCapitalization.sentences,
          decoration: const InputDecoration(hintText: 'e.g. Deploy notes'),
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
          .updateChatSession(widget.instanceId, c.id, title: name.trim());
      await _refresh();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  Future<void> _togglePin(ChatSession c) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref
          .read(apiProvider)
          .updateChatSession(widget.instanceId, c.id, pinned: !c.pinned);
      await _refresh();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  Future<void> _delete(ChatSession c) async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Delete this chat?'),
        content: Text(
          '"${c.displayTitle}" and its ${c.messageCount} message'
          '${c.messageCount == 1 ? '' : 's'} are removed for good.\n\n'
          'Your other chats with ${widget.instanceName} are untouched.',
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
    final navigator = Navigator.of(context);
    try {
      await ref
          .read(apiProvider)
          .deleteChatSession(widget.instanceId, c.id);
      // Deleting the chat you are reading has to move you somewhere real.
      if (c.id == widget.activeChatId) {
        final left = await ref.read(apiProvider).chatSessions(widget.instanceId);
        navigator.pop(left.isEmpty
            ? const ChatSession(
                id: '', title: '', pinned: false, messageCount: 0)
            : left.first);
        return;
      }
      await _refresh();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  @override
  Widget build(BuildContext context) {
    return Padding(
      padding: EdgeInsets.only(
        bottom: MediaQuery.of(context).viewInsets.bottom,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        children: [
          Padding(
            padding: const EdgeInsets.fromLTRB(16, 16, 8, 8),
            child: Row(
              children: [
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Text('Chats with ${widget.instanceName}',
                          style: const TextStyle(
                              fontSize: 16, fontWeight: FontWeight.w700)),
                      Text('Each chat is its own context',
                          style:
                              TextStyle(color: Fleet.ink400, fontSize: 11)),
                    ],
                  ),
                ),
                FilledButton.icon(
                  onPressed: _newChat,
                  icon: const Icon(Icons.add, size: 17),
                  label: const Text('New'),
                ),
              ],
            ),
          ),
          Flexible(child: _buildList()),
          const SizedBox(height: 12),
        ],
      ),
    );
  }

  Widget _buildList() {
    if (_loading) {
      return const Padding(
        padding: EdgeInsets.all(28),
        child: CircularProgressIndicator(),
      );
    }
    if (_error != null) {
      return Padding(
        padding: const EdgeInsets.all(24),
        child: Text('Could not load chats: $_error',
            textAlign: TextAlign.center,
            style: TextStyle(color: Fleet.bad, fontSize: 12)),
      );
    }
    if (_sessions.isEmpty) {
      return Padding(
        padding: const EdgeInsets.fromLTRB(24, 8, 24, 24),
        child: Text(
          'No separate chats yet. "New" starts one with a clean context.',
          textAlign: TextAlign.center,
          style: TextStyle(color: Fleet.ink400, fontSize: 12.5, height: 1.4),
        ),
      );
    }

    return ListView.builder(
      shrinkWrap: true,
      padding: const EdgeInsets.symmetric(horizontal: 12),
      itemCount: _sessions.length,
      itemBuilder: (_, i) => _tile(_sessions[i]),
    );
  }

  Widget _tile(ChatSession c) {
    final active = c.id == widget.activeChatId;

    return Card(
      color: active ? Fleet.live.withValues(alpha: 0.12) : Fleet.ink850,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(12),
        side: active
            ? BorderSide(color: Fleet.live.withValues(alpha: 0.45))
            : BorderSide.none,
      ),
      margin: const EdgeInsets.only(bottom: 8),
      child: ListTile(
        dense: true,
        onTap: () => Navigator.pop(context, c),
        leading: Icon(
          c.pinned ? Icons.push_pin : Icons.chat_bubble_outline,
          size: 18,
          color: c.pinned ? Fleet.warn : Fleet.ink400,
        ),
        title: Row(
          children: [
            Flexible(
              child: Text(c.displayTitle,
                  overflow: TextOverflow.ellipsis,
                  style: const TextStyle(
                      fontSize: 13.5, fontWeight: FontWeight.w600)),
            ),
            if (active) ...[
              const SizedBox(width: 6),
              Text('OPEN',
                  style: TextStyle(
                      fontSize: 9,
                      letterSpacing: 0.5,
                      fontWeight: FontWeight.w700,
                      color: Fleet.live)),
            ],
          ],
        ),
        subtitle: Text(
          '${c.messageCount} message${c.messageCount == 1 ? '' : 's'}',
          style: TextStyle(color: Fleet.ink400, fontSize: 11),
        ),
        trailing: PopupMenuButton<String>(
          color: Fleet.ink850,
          icon: Icon(Icons.more_vert, size: 18, color: Fleet.ink400),
          onSelected: (a) => switch (a) {
            'rename' => _rename(c),
            'pin' => _togglePin(c),
            _ => _delete(c),
          },
          itemBuilder: (_) => [
            // The earlier chat is not a real row on the server — it stands for
            // the history from before chats could be separated — so it can be
            // cleared but not named or pinned.
            if (!c.isDefault) ...[
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
            ],
            PopupMenuItem(
              value: 'delete',
              child: ListTile(
                dense: true,
                leading: Icon(Icons.delete_outline, color: Fleet.bad),
                title: Text(c.isDefault ? 'Clear' : 'Delete',
                    style: TextStyle(color: Fleet.bad)),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
