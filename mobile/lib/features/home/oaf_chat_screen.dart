import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:image_picker/image_picker.dart';

import '../../core/markdown/markdown_lite.dart';
import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/voice/voice_service.dart';
import 'home_chat_screen.dart' show OafAvatar;
import 'session_settings_sheet.dart';

/// One session with Oaf on the phone: the chat as an agent.
///
/// Oaf works in the session's folder on its device (your PC through
/// `fleetctl host`, or this phone for what a phone can do), runs fleet
/// commands, asks bots and hands the fleet work. Every tool call shows in the
/// thread as it happens. Attach photos from the camera roll; hold the mic to
/// dictate, or switch voice mode on to talk and hear the answers.
class OafChatScreen extends ConsumerStatefulWidget {
  const OafChatScreen({
    super.key,
    required this.session,
    required this.devices,
    required this.providers,
    required this.readOnly,
    required this.onSessionChanged,
    required this.onOpenSessions,
    this.startInVoiceMode = false,
  });

  final OafSession session;
  final List<OafDevice> devices;
  final List<AIProvider> providers;
  final bool readOnly;
  final ValueChanged<OafSession> onSessionChanged;
  final VoidCallback onOpenSessions;
  final bool startInVoiceMode;

  @override
  ConsumerState<OafChatScreen> createState() => _OafChatScreenState();
}

class _OafChatScreenState extends ConsumerState<OafChatScreen> {
  final _controller = TextEditingController();
  final _scroll = ScrollController();
  late final VoiceService _voice = VoiceService(api: ref.read(apiProvider));

  List<PeerMessage> _messages = const [];
  final _pending = <OafAttachment>[];
  bool _loading = true;
  bool _busy = false;
  bool _uploading = false;
  bool _listening = false;
  late bool _voiceMode = widget.startInVoiceMode;
  String _heard = '';
  String? _error;
  Timer? _poll;

