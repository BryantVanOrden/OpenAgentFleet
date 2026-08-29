import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';

/// The fleet's group chat.
///
/// This is the same peer bus agents use for message_peer and delegate_task, so
/// what you see here is the actual conversation between bots rather than a
/// summary of it — and anything you send lands in their inbox on their next
/// turn. It lives under Fleet, not Vault: a conversation between agents is not
/// a credential store, and filing it there made it invisible.
///
/// Agents are always reachable, so there is no team to assemble first. Sending
/// with no recipient broadcasts to everyone.
class CommsScreen extends ConsumerStatefulWidget {
  const CommsScreen({super.key});

  @override
  ConsumerState<CommsScreen> createState() => _CommsScreenState();
}

class _CommsScreenState extends ConsumerState<CommsScreen> {
  final _controller = TextEditingController();
  final _scroll = ScrollController();

  List<PeerMessage> _messages = const [];
  String _to = 'broadcast';
  bool _busy = false;
  bool _loading = true;
  String? _error;
  Timer? _poll;

  @override
  void initState() {
    super.initState();
    _refresh();
    // The peer bus has no websocket topic of its own, so this polls. Five
    // seconds keeps a conversation feeling live without hammering the API.
    _poll = Timer.periodic(const Duration(seconds: 5), (_) => _refresh());
  }

  @override
  void dispose() {
    _poll?.cancel();
    _controller.dispose();
    _scroll.dispose();
    super.dispose();
  }

  Future<void> _refresh() async {
    try {
      final list = await ref.read(apiProvider).peerMessages();
      if (!mounted) return;
      setState(() {
        // The API returns newest first; a conversation reads oldest first.
        _messages = list.reversed.toList();
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

  Future<void> _send() async {
    final text = _controller.text.trim();
    if (text.isEmpty) return;
    setState(() => _busy = true);
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(apiProvider).sendPeerMessage(
            content: text,
            toInstanceId: _to,
          );
      _controller.clear();
      await _refresh();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    } finally {
      if (mounted) setState(() => _busy = false);
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
                'What your agents say to each other. Anything you send arrives '
                'in their inbox on the next step.',
                style: TextStyle(color: Fleet.ink400, fontSize: 11),
              ),
            ),
          ),
        ),
      ),
      body: Column(
        children: [
          Expanded(child: _buildList()),
          _buildComposer(instances),
        ],
      ),
    );
  }

  Widget _buildList() {
    if (_loading) {
      return const Center(child: CircularProgressIndicator());
    }
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
    if (_messages.isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.forum_outlined, size: 34, color: Fleet.ink600),
              const SizedBox(height: 12),
              Text(
                'Nothing said yet.\n\nAgents message each other here as they '
                'work — no group has to be set up. You can start the '
                'conversation below.',
                textAlign: TextAlign.center,
                style: TextStyle(color: Fleet.ink400, fontSize: 13, height: 1.4),
              ),
            ],
          ),
        ),
      );
    }

    return ListView.builder(
      controller: _scroll,
      padding: const EdgeInsets.all(12),
      itemCount: _messages.length,
      itemBuilder: (_, i) => _MessageTile(message: _messages[i]),
    );
  }

  Widget _buildComposer(List<Instance> instances) {
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Fleet.ink900,
        border: Border(top: BorderSide(color: Fleet.ink800)),
      ),
      child: Column(
        children: [
          Row(
            children: [
              Icon(Icons.alternate_email, size: 14, color: Fleet.ink400),
              const SizedBox(width: 6),
              Expanded(
                child: DropdownButtonHideUnderline(
                  child: DropdownButton<String>(
                    value: _to,
                    isDense: true,
                    isExpanded: true,
                    style: TextStyle(color: Fleet.ink200, fontSize: 12),
                    dropdownColor: Fleet.ink850,
                    items: [
                      const DropdownMenuItem(
                          value: 'broadcast',
                          child: Text('Everyone (broadcast)')),
                      for (final i in instances)
                        DropdownMenuItem(value: i.id, child: Text(i.name)),
                    ],
                    onChanged: (v) => setState(() => _to = v ?? 'broadcast'),
                  ),
                ),
              ),
            ],
          ),
          const SizedBox(height: 4),
          Row(
            children: [
              Expanded(
                child: TextField(
                  controller: _controller,
                  enabled: !_busy,
                  minLines: 1,
                  maxLines: 3,
                  textCapitalization: TextCapitalization.sentences,
                  decoration: const InputDecoration(
                    hintText: 'Say something to the fleet',
                    contentPadding:
                        EdgeInsets.symmetric(horizontal: 12, vertical: 10),
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
                        child: CircularProgressIndicator(strokeWidth: 2),
                      )
                    : const Icon(Icons.send_rounded, size: 18),
              ),
            ],
          ),
        ],
      ),
    );
  }
}

class _MessageTile extends StatelessWidget {
  const _MessageTile({required this.message});
  final PeerMessage message;

  @override
  Widget build(BuildContext context) {
    final broadcast = message.toInstanceId == 'broadcast';
    // Operator messages are the ones you sent; showing them aligned like your
    // own chat makes the thread readable at a glance.
    final mine = message.fromInstanceName.toLowerCase().contains('operator');

    final kindColour = switch (message.kind) {
      'delegation' => Fleet.warn,
      'question' => Fleet.cool,
      _ => Fleet.ink300,
    };

    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Row(
        mainAxisAlignment:
            mine ? MainAxisAlignment.end : MainAxisAlignment.start,
        children: [
          Flexible(
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 9),
              decoration: BoxDecoration(
                color: mine ? Fleet.live.withValues(alpha: 0.14) : Fleet.ink800,
                borderRadius: BorderRadius.circular(12),
                border: mine
                    ? Border.all(color: Fleet.live.withValues(alpha: 0.3))
                    : null,
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Row(
                    mainAxisSize: MainAxisSize.min,
                    children: [
                      Flexible(
                        child: Text(
                          broadcast
                              ? '${message.fromInstanceName} → everyone'
                              : '${message.fromInstanceName} → ${message.toInstanceId}',
                          overflow: TextOverflow.ellipsis,
                          style: const TextStyle(
                              fontSize: 11, fontWeight: FontWeight.w700),
                        ),
                      ),
                      const SizedBox(width: 6),
                      Container(
                        padding: const EdgeInsets.symmetric(
                            horizontal: 5, vertical: 1),
                        decoration: BoxDecoration(
                          color: kindColour.withValues(alpha: 0.16),
                          borderRadius: BorderRadius.circular(4),
                        ),
                        child: Text(
                          message.kind,
                          style: TextStyle(fontSize: 9, color: kindColour),
                        ),
                      ),
                    ],
                  ),
                  const SizedBox(height: 5),
                  Text(message.content,
                      style: const TextStyle(fontSize: 13, height: 1.35)),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}
