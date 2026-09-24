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
    this.siblings = const [],
  });

  final Conversation conversation;
  final String title;

  /// Every thread with the same participants, most recent first. More than one
  /// is ordinary — the same way you can have several chats with a colleague —
  /// so the screen opens the newest and offers the rest.
  final List<Conversation> siblings;

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
  late Conversation _current = widget.conversation;
  late List<Conversation> _siblings = widget.siblings.isEmpty
      ? [widget.conversation]
      : [...widget.siblings];
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
          .conversationMessages(_current.id);
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
      // Each speaker at its own pace. In a thread with several bots in it,
      // pace is as much of the "who just said that" as the voice is.
      speed: (sender?.voiceSpeed ?? 0) != 0 ? sender!.voiceSpeed : 1.0,
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
            conversationId: _current.id,
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
    final agents = _current.members
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
      await ref.read(apiProvider).compactConversation(_current.id);
      await _refresh();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    } finally {
      if (mounted) setState(() => _compacting = false);
    }
  }

  Future<void> _rename() async {
    final controller = TextEditingController(text: _current.title);
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
          .updateConversation(_current.id, title: name.trim());
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
          .updateConversation(_current.id, pinned: !_pinned);
      if (mounted) setState(() => _pinned = updated.pinned);
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  /// Confirm removing one thread. Shared by the open thread's own menu and by
  /// the picker, so both say the same thing about what survives.
  Future<bool> _confirmDelete(BuildContext ctx, Conversation t) async {
    final name = t.title.isEmpty ? 'this conversation' : '"${t.title}"';
    final ok = await showDialog<bool>(
      context: ctx,
      builder: (d) => AlertDialog(
        title: Text('Delete $name?'),
        content: const Text(
          'The thread and everything said in it are removed. This cannot be '
          'undone.',
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(d, false),
              child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Fleet.bad),
            onPressed: () => Navigator.pop(d, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    return ok == true;
  }

  /// Remove one of the sibling threads without leaving the screen.
  ///
  /// Deleting the thread you are reading moves you to a sibling rather than
  /// closing the screen — you came here to prune a list, not to leave it.
  Future<void> _deleteSibling(Conversation t) async {
    try {
      await ref.read(apiProvider).deleteConversation(t.id);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
      return;
    }
    if (!mounted) return;
    final remaining = _siblings.where((s) => s.id != t.id).toList();
    setState(() {
      _siblings = remaining;
      if (t.id == _current.id && remaining.isNotEmpty) {
        _current = remaining.first;
        _title = _current.title;
        _pinned = _current.pinned;
        _messages = const [];
        _loading = true;
        _lastSpokenId = '';
      }
    });
    if (t.id == _current.id || _messages.isEmpty) await _refresh();
  }

  Future<void> _delete() async {
    final confirmed = await _confirmDelete(context, _current);
    if (!confirmed || !mounted) return;

    final messenger = ScaffoldMessenger.of(context);
    final navigator = Navigator.of(context);
    try {
      await ref.read(apiProvider).deleteConversation(_current.id);
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
              _current.isPair
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
              // The built-in channel can be renamed and pinned like any other
              // -- the server stores both. Only deleting it is refused, since
              // an unaddressed message would then have nowhere to land.
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
                      _pinned ? Icons.push_pin_outlined : Icons.push_pin),
                  title: Text(_pinned ? 'Unpin' : 'Pin to top'),
                ),
              ),
              // Offered for the built-in channel too: the server allows it
              // once another everyone-channel exists, and refuses with a
              // message saying so when it does not.
              if (!_current.isBroadcast || _siblings.length > 1)
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
          _threadBar(),
          Expanded(child: _buildList()),
          _buildComposer(),
        ],
      ),
    );
  }

  /// Which of these people's threads is open, and how to reach the others.
  ///
  /// Always shown, including on a thread with no siblings yet: this bar is
  /// where a new chat with the same people is started, so hiding it when there
  /// is only one is hiding the only way to make a second — which is exactly
  /// what happened on the everyone-channel.
  ///
  /// On screen rather than in a menu for the same reason the per-bot chat
  /// switcher is: which thread you are in decides who reads what you type.
  Widget _threadBar() {
    return InkWell(
      onTap: _switchThread,
      child: Container(
        padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 9),
        decoration: BoxDecoration(
          color: Fleet.ink900,
          border: Border(bottom: BorderSide(color: Fleet.ink800)),
        ),
        child: Row(
          children: [
            Icon(Icons.forum_outlined, size: 15, color: Fleet.ink400),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                _siblings.length > 1
                    ? '$_title  ·  ${_siblings.length} chats'
                    : _title,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(
                    fontSize: 12.5, fontWeight: FontWeight.w600),
              ),
            ),
            IconButton(
              tooltip: 'New chat with the same people',
              visualDensity: VisualDensity.compact,
              icon: Icon(Icons.add_comment_outlined,
                  size: 18, color: Fleet.ink300),
              onPressed: _newSiblingThread,
            ),
            if (_siblings.length > 1)
              Icon(Icons.expand_more, size: 18, color: Fleet.ink400),
          ],
        ),
      ),
    );
  }

  Future<void> _switchThread() async {
    if (_siblings.length < 2) return;
    // Deleting is offered here, per row, so several can be pruned in one go.
    // It used to live only in the open thread's own menu, which meant getting
    // from four chats down to one was four round trips through the list.
    final picked = await showModalBottomSheet<Conversation>(
      context: context,
      builder: (sheetCtx) => StatefulBuilder(
        builder: (sheetCtx, setSheet) => SafeArea(
          child: ListView(
            shrinkWrap: true,
            children: [
              for (final t in _siblings)
                ListTile(
                  dense: true,
                  selected: t.id == _current.id,
                  leading: Icon(
                      t.id == _current.id
                          ? Icons.radio_button_checked
                          : Icons.radio_button_unchecked,
                      size: 18,
                      color: t.id == _current.id ? Fleet.live : Fleet.ink500),
                  title: Text(t.title.isEmpty ? 'Untitled chat' : t.title,
                      style: const TextStyle(fontSize: 13)),
                  subtitle: Text(
                      '${t.messageCount} message${t.messageCount == 1 ? '' : 's'}',
                      style: TextStyle(color: Fleet.ink400, fontSize: 11)),
                  // Anything can go while something is left to take
                  // unaddressed traffic -- including the built-in channel,
                  // which used to be refused outright and no longer needs to
                  // be once a second everyone-channel exists. Deleting the one
                  // you are reading just moves you to a sibling.
                  trailing: _siblings.length < 2
                      ? null
                      : IconButton(
                          tooltip: 'Delete this chat',
                          icon: Icon(Icons.delete_outline,
                              size: 18, color: Fleet.bad),
                          onPressed: () async {
                            final ok = await _confirmDelete(sheetCtx, t);
                            if (!ok) return;
                            await _deleteSibling(t);
                            if (!sheetCtx.mounted) return;
                            if (_siblings.length < 2) {
                              Navigator.pop(sheetCtx, _current);
                            } else {
                              setSheet(() {});
                            }
                          },
                        ),
                  onTap: () => Navigator.pop(sheetCtx, t),
                ),
            ],
          ),
        ),
      ),
    );
    if (picked == null || !mounted || picked.id == _current.id) return;
    setState(() {
      _current = picked;
      _title = picked.title.isEmpty ? _title : picked.title;
      _pinned = picked.pinned;
      _messages = const [];
      _loading = true;
      _lastSpokenId = '';
    });
    await _refresh();
  }

  Future<void> _newSiblingThread() async {
    final name = await showDialog<String>(
      context: context,
      builder: (ctx) {
        final c = TextEditingController();
        return AlertDialog(
          title: const Text('New chat'),
          content: TextField(
            controller: c,
            autofocus: true,
            textCapitalization: TextCapitalization.sentences,
            decoration: const InputDecoration(
              labelText: 'Name',
              hintText: 'e.g. Release checks',
            ),
            onSubmitted: (v) => Navigator.pop(ctx, v),
          ),
          actions: [
            TextButton(
                onPressed: () => Navigator.pop(ctx),
                child: const Text('Cancel')),
            FilledButton(
                onPressed: () => Navigator.pop(ctx, c.text),
                child: const Text('Create')),
          ],
        );
      },
    );
    if (name == null || name.trim().isEmpty || !mounted) return;

    final messenger = ScaffoldMessenger.of(context);
    try {
      // A new chat made from an everyone-channel is another everyone-channel,
      // not a group that happens to contain today's bots. Sending the roster
      // as a member list made a thread that grouped separately from the
      // broadcast and silently excluded every bot added afterwards.
      final everyone = _current.isEveryone;

      final created = await ref.read(apiProvider).createConversation(
            title: name.trim(),
            members: everyone ? const [] : _current.members,
            kind: everyone ? 'broadcast' : '',
          );
      if (!mounted) return;
      setState(() {
        _siblings = [created, ..._siblings];
        _current = created;
        _title = created.title;
        _pinned = false;
        _messages = const [];
        _loading = true;
        _lastSpokenId = '';
      });
      await _refresh();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
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
            _current.isPair
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
                hintText: _current.isPair
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
                : const Icon(Icons.send_rounded, size: 18, semanticLabel: 'Send'),
          ),
        ],
      ),
    );
  }
}
