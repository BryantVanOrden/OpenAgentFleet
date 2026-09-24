import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/markdown/markdown_lite.dart';
import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/voice/voice_service.dart';
import '../../core/widgets/inline_error.dart';
import '../../core/widgets/thinking.dart';
import '../fleet_comms/message_tile.dart';
import 'setup_card.dart';

/// Commands matching what has been typed so far, best match first.
///
/// [typed] is the composer text, slash included. A name that starts with the
/// prefix ranks above one that merely mentions it in its usage or description,
/// so "/st" puts /status on top even when three other commands describe
/// themselves as "showing status". An empty prefix is every command.
List<FleetCommand> filterCommands(List<FleetCommand> all, String typed) {
  final q = typed.trim().replaceFirst(RegExp(r'^/'), '').toLowerCase();
  if (q.isEmpty) return List.of(all);
  final byName = <FleetCommand>[];
  final byText = <FleetCommand>[];
  for (final c in all) {
    final name = c.name.toLowerCase();
    if (name.startsWith(q)) {
      byName.add(c);
    } else if (name.contains(q) ||
        c.usage.toLowerCase().contains(q) ||
        c.description.toLowerCase().contains(q)) {
      byText.add(c);
    }
  }
  return [...byName, ...byText];
}

/// The chat with the whole fleet.
///
/// The built-in broadcast channel, with two things the comms screen does not
/// have: slash commands, which replace the screens missions and status used to
/// need, and notes from Oaf — the platform itself telling you what happened.
class HomeChatScreen extends ConsumerStatefulWidget {
  const HomeChatScreen({super.key, this.onOpenSessions});

  /// Opens the sessions sheet (fleet, Oaf sessions, bot threads). Supplied by
  /// the home shell; absent when the screen is shown on its own.
  final VoidCallback? onOpenSessions;

  @override
  ConsumerState<HomeChatScreen> createState() => _HomeChatScreenState();
}

class _HomeChatScreenState extends ConsumerState<HomeChatScreen> {
  static const _quickActions = ['/bots', '/status', '/alerts', '/missions', '/help'];

  final _controller = TextEditingController();
  final _scroll = ScrollController();
  late final VoiceService _voice = VoiceService(api: ref.read(apiProvider));

  List<PeerMessage> _messages = const [];
  final _ephemera = <_Ephemeral>[];
  List<FleetCommand> _commands = const [];
  SetupStatus? _setup;
  final _composerFocus = FocusNode();
  String? _commandsError;

  bool _loading = true;
  bool _busy = false;
  /// When you last spoke to the fleet and nobody has answered yet; the
  /// reading bubble stays up until a bot's message newer than this arrives.
  DateTime? _awaitingSince;
  Timer? _awaitingTimeout;
  bool _listening = false;
  String? _error;
  Timer? _poll;

  /// Off by default: a fleet that talks constantly should not talk over you.
  bool _readAloud = false;

  /// The newest message seen on the last refresh. Anything after it on the
  /// next one is news; anything up to it is history, which is never spoken.
  String _seenLatestId = '';
  bool _everLoaded = false;
  final _speakQueue = <PeerMessage>[];
  bool _draining = false;
  String _speakingId = '';

  bool get _visible => ref.read(selectedTabProvider) == Tabs.chat;

  @override
  void initState() {
    super.initState();
    unawaited(_loadCommands());
    if (_visible) _start();
    // Both arrival and departure matter here: polling and reading aloud should
    // not carry on behind another tab.
    ref.listenManual(selectedTabProvider, (prev, next) {
      if (next == Tabs.chat && prev != Tabs.chat) {
        _start();
      } else if (next != Tabs.chat && prev == Tabs.chat) {
        _stop();
      }
    });
    _controller.addListener(() => setState(() {}));
  }

  @override
  void dispose() {
    _poll?.cancel();
    _awaitingTimeout?.cancel();
    _controller.dispose();
    _composerFocus.dispose();
    _scroll.dispose();
    _voice.dispose();
    super.dispose();
  }

  void _start() {
    // What arrived while you were on another tab is backlog, not news.
    unawaited(_refresh(speak: false));
    _poll?.cancel();
    _poll = Timer.periodic(const Duration(seconds: 5), (_) => _refresh());
  }