  @override
  void initState() {
    super.initState();
    unawaited(_refresh());
    _controller.addListener(() => setState(() {}));
    if (_voiceMode) WidgetsBinding.instance.addPostFrameCallback((_) => _listen());
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
      final list = await ref.read(apiProvider).oafMessages(widget.session.id);
      if (!mounted) return;
      final atBottom = !_scroll.hasClients || _scroll.position.pixels >= _scroll.position.maxScrollExtent - 60;
      setState(() {
        _messages = list;
        _loading = false;
      });
      if (atBottom) _scrollToEnd();
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
      if (_scroll.hasClients) _scroll.jumpTo(_scroll.position.maxScrollExtent);
    });
  }

  // ------------------------------------------------------------- attachments ---

  Future<void> _attach() async {
    final picker = ImagePicker();
    final files = await picker.pickMultiImage(imageQuality: 85);
    if (files.isEmpty) return;
    setState(() => _uploading = true);
    try {
      for (final f in files) {
        final bytes = await f.readAsBytes();
        final att = await ref.read(apiProvider).oafUpload(
              widget.session.id,
              bytes,
              f.name,
              f.mimeType ?? 'image/jpeg',
            );
        if (mounted) setState(() => _pending.add(att));
      }
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _uploading = false);
    }
  }

  // ------------------------------------------------------------------- voice ---

  Future<void> _listen() async {
    if (_listening || _busy || widget.readOnly) return;
    setState(() {
      _listening = true;
      _heard = '';
    });
    final text = await _voice.listenOnce(onPartial: (t) => setState(() => _heard = t));
    if (!mounted) return;
    setState(() {
      _listening = false;
      _heard = '';
    });
    if (text == null || text.trim().isEmpty) {
      if (_voice.lastError.isNotEmpty) setState(() => _error = _voice.lastError);
      return;
    }
    if (_voiceMode) {
      await _send(text.trim());
    } else {
      _controller.text = (_controller.text.isEmpty ? '' : '${_controller.text} ') + text.trim();
    }
  }

  // -------------------------------------------------------------------- send ---

  Future<void> _send([String? override]) async {
    final text = (override ?? _controller.text).trim();
    if ((text.isEmpty && _pending.isEmpty) || _busy || widget.readOnly) return;
    unawaited(HapticFeedback.selectionClick());
    final atts = _pending.map((a) => a.id).toList();
    setState(() {
      _busy = true;
      _error = null;
      _controller.clear();
      _pending.clear();
      _messages = [
        ..._messages,
        PeerMessage(
          id: 'local-${DateTime.now().microsecondsSinceEpoch}',
          fromInstanceId: '',
          fromInstanceName: 'You',
          toInstanceId: 'oaf',
          kind: 'message',
          content: text.isEmpty ? '(attachments)' : text,
          createdAt: DateTime.now(),
        ),
      ];
    });
    _scrollToEnd();
    _poll = Timer.periodic(const Duration(seconds: 3), (_) => _refresh());
    try {
      final reply = await ref.read(apiProvider).oafSend(
            widget.session.id,
            text.isEmpty ? 'Look at what I attached.' : text,
            attachments: atts,
          );
      await _refresh();
      if (_voiceMode) {
        await _voice.speak(reply.content);
        if (mounted && _voiceMode) unawaited(_listen());
      }
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
      await _refresh();
    } finally {
      _poll?.cancel();
      _poll = null;
      if (mounted) setState(() => _busy = false);
    }
  }

  Future<void> _openSettings() async {
    List<AIProvider> providers = widget.providers;
    final updated = await showModalBottomSheet<OafSession>(
      context: context,
      isScrollControlled: true,
      showDragHandle: true,
      builder: (_) => SessionSettingsSheet(session: widget.session, devices: widget.devices, providers: providers),
    );
    if (updated != null) widget.onSessionChanged(updated);
  }

  @override
  Widget build(BuildContext context) {
    final device = widget.devices.where((d) => d.id == widget.session.deviceId).firstOrNull;
    return Scaffold(
      appBar: AppBar(
        leading: IconButton(
          icon: const Icon(Icons.menu_rounded),
          tooltip: 'Sessions',
          onPressed: widget.onOpenSessions,
        ),
        title: InkWell(
          onTap: widget.readOnly ? null : _openSettings,
          borderRadius: BorderRadius.circular(8),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(widget.session.name, maxLines: 1, overflow: TextOverflow.ellipsis,
                  style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w600)),
              Row(
                children: [
                  if (device != null) ...[
                    Container(
                      width: 6,
                      height: 6,
                      margin: const EdgeInsets.only(right: 5),
                      decoration: BoxDecoration(shape: BoxShape.circle, color: device.online ? Fleet.good : Fleet.ink500),
                    ),
                  ],
                  Flexible(
                    child: Text(
                      device == null
                          ? 'no device · fleet only · tap to attach'
                          : '${device.name}${widget.session.cwd.isNotEmpty ? ' · ${widget.session.cwd}' : ' · no folder'}',
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: 11, fontFamily: 'monospace', color: Fleet.ink400),
                    ),
                  ),
                ],
              ),
            ],
          ),
        ),
        actions: [
          IconButton(
            tooltip: _voiceMode ? 'Leave voice mode' : 'Voice mode',
            icon: Icon(_voiceMode ? Icons.record_voice_over : Icons.record_voice_over_outlined,
                color: _voiceMode ? Fleet.live : null),
            onPressed: widget.readOnly
                ? null
                : () {
                    setState(() => _voiceMode = !_voiceMode);
                    if (_voiceMode) {
                      unawaited(_listen());
                    } else {
                      unawaited(_voice.stopListening());
                      unawaited(_voice.stopSpeaking());
                    }
                  },
          ),
          IconButton(icon: const Icon(Icons.refresh), onPressed: _refresh),
        ],
      ),
      body: Column(
        children: [
          Expanded(child: _stream(device)),
          if (_pending.isNotEmpty || _uploading) _pendingRow(),
          _composer(),
        ],
      ),
    );
  }

  Widget _stream(OafDevice? device) {
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_messages.isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(28),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              const OafAvatar(size: 84),
              const SizedBox(height: 14),
              Text(
                device == null
                    ? 'No device yet, so Oaf has the fleet but not a machine. Tap the title to attach your PC (fleetctl host) or this phone, pick a folder, and this becomes a session on it.'
                    : 'This session works in ${widget.session.cwd.isNotEmpty ? widget.session.cwd : device.name}. Ask for a change, a command, a check — Oaf reads before it writes and shows every step.',
                textAlign: TextAlign.center,
                style: TextStyle(fontSize: 13, height: 1.45, color: Fleet.ink300),
              ),
            ],
          ),
        ),
      );
    }
    return ListView.builder(
      controller: _scroll,
      padding: const EdgeInsets.fromLTRB(12, 12, 12, 8),
      itemCount: _messages.length + (_busy ? 1 : 0) + (_error != null ? 1 : 0),
      itemBuilder: (context, i) {
        if (i < _messages.length) return _row(_messages[i]);
        if (_error != null && i == _messages.length) {
          return Padding(
            padding: const EdgeInsets.symmetric(vertical: 6),
            child: Text(_error!, style: TextStyle(fontSize: 12, color: Fleet.bad)),
          );
        }
        return Padding(
          padding: const EdgeInsets.fromLTRB(44, 6, 0, 6),
          child: Row(
            children: [
              SizedBox(width: 10, height: 10, child: CircularProgressIndicator(strokeWidth: 2, color: Fleet.live)),
              const SizedBox(width: 8),
              Text('Oaf is working…', style: TextStyle(fontSize: 12, color: Fleet.ink400)),
            ],
          ),
        );
      },
    );
  }

  Widget _row(PeerMessage m) {
    if (m.kind == 'tool') return _ToolRow(message: m);
    final fromOaf = m.fromInstanceId == 'oaf';
    final atts = m.attachments;
    final bubble = Container(
      constraints: BoxConstraints(maxWidth: MediaQuery.of(context).size.width * 0.82),
      padding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
      decoration: BoxDecoration(
        color: fromOaf ? Fleet.ink850 : Fleet.live.withValues(alpha: 0.10),
        border: Border.all(color: fromOaf ? Fleet.ink800 : Fleet.live.withValues(alpha: 0.25)),
        borderRadius: BorderRadius.only(
          topLeft: Radius.circular(fromOaf ? 6 : 18),
          topRight: Radius.circular(fromOaf ? 18 : 6),
          bottomLeft: const Radius.circular(18),
          bottomRight: const Radius.circular(18),
        ),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (fromOaf && m.data['done'] == true)
            Padding(
              padding: const EdgeInsets.only(bottom: 6),
              child: Text('GOAL REACHED',
                  style: TextStyle(fontSize: 10, letterSpacing: 1.2, fontWeight: FontWeight.w700, color: Fleet.good)),
            ),
          MarkdownLite(m.content, baseStyle: const TextStyle(fontSize: 14, height: 1.45)),
          if (atts.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Wrap(
                spacing: 6,
                runSpacing: 6,
                children: [for (final a in atts) _attachmentChip(a)],
              ),
            ),
        ],
      ),
    );
    return Padding(
      padding: const EdgeInsets.symmetric(vertical: 5),
      child: Row(
        mainAxisAlignment: fromOaf ? MainAxisAlignment.start : MainAxisAlignment.end,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          if (fromOaf) ...[const OafAvatar(size: 30), const SizedBox(width: 8)],
          Flexible(child: bubble),
        ],
      ),
    );
  }

  Widget _attachmentChip(OafAttachment a) {
    final api = ref.read(apiProvider);
    if (a.isImage && a.url.isNotEmpty) {
      return ClipRRect(
        borderRadius: BorderRadius.circular(10),
        child: Image.network(
          '${api.baseUrl}${a.url}',
          headers: {if (api.token != null) 'Authorization': 'Bearer ${api.token}'},
          height: 140,
          fit: BoxFit.cover,
          errorBuilder: (_, __, ___) => _fileChip(a),
        ),
      );
    }
    return _fileChip(a);
  }

  Widget _fileChip(OafAttachment a) => Container(
        padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 5),
        decoration: BoxDecoration(
          color: Fleet.ink900,
          borderRadius: BorderRadius.circular(8),
          border: Border.all(color: Fleet.ink700),
        ),
        child: Text('📄 ${a.name}', style: const TextStyle(fontSize: 12)),
      );

  Widget _pendingRow() {
    return SizedBox(
      height: 64,
      child: ListView(
        scrollDirection: Axis.horizontal,
        padding: const EdgeInsets.symmetric(horizontal: 12),
        children: [
          for (final a in _pending)
            Padding(
              padding: const EdgeInsets.only(right: 8, top: 6, bottom: 6),
              child: Stack(
                children: [
                  _attachmentThumb(a),
                  Positioned(
                    right: -6,
                    top: -6,
                    child: IconButton(
                      icon: const Icon(Icons.cancel, size: 18),
                      onPressed: () => setState(() => _pending.remove(a)),
                    ),
                  ),
                ],
              ),
            ),
          if (_uploading)
            const Padding(
              padding: EdgeInsets.all(18),
              child: SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2)),
            ),
        ],
      ),
    );
  }

  Widget _attachmentThumb(OafAttachment a) {
    final api = ref.read(apiProvider);
    return ClipRRect(
      borderRadius: BorderRadius.circular(10),
      child: a.isImage
          ? Image.network(
              '${api.baseUrl}${a.url}',
              headers: {if (api.token != null) 'Authorization': 'Bearer ${api.token}'},
              width: 52,
              height: 52,
              fit: BoxFit.cover,
            )
          : Container(
              width: 52,
              height: 52,
              color: Fleet.ink850,
              alignment: Alignment.center,
              child: const Icon(Icons.description_outlined),
            ),
    );
  }

  Widget _composer() {
    final enabled = !widget.readOnly && !_busy;
    return SafeArea(
      top: false,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(8, 4, 8, 8),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.end,
          children: [
            IconButton(
              tooltip: 'Attach photos',
              icon: const Icon(Icons.add_photo_alternate_outlined),
              onPressed: enabled ? _attach : null,
            ),
            Expanded(
              child: TextField(
                controller: _controller,
                enabled: enabled,
                minLines: 1,
                maxLines: 5,
                textInputAction: TextInputAction.newline,
                decoration: InputDecoration(
                  hintText: widget.readOnly
                      ? 'Auditors can read a session but not act in it'
                      : _listening
                          ? (_heard.isEmpty ? 'Listening…' : _heard)
                          : 'Ask Oaf, give it work, or / for commands',
                  isDense: true,
                  border: OutlineInputBorder(borderRadius: BorderRadius.circular(18)),
                  contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 10),
                ),
              ),
            ),
            IconButton(
              tooltip: _listening ? 'Stop listening' : 'Dictate',
              icon: Icon(_listening ? Icons.mic : Icons.mic_none, color: _listening ? Fleet.live : null),
              onPressed: enabled ? (_listening ? _voice.stopListening : _listen) : null,
            ),
            FilledButton(
              onPressed: enabled && !_uploading && (_controller.text.trim().isNotEmpty || _pending.isNotEmpty)
                  ? _send
                  : null,
              style: FilledButton.styleFrom(
                shape: const CircleBorder(),
                padding: const EdgeInsets.all(12),
                minimumSize: const Size(44, 44),
              ),
              child: _busy
                  ? const SizedBox(width: 16, height: 16, child: CircularProgressIndicator(strokeWidth: 2))
                  : const Icon(Icons.send_rounded, size: 20),
            ),
          ],
        ),
      ),
    );
  }
}

