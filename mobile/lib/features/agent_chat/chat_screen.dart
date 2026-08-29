import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/theme/theme_controller.dart';
import '../../core/voice/voice_service.dart';
import '../settings/voice_settings_card.dart';

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
  final _voice = VoiceService();
  bool _busy = false;
  bool _listening = false;

  /// Read replies aloud. Off by default: someone triaging on a train does not
  /// want the agent talking, and this is a preference per session rather than
  /// per message.
  bool _speakReplies = false;

  /// Guards against re-reading the same reply when the provider rebuilds for
  /// an unrelated reason.
  String _lastSpokenId = '';

  @override
  void dispose() {
    _controller.dispose();
    _scroll.dispose();
    _voice.dispose();
    super.dispose();
  }

  /// Dictate into the message box. The transcript lands in the same field the
  /// keyboard fills, so speaking is just another way to compose — the user
  /// still chooses Ask or Run, and can edit a misheard word first.
  Future<void> _dictate() async {
    if (_listening) {
      await _voice.stopListening();
      if (mounted) setState(() => _listening = false);
      return;
    }

    setState(() => _listening = true);
    final heard = await _voice.listenOnce(
      onPartial: (words) {
        if (mounted) _controller.text = words;
      },
    );
    if (!mounted) return;
    setState(() => _listening = false);

    if (heard == null || heard.isEmpty) {
      final why = _voice.lastError.isEmpty
          ? 'Nothing was heard.'
          : 'Could not listen: ${_voice.lastError}';
      ScaffoldMessenger.of(context)
          .showSnackBar(SnackBar(content: Text(why)));
      return;
    }
    _controller.text = heard;
    _controller.selection =
        TextSelection.collapsed(offset: _controller.text.length);
  }

  Future<void> _send({required String mode}) async {
    final text = _controller.text.trim();
    if (text.isEmpty) return;

    if (mode == 'task') {
      final confirmed = await showDialog<bool>(
        context: context,
        builder: (context) => AlertDialog(
          backgroundColor: Fleet.ink850,
          title: const Text('Run as a task?'),
          content: Text(
            'The agent will start acting on this machine straight away:\n\n"$text"',
            style: TextStyle(color: Fleet.ink300),
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
      await ref
          .read(apiProvider)
          .sendChat(widget.instanceId, text, mode: mode);
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
    // Speaking happens here rather than after sendChat: that call returns
    // nothing, and the agent's reply only exists once the chat reloads.
    ref.listen(chatProvider(widget.instanceId), (_, next) {
      if (!_speakReplies) return;
      final list = next.valueOrNull;
      if (list == null || list.isEmpty) return;
      final latest = list.last;
      if (latest.isUser || latest.id == _lastSpokenId) return;
      _lastSpokenId = latest.id;
      // Pick up the speed and voice chosen in settings rather than whatever
      // the engine defaults to.
      unawaited(applyStoredVoiceSettings(
              _voice, ref.read(sharedPreferencesProvider))
          .then((_) => _voice.speak(latest.body)));
    });

    final messages = ref.watch(chatProvider(widget.instanceId));

    return Column(
      children: [
        Expanded(
          child: messages.when(
            loading: () => const Center(child: CircularProgressIndicator()),
            error: (err, _) =>
                Center(child: Text('$err', style: TextStyle(color: Fleet.bad))),
            data: (list) {
              if (list.isEmpty) {
                return Center(
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
                itemBuilder: (context, i) =>
                    _Bubble(message: list[i], instanceId: widget.instanceId),
              );
            },
          ),
        ),
        SafeArea(
          top: false,
          child: Container(
            padding: const EdgeInsets.fromLTRB(12, 8, 12, 12),
            decoration: BoxDecoration(
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
                    hintText: _listening
                        ? 'Listening...'
                        : widget.enabled
                            ? 'Talk to this agent'
                            : 'Instance is not running',
                  ),
                ),
                const SizedBox(height: 8),
                Row(
                  children: [
                    // Dictate. Voice is a way to talk to this agent, not a
                    // separate place, so it sits on the agent's own composer.
                    IconButton(
                      onPressed: widget.enabled && !_busy ? _dictate : null,
                      tooltip: _listening ? 'Stop dictating' : 'Dictate',
                      icon: Icon(
                        _listening ? Icons.mic : Icons.mic_none_outlined,
                        color: _listening ? Fleet.good : null,
                      ),
                    ),
                    IconButton(
                      onPressed: () {
                        setState(() => _speakReplies = !_speakReplies);
                        if (!_speakReplies) unawaited(_voice.stopSpeaking());
                      },
                      tooltip: _speakReplies
                          ? 'Stop reading replies aloud'
                          : 'Read replies aloud',
                      icon: Icon(
                        _speakReplies
                            ? Icons.volume_up_rounded
                            : Icons.volume_off_outlined,
                        color: _speakReplies ? Fleet.good : null,
                      ),
                    ),
                    Expanded(
                      child: OutlinedButton.icon(
                        onPressed: widget.enabled && !_busy
                            ? () => _send(mode: 'plan')
                            : null,
                        icon: const Icon(Icons.checklist_rtl, size: 18),
                        label: const Text('Plan'),
                      ),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: FilledButton.icon(
                        onPressed: widget.enabled && !_busy
                            ? () => _send(mode: 'chat')
                            : null,
                        icon: const Icon(Icons.send_rounded, size: 18),
                        label: const Text('Send'),
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

class _Bubble extends ConsumerStatefulWidget {
  const _Bubble({required this.message, required this.instanceId});
  final ChatMessage message;
  final String instanceId;

  @override
  ConsumerState<_Bubble> createState() => _BubbleState();
}

class _BubbleState extends ConsumerState<_Bubble> {
  bool _busy = false;

  Future<void> _answerPlan(bool approve) async {
    final msg = widget.message;
    setState(() => _busy = true);
    final messenger = ScaffoldMessenger.of(context);
    try {
      final api = ref.read(apiProvider);
      if (approve) {
        await api.approvePlan(widget.instanceId, msg.id);
      } else {
        await api.discardPlan(widget.instanceId, msg.id);
      }
      ref.invalidate(chatProvider(widget.instanceId));
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final message = widget.message;
    final mine = message.isUser;
    return Padding(
      padding: const EdgeInsets.only(bottom: 10),
      child: Row(
        mainAxisAlignment:
            mine ? MainAxisAlignment.end : MainAxisAlignment.start,
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
                  if (message.kind == 'plan') ...[
                    Row(
                      children: [
                        Icon(Icons.checklist_rtl,
                            size: 14, color: Fleet.live),
                        const SizedBox(width: 6),
                        Text('Proposed plan',
                            style: TextStyle(
                                color: Fleet.live,
                                fontSize: 11,
                                fontWeight: FontWeight.w700)),
                      ],
                    ),
                    const SizedBox(height: 6),
                  ],
                  Text(message.body,
                      style: const TextStyle(fontSize: 14, height: 1.35)),
                  // Only an unanswered plan offers the buttons; once approved
                  // or discarded it is history, and re-approving would start
                  // the same work twice.
                  if (message.isOpenPlan) ...[
                    const SizedBox(height: 10),
                    Row(
                      children: [
                        Expanded(
                          child: OutlinedButton(
                            onPressed: _busy ? null : () => _answerPlan(false),
                            child: const Text('Discard'),
                          ),
                        ),
                        const SizedBox(width: 8),
                        Expanded(
                          child: FilledButton.icon(
                            onPressed: _busy ? null : () => _answerPlan(true),
                            icon: _busy
                                ? const SizedBox(
                                    width: 14,
                                    height: 14,
                                    child: CircularProgressIndicator(
                                        strokeWidth: 2),
                                  )
                                : const Icon(Icons.play_arrow_rounded,
                                    size: 18),
                            label: const Text('Approve'),
                          ),
                        ),
                      ],
                    ),
                  ] else if (message.kind == 'plan') ...[
                    const SizedBox(height: 6),
                    Text(
                      message.planState == 'approved'
                          ? 'Approved — this became a task.'
                          : 'Discarded.',
                      style: TextStyle(color: Fleet.ink400, fontSize: 11),
                    ),
                  ],
                  const SizedBox(height: 4),
                  Text(
                    humanAgo(message.createdAt),
                    style: TextStyle(color: Fleet.ink400, fontSize: 10),
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
