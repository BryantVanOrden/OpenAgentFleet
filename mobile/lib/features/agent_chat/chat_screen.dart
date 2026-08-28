import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';

/// Talking to a machine.
///
/// Two send buttons, deliberately. "Ask" looks at the current screen and answers
/// without touching anything. "Run" turns the message into a real autonomous
/// task. Collapsing those into one button is how you end up with an agent
/// clicking Deploy because you asked whether it was ready to deploy.
class ChatScreen extends ConsumerStatefulWidget {
  const ChatScreen({super.key, required this.instanceId, this.enabled = true});

  final String instanceId;
  final bool enabled;

  @override
  ConsumerState<ChatScreen> createState() => _ChatScreenState();
}

class _ChatScreenState extends ConsumerState<ChatScreen> {
  final _controller = TextEditingController();
  final _scroll = ScrollController();
  bool _busy = false;

  @override
  void dispose() {
    _controller.dispose();
    _scroll.dispose();
    super.dispose();
  }

  Future<void> _send({required bool asTask}) async {
    final text = _controller.text.trim();
    if (text.isEmpty) return;

    if (asTask) {
      final confirmed = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          backgroundColor: Fleet.ink850,
          title: const Text('Run as a task?'),
          content: Text(
            'The agent will start acting on this machine straight away:\n\n"$text"',
            style: const TextStyle(color: Fleet.ink300),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(context, false),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(context, true),
              child: const Text('Start'),
            ),
          ],
        ),
      );
      if (confirmed != true) return;
    }

    if (!mounted) return;
    setState(() => _busy = true);
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(apiProvider).sendChat(widget.instanceId, text, asTask: asTask);
      _controller.clear();
      ref.invalidate(chatProvider(widget.instanceId));
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final messages = ref.watch(chatProvider(widget.instanceId));

    return Column(
      children: [
        Expanded(
          child: messages.when(
            loading: () => const Center(child: CircularProgressIndicator()),
            error: (err, _) =>
                Center(child: Text('$err', style: const TextStyle(color: Fleet.bad))),
            data: (list) {
              if (list.isEmpty) {
                return const Center(
                  child: Padding(
                    padding: EdgeInsets.all(32),
                    child: Text(
                      'Ask what is happening on this machine, or send an\n'
                      'instruction and let the agent run with it.',
                      textAlign: TextAlign.center,
                      style: TextStyle(color: Fleet.ink400, fontSize: 13),
                    ),
                  ),
                );
              }
              WidgetsBinding.instance.addPostFrameCallback((_) {
                if (_scroll.hasClients) {
                  _scroll.animateTo(
                    _scroll.position.maxScrollExtent,
                    duration: const Duration(milliseconds: 250),
                    curve: Curves.easeOut,
                  );
                }
              });
              return ListView.builder(
                controller: _scroll,
                padding: const EdgeInsets.all(16),
                itemCount: list.length,
                itemBuilder: (context, i) => _Bubble(message: list[i]),
              );
            },
          ),
        ),

        SafeArea(
          top: false,
          child: Container(
            padding: const EdgeInsets.fromLTRB(12, 8, 12, 12),
            decoration: const BoxDecoration(
              color: Fleet.ink900,
              border: Border(top: BorderSide(color: Fleet.ink800)),
            ),
            child: Column(
              children: [
                TextField(
                  controller: _controller,
                  enabled: widget.enabled && !_busy,
                  minLines: 1,
                  maxLines: 4,
                  textCapitalization: TextCapitalization.sentences,
                  decoration: InputDecoration(
                    hintText: widget.enabled
                        ? 'What is on screen right now?'
                        : 'Instance is not running',
                  ),
                ),
                const SizedBox(height: 8),
                Row(
                  children: [
                    Expanded(
                      child: OutlinedButton.icon(
                        onPressed: widget.enabled && !_busy ? () => _send(asTask: false) : null,
                        icon: const Icon(Icons.visibility_outlined, size: 18),
                        label: const Text('Ask'),
                      ),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: FilledButton.icon(
                        onPressed: widget.enabled && !_busy ? () => _send(asTask: true) : null,
                        icon: const Icon(Icons.play_arrow_rounded, size: 20),
                        label: const Text('Run'),
                      ),
                    ),
                  ],
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

class _Bubble extends StatelessWidget {
  const _Bubble({required this.message});
  final ChatMessage message;

  @override
  Widget build(BuildContext context) {
    final mine = message.isUser;
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Row(
        mainAxisAlignment: mine ? MainAxisAlignment.end : MainAxisAlignment.start,
        children: [
          Flexible(
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
              decoration: BoxDecoration(
                color: mine ? Fleet.live.withValues(alpha: 0.15) : Fleet.ink800,
                borderRadius: BorderRadius.only(
                  topLeft: const Radius.circular(16),
                  topRight: const Radius.circular(16),
                  bottomLeft: Radius.circular(mine ? 16 : 4),
                  bottomRight: Radius.circular(mine ? 4 : 16),
                ),
                border: mine
                    ? Border.all(color: Fleet.live.withValues(alpha: 0.3))
                    : null,
              ),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.start,
                children: [
                  Text(message.body, style: const TextStyle(fontSize: 14, height: 1.35)),
                  const SizedBox(height: 4),
                  Text(
                    humanAgo(message.createdAt),
                    style: const TextStyle(color: Fleet.ink400, fontSize: 10),
                  ),
                ],
              ),
            ),
          ),
        ],
      ),
    );
  }
}
