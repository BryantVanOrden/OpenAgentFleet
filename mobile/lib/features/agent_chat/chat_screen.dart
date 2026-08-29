import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/theme/theme_controller.dart';
import '../../core/voice/voice_service.dart';
import '../../core/voice/voice_prefs.dart';
import 'chat_sessions_sheet.dart';

/// Talking to a machine.
///
/// Two send buttons, deliberately. "Ask" looks at the current screen and answers
/// without touching anything. "Run" turns the message into a real autonomous
/// task. Collapsing those into one button is how you end up with an agent
/// clicking Deploy because you asked whether it was ready to deploy.
class ChatScreen extends ConsumerStatefulWidget {
  const ChatScreen({
    super.key,
    required this.instanceId,
    this.instanceName = 'this bot',
    this.enabled = true,
    this.voice = '',
  });

  /// Shown when managing chats, so a confirmation names the bot rather than
  /// saying "this bot".
  final String instanceName;

  /// The voice this agent speaks in, from its own settings. Empty falls back
  /// to the app-wide default.
  final String voice;

  final String instanceId;
  final bool enabled;

  @override
  ConsumerState<ChatScreen> createState() => _ChatScreenState();
}

class _ChatScreenState extends ConsumerState<ChatScreen> {
  /// Which chat with this bot is open. Empty is the original chat, which is
  /// where history from before chats could be separated lives.
  String _chatId = '';
  String _chatTitle = '';

  ChatRef get _chatRef => (instanceId: widget.instanceId, chatId: _chatId);

  /// Where the last-open chat is remembered, per bot. Coming back to an agent
  /// should return you to the conversation you were having with it, not to
  /// whichever chat happens to be oldest.
  String get _lastChatKey => 'chat.last.${widget.instanceId}';

  final _controller = TextEditingController();
  final _scroll = ScrollController();
  late final VoiceService _voice = VoiceService(api: ref.read(apiProvider));
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
  void initState() {
    super.initState();
    unawaited(_restoreLastChat());
  }

  @override
  void dispose() {
    _controller.dispose();
    _scroll.dispose();
    _voice.dispose();
    super.dispose();
  }

  /// Read a reply aloud in this agent's own voice.
  ///
  /// The server voice is chosen per agent and wins when one is set; the device
  /// settings are still applied underneath so the fallback sounds right too.
  Future<void> _speakReply(String body) async {
    final prefs = ref.read(sharedPreferencesProvider);
    final chosen = widget.voice;
    _voice.useServerVoice(
      chosen.isEmpty ? null : chosen,
      speed: (prefs.getDouble('voice.rate') ?? 0.5) / 0.5,
    );
    await applyStoredVoiceSettings(_voice, prefs);
    await _voice.speak(body);
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
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(content: Text(why)));
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
          .sendChat(widget.instanceId, text, mode: mode, chatId: _chatId);
      _controller.clear();
      ref.invalidate(chatProvider(_chatRef));
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// Reopen whichever chat was last in use with this bot.
  ///
  /// The screen is rebuilt whenever you switch tabs or come back to an agent,
  /// and defaulting to the original chat meant every return dropped you into
  /// the oldest conversation rather than the one you were just having.
  Future<void> _restoreLastChat() async {
    final prefs = ref.read(sharedPreferencesProvider);
    final remembered = prefs.getString(_lastChatKey);

    List<ChatSession> sessions;
    try {
      sessions = await ref.read(apiProvider).chatSessions(widget.instanceId);
    } catch (_) {
      // Offline or the server is down: the original chat still renders from
      // whatever the chat provider can fetch, so this is not worth an error.
      return;
    }
    if (!mounted || sessions.isEmpty) return;

    // A remembered chat that has since been deleted must not strand the screen
    // on an empty conversation.
    ChatSession? target;
    if (remembered != null) {
      target = sessions.where((c) => c.id == remembered).firstOrNull;
    }
    // Nothing remembered: fall back to genuinely most recent activity rather
    // than the list order, which puts pinned chats first.
    target ??= _mostRecent(sessions);
    if (target == null) return;

    setState(() {
      _chatId = target!.isDefault ? '' : target.id;
      _chatTitle = target.displayTitle;
    });
  }