  void _stop() {
    _poll?.cancel();
    _poll = null;
    _speakQueue.clear();
    unawaited(_voice.stopSpeaking());
    if (mounted) setState(() => _speakingId = '');
  }

  Future<void> _loadCommands() async {
    try {
      final list = await ref.read(apiProvider).fleetCommands();
      if (!mounted) return;
      setState(() {
        _commands = list;
        _commandsError = null;
      });
    } catch (err) {
      if (mounted) setState(() => _commandsError = '$err');
    }
  }

  Future<void> _refresh({bool speak = true}) async {
    try {
      final api = ref.read(apiProvider);
      final list = await api.conversationMessages(Conversation.broadcastId);
      // What is still missing on a fresh deployment, fetched with the stream
      // so the card leaves the moment the step is done. Never fatal.
      final setup = await api.setup().then<SetupStatus?>((s) => s, onError: (_) => null);
      if (!mounted) return;
      final atBottom = !_scroll.hasClients ||
          _scroll.position.pixels >= _scroll.position.maxScrollExtent - 40;
      final fresh = _newSince(list);
      final since = _awaitingSince;
      final answered = since != null &&
          list.any((m) => m.fromInstanceId.isNotEmpty && m.createdAt.isAfter(since));
      setState(() {
        _messages = list;
        if (answered) _awaitingSince = null;
        if (setup != null) _setup = setup;
        _loading = false;
        _error = null;
      });
      if (atBottom) _scrollToEnd();
      if (speak) _enqueue(fresh);
    } catch (err) {
      if (!mounted) return;
      setState(() {
        _loading = false;
        _error = '$err';
      });
    }
  }

  /// Messages that were not there last time, and move the marker past them.
  List<PeerMessage> _newSince(List<PeerMessage> list) {
    if (!_everLoaded) {
      _everLoaded = true;
      _seenLatestId = list.lastOrNull?.id ?? '';
      return const [];
    }
    final idx = _seenLatestId.isEmpty
        ? -1
        : list.indexWhere((m) => m.id == _seenLatestId);
    // The marker missing from a non-empty history means the thread was
    // compacted underneath us; nothing in it can be told apart as new.
    final fresh = idx >= 0
        ? list.sublist(idx + 1)
        : (_seenLatestId.isEmpty ? list : const <PeerMessage>[]);
    if (list.isNotEmpty) _seenLatestId = list.last.id;
    return fresh;
  }

  void _scrollToEnd() {
    WidgetsBinding.instance.addPostFrameCallback((_) {
      if (_scroll.hasClients) {
        _scroll.jumpTo(_scroll.position.maxScrollExtent);
      }
    });
  }

  // ------------------------------------------------------------- speech ---

  void _enqueue(List<PeerMessage> fresh) {
    if (!_readAloud) return;
    for (final m in fresh) {
      // Your own words are not read back; Oaf's notes and compaction markers
      // are the platform's, and have no voice to be read in.
      if (m.fromInstanceId.isEmpty ||
          m.fromInstanceId == 'system' ||
          m.kind == 'system' ||
          m.kind == 'summary') {
        continue;
      }
      _speakQueue.add(m);
    }
    unawaited(_drain());
  }

  /// Speak queued messages one at a time, each in its sender's own voice.
  Future<void> _drain() async {
    if (_draining) return;
    _draining = true;
    try {
      while (_speakQueue.isNotEmpty && _readAloud && mounted) {
        final m = _speakQueue.removeAt(0);
        final instances = ref.read(instancesProvider).valueOrNull ?? const [];
        final sender =
            instances.where((i) => i.id == m.fromInstanceId).firstOrNull;
        _voice.useServerVoice(
          (sender?.voice.isNotEmpty ?? false) ? sender!.voice : null,
          speed: (sender?.voiceSpeed ?? 0) != 0 ? sender!.voiceSpeed : 1.0,
        );
        setState(() => _speakingId = m.id);
        await _voice.speak(m.content);
      }
    } finally {
      _draining = false;
      if (mounted) setState(() => _speakingId = '');
    }
  }

  void _toggleReadAloud() {
    setState(() => _readAloud = !_readAloud);
    if (!_readAloud) {
      _speakQueue.clear();
      unawaited(_voice.stopSpeaking());
      setState(() => _speakingId = '');
    }
  }

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

