import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/voice/voice_service.dart';
import 'message_tile.dart';

/// One conversation.
///
/// Every message is read aloud in the voice of the agent that sent it. In a
/// thread several agents talk in, a single shared voice tells you nothing about
/// who is speaking, which is the whole reason agents have their own.
class ConversationScreen extends ConsumerStatefulWidget {
  const ConversationScreen({
    super.key,
    required this.conversation,
    required this.title,
  });

  final Conversation conversation;
  final String title;

  @override
  ConsumerState<ConversationScreen> createState() => _ConversationScreenState();
}

class _ConversationScreenState extends ConsumerState<ConversationScreen> {
  final _controller = TextEditingController();
  final _scroll = ScrollController();

  late final VoiceService _voice = VoiceService(api: ref.read(apiProvider));

  List<PeerMessage> _messages = const [];
  bool _loading = true;
  bool _busy = false;
  bool _compacting = false;
  String? _error;
  Timer? _poll;

  /// Off by default: a fleet that talks constantly should not talk over you.
  bool _readAloud = false;

  late bool _pinned = widget.conversation.pinned;
  late String _title = widget.title;
  String _lastSpokenId = '';
  String _speakingId = '';

  @override
  void initState() {
    super.initState();
    _refresh();
    _poll = Timer.periodic(const Duration(seconds: 5), (_) => _refresh());
  }

  @override
  void dispose() {
    _poll?.cancel();
    _controller.dispose();
    _scroll.dispose();
    _voice.dispose();
    super.dispose();
  }