  ChatSession? _mostRecent(List<ChatSession> sessions) {
    ChatSession? best;
    for (final c in sessions) {
      final at = c.lastMessageAt;
      if (at == null) continue;
      if (best?.lastMessageAt == null || at.isAfter(best!.lastMessageAt!)) {
        best = c;
      }
    }
    return best ?? sessions.first;
  }

  /// Remember the open chat so returning to this bot resumes it.
  void _rememberChat(String id) {
    // The original chat is stored under its own name rather than as an empty
    // string, so "the earlier chat, deliberately" is distinguishable from
    // "nothing chosen yet".
    ref
        .read(sharedPreferencesProvider)
        .setString(_lastChatKey, id.isEmpty ? ChatSession.defaultId : id);
  }

  /// The chat you are in, and the way to get to the others.
  ///
  /// A bot holds several separate chats and each is its own context, so which
  /// one you are in changes what the agent can see — that has to be on screen,
  /// not buried in a menu.
  Widget _chatBar() {
    return InkWell(
      onTap: _manageChats,
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
                _chatTitle.isEmpty ? 'Chat' : _chatTitle,
                overflow: TextOverflow.ellipsis,
                style: const TextStyle(
                    fontSize: 12.5, fontWeight: FontWeight.w600),
              ),
            ),
            IconButton(
              tooltip: 'New chat',
              visualDensity: VisualDensity.compact,
              icon: Icon(Icons.add_comment_outlined,
                  size: 18, color: Fleet.ink300),
              onPressed: _startNewChat,
            ),
            Icon(Icons.expand_more, size: 18, color: Fleet.ink400),
          ],
        ),
      ),
    );
  }

  Future<void> _manageChats() async {
    final picked = await ChatSessionsSheet.show(
      context,
      instanceId: widget.instanceId,
      instanceName: widget.instanceName,
      activeChatId: _chatId,
    );
    if (picked == null || !mounted) return;
    setState(() {
      _chatId = picked.id == ChatSession.defaultId ? '' : picked.id;
      _chatTitle = picked.displayTitle;
      _lastSpokenId = '';
    });
    _rememberChat(_chatId);
    ref.invalidate(chatProvider(_chatRef));
  }

  Future<void> _startNewChat() async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      final created =
          await ref.read(apiProvider).createChatSession(widget.instanceId);
      if (!mounted) return;
      setState(() {
        _chatId = created.id;
        _chatTitle = created.displayTitle;
        _lastSpokenId = '';
      });
      _rememberChat(_chatId);
      ref.invalidate(chatProvider(_chatRef));
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  @override
  Widget build(BuildContext context) {
    // Speaking happens here rather than after sendChat: that call returns
    // nothing, and the agent's reply only exists once the chat reloads.
    ref.listen(chatProvider(_chatRef), (_, next) {
      if (!_speakReplies) return;
      final list = next.valueOrNull;
      if (list == null || list.isEmpty) return;
      final latest = list.last;
      if (latest.isUser || latest.id == _lastSpokenId) return;
      _lastSpokenId = latest.id;
      unawaited(_speakReply(latest.body));
    });

    final messages = ref.watch(chatProvider(_chatRef));

    return Column(
      children: [
        _chatBar(),
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
                    _Bubble(
                        message: list[i],
                        instanceId: widget.instanceId,
                        chatId: _chatId),
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
  const _Bubble({
    required this.message,
    required this.instanceId,
    required this.chatId,
  });
  final ChatMessage message;
  final String instanceId;

  /// Which chat this bubble is in, so approving a plan refreshes that chat
  /// rather than the bot's original one.
  final String chatId;

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
      ref.invalidate(chatProvider(
          (instanceId: widget.instanceId, chatId: widget.chatId)));
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
                        Icon(Icons.checklist_rtl, size: 14, color: Fleet.live),
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