  // ------------------------------------------------------------ sending ---

  Future<void> _send() async {
    final text = _controller.text.trim();
    if (text.isEmpty) return;
    // A tap that does something should feel like it did.
    unawaited(HapticFeedback.selectionClick());
    if (text.startsWith('/')) {
      await _runCommand(text);
      return;
    }
    setState(() => _busy = true);
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(apiProvider).sendPeerMessage(
            content: text,
            toInstanceId: Conversation.broadcastId,
            conversationId: Conversation.broadcastId,
          );
      _controller.clear();
      _awaitingSince = DateTime.now();
      _awaitingTimeout?.cancel();
      _awaitingTimeout = Timer(const Duration(minutes: 2), () {
        if (mounted) setState(() => _awaitingSince = null);
      });
      await _refresh();
      _scrollToEnd();
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _runCommand(String text) async {
    unawaited(HapticFeedback.selectionClick());
    setState(() => _busy = true);
    final messenger = ScaffoldMessenger.of(context);
    try {
      final result = await ref.read(apiProvider).runFleetCommand(text);
      if (!mounted) return;
      setState(() {
        if (_controller.text.trim() == text) _controller.clear();
        _ephemera.add(_Ephemeral(
          afterId: _messages.lastOrNull?.id ?? '',
          result: result,
        ));
      });
      _scrollToEnd();
      // A command that changed something usually also said something into
      // the channel; fetch now rather than waiting out the poll.
      if (result.ok) unawaited(_refresh());
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// Put a command's usage in the field, with the placeholder after the name
  /// selected so typing the arguments replaces it.
  void _insertCommand(FleetCommand c) {
    final usage = c.usage.isEmpty ? '/${c.name}' : c.usage;
    final nameEnd = usage.indexOf(' ');
    _controller.value = TextEditingValue(
      text: usage,
      selection: nameEnd < 0
          ? TextSelection.collapsed(offset: usage.length)
          : TextSelection(baseOffset: nameEnd + 1, extentOffset: usage.length),
    );
  }

  // -------------------------------------------------------------- build ---

  bool get _showCommands {
    final t = _controller.text;
    return t.startsWith('/') && !t.contains(RegExp(r'\s'));
  }

  @override
  Widget build(BuildContext context) {
    final instances = ref.watch(instancesProvider).valueOrNull ?? const [];
    var working = 0;
    for (final i in instances) {
      final tasks = ref.watch(tasksProvider(i.id)).valueOrNull ?? const [];
      if (tasks.any((t) => t.isLive)) working++;
    }
    final role = ref.watch(meProvider).valueOrNull?.role ?? '';
    final readOnly = role == 'viewer' || role == 'auditor';

    return Scaffold(
      appBar: AppBar(
        leading: widget.onOpenSessions == null
            ? null
            : IconButton(
                icon: const Icon(Icons.menu_rounded),
                tooltip: 'Sessions',
                onPressed: widget.onOpenSessions,
              ),
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('Fleet chat', style: TextStyle(fontSize: 16)),
            Text(
              '${instances.length} agent${instances.length == 1 ? '' : 's'}'
              ' · $working working',
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
            onPressed: _toggleReadAloud,
          ),
          IconButton(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: () => _refresh(speak: false),
          ),
        ],
      ),
      body: Column(
        children: [
          Expanded(child: _buildStream(readOnly: readOnly)),
          _quickActionRow(enabled: !readOnly && !_busy),
          if (_showCommands && !readOnly) _commandPanel(),
          _composer(readOnly: readOnly),
        ],
      ),
    );
  }

  Widget _buildStream({required bool readOnly}) {
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_error != null && _messages.isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(24),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Text('Could not reach the fleet: $_error',
                  textAlign: TextAlign.center,
                  style: TextStyle(color: Fleet.bad, fontSize: 12)),
              const SizedBox(height: 14),
              OutlinedButton(
                  onPressed: () => _refresh(speak: false),
                  child: const Text('Retry')),
            ],
          ),
        ),
      );
    }
    final setup = _setup;
    final card = (setup != null && !setup.ready)
        ? SetupCard(
            status: setup,
            busy: _busy,
            readOnly: readOnly,
            onRun: _runCommand,
            onFocusComposer: _composerFocus.requestFocus,
            leading: const OafAvatar(size: 44),
          )
        : null;
    if (_messages.isEmpty && _ephemera.isEmpty) {
      if (card == null) return const _EmptyState();
      return ListView(children: [card, const _EmptyState()]);
    }

    final items = _items();
    // Presence, under the newest message: who is mid-run right now, and the
    // fleet reading a message you just sent. Not messages; the channel's
    // status strip, so it is never in the history.
    final presence = _presence();
    return ListView.builder(
      controller: _scroll,
      padding: const EdgeInsets.all(12),
      itemCount: items.length + (card == null ? 0 : 1) + (presence == null ? 0 : 1),
      itemBuilder: (_, i) {
        if (card != null) {
          if (i == 0) return card;
          i -= 1;
        }
        if (i == items.length) return presence!;
        final item = items[i];
        if (item is _Ephemeral) {
          return CommandResultCard(
            result: item.result,
            onDismiss: () => setState(() => _ephemera.remove(item)),
          );
        }
        final m = item as PeerMessage;
        return MessageEnter(
          key: ValueKey(m.id),
          child: FleetMessageTile(message: m, speaking: m.id == _speakingId),
        );
      },
    );
  }

  Widget? _presence() {
    final lines = <Widget>[];
    if (_busy || _awaitingSince != null) {
      lines.add(const ThinkingBubble(who: 'The fleet', hint: 'reading your message', compact: true));
    }
    final instances = ref.watch(instancesProvider).valueOrNull ?? const <Instance>[];
    for (final i in instances) {
      final tasks = ref.watch(tasksProvider(i.id)).valueOrNull ?? const <Task>[];
      for (final t in tasks.where((t) => t.isLive)) {
        lines.add(WorkingLine(name: i.name, step: t.step, maxSteps: t.maxSteps, goal: t.goal));
      }
    }
    if (lines.isEmpty) return null;
    return Padding(
      padding: const EdgeInsets.only(top: 4),
      child: Column(crossAxisAlignment: CrossAxisAlignment.stretch, children: lines),
    );
  }

  /// Messages with command results slotted in after the message that was
  /// newest when each command ran. Anchoring on an id rather than a clock
  /// keeps a card where it appeared even when the phone and server disagree
  /// about the time; a card whose anchor was compacted away goes to the top,
  /// where the history it followed used to be.
  List<Object> _items() {
    final ids = {for (final m in _messages) m.id};
    final after = <String, List<_Ephemeral>>{};
    final head = <_Ephemeral>[];
    for (final e in _ephemera) {
      if (e.afterId.isNotEmpty && ids.contains(e.afterId)) {
        (after[e.afterId] ??= []).add(e);
      } else {
        head.add(e);
      }
    }
    return [
      ...head,
      for (final m in _messages) ...[m, ...?after[m.id]],
    ];
  }

  Widget _quickActionRow({required bool enabled}) {
    return SizedBox(
      height: 44,
      child: ListView(
        scrollDirection: Axis.horizontal,
        padding: const EdgeInsets.symmetric(horizontal: 12),
        children: [
          for (final a in _quickActions)
            Padding(
              padding: const EdgeInsets.only(right: 6),
              child: ActionChip(
                label: Text(a,
                    style: const TextStyle(
                        fontSize: 12, fontFamily: 'monospace')),
                visualDensity: VisualDensity.compact,
                onPressed: enabled ? () => _runCommand(a) : null,
              ),
            ),
        ],
      ),
    );
  }

  Widget _commandPanel() {
    final matches = filterCommands(_commands, _controller.text);
    return Container(
      constraints: const BoxConstraints(maxHeight: 220),
      margin: const EdgeInsets.fromLTRB(12, 0, 12, 4),
      decoration: BoxDecoration(
        color: Fleet.ink850,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: Fleet.ink700),
      ),
      child: _commandsError != null
          ? Padding(
              padding: const EdgeInsets.fromLTRB(12, 0, 12, 12),
              child: InlineError('Could not load commands: $_commandsError'),
            )
          : matches.isEmpty
              ? Padding(
                  padding: const EdgeInsets.all(14),
                  child: Text(
                    _commands.isEmpty
                        ? 'Loading commands…'
                        : 'No command matches. Try /help.',
                    style: TextStyle(color: Fleet.ink400, fontSize: 12),
                  ),
                )
              : ListView.builder(
                  shrinkWrap: true,
                  padding: const EdgeInsets.symmetric(vertical: 4),
                  itemCount: matches.length,
                  itemBuilder: (_, i) {
                    final c = matches[i];
                    return ListTile(
                      dense: true,
                      onTap: () => _insertCommand(c),
                      leading: Icon(
                        c.mutates
                            ? Icons.bolt_outlined
                            : Icons.info_outline,
                        size: 16,
                        color: c.mutates ? Fleet.warn : Fleet.ink400,
                      ),
                      title: Text(
                        c.usage.isEmpty ? '/${c.name}' : c.usage,
                        style: const TextStyle(
                            fontSize: 13, fontFamily: 'monospace'),
                      ),
                      subtitle: c.description.isEmpty
                          ? null
                          : Text(c.description,
                              maxLines: 2,
                              overflow: TextOverflow.ellipsis,
                              style: TextStyle(
                                  color: Fleet.ink400, fontSize: 11)),
                    );
                  },
                ),
    );
  }

  Widget _composer({required bool readOnly}) {
    final enabled = !readOnly && !_busy;
    return SafeArea(
      top: false,
      child: Container(
        padding: const EdgeInsets.fromLTRB(8, 8, 12, 12),
        decoration: BoxDecoration(
          color: Fleet.ink900,
          border: Border(top: BorderSide(color: Fleet.ink800)),
        ),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            IconButton(
              onPressed: enabled ? _dictate : null,
              tooltip: _listening ? 'Stop dictating' : 'Dictate',
              icon: Icon(
                _listening ? Icons.mic : Icons.mic_none_outlined,
                color: _listening ? Fleet.good : null,
              ),
            ),
            Expanded(
              child: TextField(
                controller: _controller,
                enabled: enabled,
                minLines: 1,
                maxLines: 4,
                // Sentence case would capitalise a slash command's first
                // argument; commands and names are typed as-is.
                textCapitalization: TextCapitalization.none,
                decoration: InputDecoration(
                  hintText: readOnly
                      ? 'Your role can read the fleet chat but not post'
                      : _listening
                          ? 'Listening...'
                          : 'Tell the fleet what you want done, or / for commands',
                  contentPadding: const EdgeInsets.symmetric(
                      horizontal: 12, vertical: 10),
                ),
                onSubmitted: enabled ? (_) => _send() : null,
              ),
            ),
            const SizedBox(width: 8),
            FilledButton(
              onPressed: enabled ? _send : null,
              child: _busy
                  ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2))
                  : const Icon(Icons.send_rounded, size: 18, semanticLabel: 'Send'),
            ),
          ],
        ),
      ),
    );
  }
}