  Future<void> _refresh() async {
    try {
      final list = await ref
          .read(apiProvider)
          .conversationMessages(widget.conversation.id);
      if (!mounted) return;
      final atBottom = !_scroll.hasClients ||
          _scroll.position.pixels >= _scroll.position.maxScrollExtent - 40;
      setState(() {
        _messages = list;
        _loading = false;
        _error = null;
      });
      if (atBottom) _scrollToEnd();
      unawaited(_speakLatest(list));
    } catch (err) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = '$err';
      });
    }
  }

  void _scrollToEnd() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_scroll.hasClients) {
        _scroll.jumpTo(_scroll.position.maxScrollExtent);
      }
    });
  }

  /// Speak the newest message in the voice of whoever sent it.
  Future<void> _speakLatest(List<PeerMessage> ordered) async {
    if (!_readAloud || ordered.isEmpty) return;
    final m = ordered.last;
    if (m.id == _lastSpokenId) return;
    _lastSpokenId = m.id;

    // Your own messages are not read back to you.
    if (m.fromInstanceId.isEmpty) return;

    final instances = ref.read(instancesProvider).valueOrNull ?? const [];
    final sender =
        instances.where((i) => i.id == m.fromInstanceId).firstOrNull;
    _voice.useServerVoice(
      (sender?.voice.isNotEmpty ?? false) ? sender!.voice : null,
    );

    if (mounted) setState(() => _speakingId = m.id);
    await _voice.speak(m.content);
    if (mounted && _speakingId == m.id) setState(() => _speakingId = '');
  }

  Future<void> _send() async {
    final text = _controller.text.trim();
    if (text.isEmpty) return;
    setState(() => _busy = true);
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(apiProvider).sendPeerMessage(
            content: text,
            conversationId: widget.conversation.id,
            // A pair thread is between two agents; the server addresses the
            // message to the other member so it does not escape to the fleet.
            toInstanceId: _defaultRecipient(),
          );
      _controller.clear();
      await _refresh();
      _scrollToEnd();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// Who a message typed here is addressed to.
  String _defaultRecipient() {
    final agents = widget.conversation.members
        .where((m) => m != Conversation.operatorId)
        .toList();
    // One agent: address it. Anything else — a group, or a pair you are only
    // watching — goes out as a broadcast the server files into this thread.
    return agents.length == 1 ? agents.first : Conversation.broadcastId;
  }

  Future<void> _compact() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Compact this conversation?'),
        content: const Text(
          'The messages so far are replaced by a single summary, so agents stop '
          'carrying the whole history around.\n\n'
          'Nothing is deleted on the server — the original messages are kept, '
          'they just stop being shown and replayed.',
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel')),
          FilledButton(
              onPressed: () => Navigator.pop(ctx, true),
              child: const Text('Compact')),
        ],
      ),
    );
    if (confirmed != true || !mounted) return;

    setState(() => _compacting = true);
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(apiProvider).compactConversation(widget.conversation.id);
      await _refresh();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    } finally {
      if (mounted) setState(() => _compacting = false);
    }
  }

  Future<void> _rename() async {
    final controller = TextEditingController(text: widget.conversation.title);
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
      final updated = await ref
          .read(apiProvider)
          .updateConversation(widget.conversation.id, title: name.trim());
      if (mounted) setState(() => _title = updated.title);
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  Future<void> _togglePin() async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      final updated = await ref
          .read(apiProvider)
          .updateConversation(widget.conversation.id, pinned: !_pinned);
      if (mounted) setState(() => _pinned = updated.pinned);
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  Future<void> _delete() async {
    final confirmed = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Delete this conversation?'),
        content: const Text(
          'The thread is removed from your comms list. What was said in it is '
          'kept on the server — closing a thread should not destroy the record '
          'of what your agents agreed.',
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
      await ref.read(apiProvider).deleteConversation(widget.conversation.id);
      navigator.pop();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                if (_pinned) ...[
                  Icon(Icons.push_pin, size: 13, color: Fleet.warn),
                  const SizedBox(width: 5),
                ],
                Flexible(
                  child: Text(_title,
                      overflow: TextOverflow.ellipsis,
                      style: const TextStyle(fontSize: 15)),
                ),
              ],
            ),
            Text(
              widget.conversation.isPair
                  ? 'Two agents talking — you are watching'
                  : '${_messages.length} message'
                      '${_messages.length == 1 ? '' : 's'}',
              style: TextStyle(color: Fleet.ink400, fontSize: 11),
            ),
          ],
        ),
        actions: [
          IconButton(
            tooltip: _readAloud ? 'Stop reading aloud' : 'Read aloud',
            icon: Icon(
              _readAloud ? Icons.volume_up_rounded : Icons.volume_off_outlined,
              color: _readAloud ? Fleet.good : null,
            ),
            onPressed: () {
              setState(() {
                _readAloud = !_readAloud;
                if (!_readAloud) _speakingId = '';
                // Whatever is on screen now is history, not an arrival: mark it
                // spoken so turning this on does not replay the backlog.
                if (_readAloud && _messages.isNotEmpty) {
                  _lastSpokenId = _messages.last.id;
                }
              });
              if (!_readAloud) unawaited(_voice.stopSpeaking());
            },
          ),
          PopupMenuButton<String>(
            color: Fleet.ink850,
            icon: const Icon(Icons.more_vert),
            onSelected: (a) => switch (a) {
              'rename' => _rename(),
              'pin' => _togglePin(),
              _ => _delete(),
            },
            itemBuilder: (_) => [
              if (!widget.conversation.isBroadcast) ...[
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
                    leading: Icon(_pinned
                        ? Icons.push_pin_outlined
                        : Icons.push_pin),
                    title: Text(_pinned ? 'Unpin' : 'Pin to top'),
                  ),
                ),
                PopupMenuItem(
                  value: 'delete',
                  child: ListTile(
                    dense: true,
                    leading: Icon(Icons.delete_outline, color: Fleet.bad),
                    title: Text('Delete conversation',
                        style: TextStyle(color: Fleet.bad)),
                  ),
                ),
              ],
            ],
          ),
          IconButton(
            tooltip: 'Compact conversation',
            icon: _compacting
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2))
                : const Icon(Icons.compress_rounded),
            onPressed: _compacting || _messages.length < 2 ? null : _compact,
          ),
        ],
      ),
      body: Column(
        children: [
          Expanded(child: _buildList()),
          _buildComposer(),
        ],
      ),
    );
  }

  Widget _buildList() {
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_error != null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Text('Could not load this conversation: $_error',
              textAlign: TextAlign.center,
              style: TextStyle(color: Fleet.bad, fontSize: 12)),
        ),
      );
    }
    if (_messages.isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Text(
            widget.conversation.isPair
                ? 'Nothing said yet.\n\nThese two can talk here. Send something '
                    'to start them off, or leave them to it.'
                : 'Nothing said yet.',
            textAlign: TextAlign.center,
            style: TextStyle(color: Fleet.ink400, fontSize: 13, height: 1.4),
          ),
        ),
      );
    }
    return ListView.builder(
      controller: _scroll,
      padding: const EdgeInsets.all(12),
      itemCount: _messages.length,
      itemBuilder: (_, i) => MessageTile(
        message: _messages[i],
        speaking: _messages[i].id == _speakingId,
      ),
    );
  }

  Widget _buildComposer() {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Fleet.ink900,
        border: Border(top: BorderSide(color: Fleet.ink800)),
      ),
      child: Row(
        children: [
          Expanded(
            child: TextField(
              controller: _controller,
              enabled: !_busy,
              minLines: 1,
              maxLines: 3,
              textCapitalization: TextCapitalization.sentences,
              decoration: InputDecoration(
                hintText: widget.conversation.isPair
                    ? 'Say something to both'
                    : 'Message',
                contentPadding:
                    const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
              ),
              onSubmitted: (_) => _send(),
            ),
          ),
          const SizedBox(width: 8),
          FilledButton(
            onPressed: _busy ? null : _send,
            child: _busy
                ? const SizedBox(
                    width: 16,
                    height: 16,
                    child: CircularProgressIndicator(strokeWidth: 2))
                : const Icon(Icons.send_rounded, size: 18),
          ),
        ],
      ),
    );
  }
}