/// One tool call in the thread, folded to a line; tap for the args and result.
class _ToolRow extends StatefulWidget {
  const _ToolRow({required this.message});
  final PeerMessage message;

  @override
  State<_ToolRow> createState() => _ToolRowState();
}

class _ToolRowState extends State<_ToolRow> {
  bool _open = false;

  @override
  Widget build(BuildContext context) {
    final m = widget.message;
    final failed = m.data['failed'] == true;
    final tool = (m.data['tool'] as String?) ?? m.content.split(' ').first;
    final result = (m.data['result'] as String?) ?? '';
    final color = failed ? Fleet.bad : Fleet.ink400;
    return Padding(
      padding: const EdgeInsets.fromLTRB(38, 2, 0, 2),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          InkWell(
            borderRadius: BorderRadius.circular(8),
            onTap: () => setState(() => _open = !_open),
            child: Padding(
              padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 4),
              child: Row(
                children: [
                  Icon(_open ? Icons.expand_more : Icons.chevron_right, size: 16, color: Fleet.ink500),
                  Container(
                    padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                    decoration: BoxDecoration(
                      color: failed ? Fleet.bad.withValues(alpha: 0.15) : Fleet.ink800,
                      borderRadius: BorderRadius.circular(5),
                    ),
                    child: Text(tool, style: TextStyle(fontSize: 11, fontFamily: 'monospace', color: color)),
                  ),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Text(
                      m.content.substring(tool.length).trim(),
                      maxLines: 1,
                      overflow: TextOverflow.ellipsis,
                      style: TextStyle(fontSize: 11, fontFamily: 'monospace', color: color),
                    ),
                  ),
                ],
              ),
            ),
          ),
          if (_open)
            Container(
              margin: const EdgeInsets.only(top: 4, left: 6),
              padding: const EdgeInsets.all(10),
              constraints: const BoxConstraints(maxHeight: 280),
              decoration: BoxDecoration(
                color: Fleet.ink950,
                borderRadius: BorderRadius.circular(10),
                border: Border.all(color: Fleet.ink800),
              ),
              child: SingleChildScrollView(
                child: SelectableText(
                  '${m.data['args'] ?? {}}\n\n${result.isEmpty ? '(no output)' : result}',
                  style: TextStyle(fontSize: 11, fontFamily: 'monospace', height: 1.4, color: Fleet.ink300),
                ),
              ),
            ),
        ],
      ),
    );
  }
}