/// A command result shown in the stream. Local to this device and this
/// session: the server does not file it into the channel, so a restart clears
/// it — which is right, since it described the fleet as it was when asked.
class _Ephemeral {
  _Ephemeral({required this.afterId, required this.result});

  /// The newest message when the command ran; empty when the stream was.
  final String afterId;
  final FleetCommandResult result;
}

/// One entry in the fleet stream: an Oaf card for the platform's own notes,
/// the ordinary tile for everything else.
class FleetMessageTile extends StatelessWidget {
  const FleetMessageTile(
      {super.key, required this.message, this.speaking = false});

  final PeerMessage message;
  final bool speaking;

  bool get isOafNote =>
      message.kind == 'system' || message.fromInstanceId == 'system';

  @override
  Widget build(BuildContext context) {
    if (isOafNote) return OafNoteCard(message: message);
    return MessageTile(message: message, speaking: speaking);
  }
}

/// A note from Open Agent Fleet itself, delivered by Oaf.
///
/// Set apart from agent messages so "the platform says a mission started" is
/// not mistaken for one bot's opinion of events.
class OafNoteCard extends StatelessWidget {
  const OafNoteCard({super.key, required this.message});

  final PeerMessage message;

  @override
  Widget build(BuildContext context) {
    return Container(
      margin: const EdgeInsets.only(bottom: 10),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      decoration: BoxDecoration(
        color: Fleet.ink850,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: Fleet.live.withValues(alpha: 0.35)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const OafAvatar(size: 30),
          const SizedBox(width: 10),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    const Text('Oaf',
                        style: TextStyle(
                            fontSize: 11, fontWeight: FontWeight.w700)),
                    const SizedBox(width: 6),
                    Container(
                      padding: const EdgeInsets.symmetric(
                          horizontal: 5, vertical: 1),
                      decoration: BoxDecoration(
                        color: Fleet.live.withValues(alpha: 0.16),
                        borderRadius: BorderRadius.circular(4),
                      ),
                      child: Text('note',
                          style: TextStyle(fontSize: 9, color: Fleet.live)),
                    ),
                    const Spacer(),
                    Text(humanAgo(message.createdAt),
                        style: TextStyle(color: Fleet.ink400, fontSize: 10)),
                  ],
                ),
                const SizedBox(height: 5),
                SelectionArea(
                  child: MarkdownLite(message.content,
                      baseStyle: const TextStyle(fontSize: 13, height: 1.35)),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}

/// The mascot, cropped round. Falls back to an icon rather than a broken image
/// if the asset is ever missing from a build.
class OafAvatar extends StatelessWidget {
  const OafAvatar({super.key, this.size = 30});

  final double size;

  @override
  Widget build(BuildContext context) {
    return ClipOval(
      child: SizedBox(
        width: size,
        height: size,
        child: Image.asset(
          'assets/branding/mascot.png',
          fit: BoxFit.cover,
          errorBuilder: (_, __, ___) =>
              Icon(Icons.smart_toy_outlined, size: size * 0.7),
        ),
      ),
    );
  }
}

/// What a slash command came back with, in the stream where it was asked.
class CommandResultCard extends StatelessWidget {
  const CommandResultCard({super.key, required this.result, this.onDismiss});

  final FleetCommandResult result;
  final VoidCallback? onDismiss;

  @override
  Widget build(BuildContext context) {
    final tone = result.ok ? Fleet.ink300 : Fleet.warn;
    return Container(
      margin: const EdgeInsets.only(bottom: 10),
      padding: const EdgeInsets.symmetric(horizontal: 12, vertical: 10),
      decoration: BoxDecoration(
        color: result.ok ? Fleet.ink850 : Fleet.warn.withValues(alpha: 0.08),
        borderRadius: BorderRadius.circular(10),
        border: Border.all(
            color: result.ok ? Fleet.ink700 : Fleet.warn.withValues(alpha: 0.45)),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(result.ok ? Icons.terminal : Icons.warning_amber_rounded,
                  size: 14, color: tone),
              const SizedBox(width: 6),
              Expanded(
                child: Text(
                  result.title.isEmpty ? result.command : result.title,
                  overflow: TextOverflow.ellipsis,
                  style: TextStyle(
                      fontSize: 11,
                      fontWeight: FontWeight.w700,
                      letterSpacing: 0.3,
                      color: tone),
                ),
              ),
              if (result.command.isNotEmpty && result.title.isNotEmpty)
                Text(result.command,
                    style: TextStyle(
                        fontSize: 10,
                        fontFamily: 'monospace',
                        color: Fleet.ink400)),
              if (onDismiss != null)
                InkWell(
                  onTap: onDismiss,
                  borderRadius: BorderRadius.circular(10),
                  child: Padding(
                    padding: const EdgeInsets.all(4),
                    child: Icon(Icons.close, size: 14, color: Fleet.ink400),
                  ),
                ),
            ],
          ),
          if (result.body.isNotEmpty) ...[
            const SizedBox(height: 6),
            SelectionArea(
              child: MarkdownLite(result.body,
                  baseStyle: const TextStyle(fontSize: 13, height: 1.35)),
            ),
          ],
        ],
      ),
    );
  }
}

class _EmptyState extends StatelessWidget {
  const _EmptyState();

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const OafAvatar(size: 96),
            const SizedBox(height: 18),
            const Text('Say something to the fleet.',
                style: TextStyle(fontSize: 16)),
            const SizedBox(height: 6),
            Text(
              'Name a bot to address it, or type / for commands.',
              textAlign: TextAlign.center,
              style: TextStyle(color: Fleet.ink400, fontSize: 13, height: 1.4),
            ),
          ],
        ),
      ),
    );
  }
}
