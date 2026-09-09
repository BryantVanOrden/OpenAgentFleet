import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:shared_preferences/shared_preferences.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../fleet_comms/conversation_screen.dart';
import 'home_chat_screen.dart';
import 'oaf_chat_screen.dart';
import 'sessions_sheet.dart';

/// The Chat tab's root: which conversation is open, and the sheet to switch.
///
/// Fleet (everyone), a session with Oaf, or a thread between bots. The pick is
/// remembered, so the app reopens where you left it. The console keeps these
/// in a rail; a phone has no room for one, so the switcher is a sheet behind
/// the menu button.
class ChatHome extends ConsumerStatefulWidget {
  const ChatHome({super.key});

  @override
  ConsumerState<ChatHome> createState() => _ChatHomeState();
}

class _ChatHomeState extends ConsumerState<ChatHome> {
  static const _pickKey = 'oaf.home.pick';

  HomePick _pick = const FleetPick();
  List<OafSession> _sessions = const [];
  List<OafDevice> _devices = const [];
  List<AIProvider> _providers = const [];
  List<Conversation> _threads = const [];
  Timer? _poll;

  @override
  void initState() {
    super.initState();
    unawaited(_restore());
    _poll = Timer.periodic(const Duration(seconds: 20), (_) => _refresh());
  }

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  Future<void> _restore() async {
    final prefs = await SharedPreferences.getInstance();
    if (mounted) setState(() => _pick = pickFromKey(prefs.getString(_pickKey)));
    await _refresh();
  }

  Future<void> _refresh() async {
    final api = ref.read(apiProvider);
    final results = await Future.wait<dynamic>([
      api.oafSessions().catchError((_) => <OafSession>[]),
      api.oafDevices().catchError((_) => <OafDevice>[]),
      api.conversations().catchError((_) => <Conversation>[]),
      if (ref.read(meProvider).valueOrNull?.isAdmin ?? false) api.providers().catchError((_) => <AIProvider>[]),
    ]);
    if (!mounted) return;
    final convs = results[2] as List<Conversation>;
    setState(() {
      _sessions = results[0] as List<OafSession>;
      _devices = results[1] as List<OafDevice>;
      if (results.length > 3) _providers = results[3] as List<AIProvider>;
      final live = (ref.read(instancesProvider).valueOrNull ?? const <Instance>[]).map((i) => i.id).toSet();
      _threads = convs.where((c) {
        if (c.id == Conversation.broadcastId || c.id.startsWith('oaf:')) return false;
        final bots = c.members.where((m) => m != 'operator').toList();
        // Threads whose bots are all gone would list as bare ids.
        return bots.length >= 2 && bots.any(live.contains);
      }).toList();
    });
  }

  Future<void> _choose(HomePick p) async {
    setState(() => _pick = p);
    final prefs = await SharedPreferences.getInstance();
    await prefs.setString(_pickKey, p.key);
  }

  String _nameOf(String id) {
    if (id == 'operator') return 'You';
    final insts = ref.read(instancesProvider).valueOrNull ?? const <Instance>[];
    return insts.where((i) => i.id == id).firstOrNull?.name ?? id.substring(0, id.length < 8 ? id.length : 8);
  }

  Future<void> _openSessions() async {
    final role = ref.read(meProvider).valueOrNull?.role ?? '';
    final readOnly = role == 'viewer' || role == 'auditor';
    await _refresh();
    if (!mounted) return;
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (_) => SessionsSheet(
        current: _pick,
        sessions: _sessions,
        threads: _threads,
        nameOf: _nameOf,
        readOnly: readOnly,
        onPick: (p) => unawaited(_choose(p)),
        onNew: () => unawaited(_newSession()),
        onRename: (s) => unawaited(_rename(s)),
        onPin: (s) => unawaited(_pin(s)),
        onDelete: (s) => unawaited(_delete(s)),
      ),
    );
  }

  Future<void> _newSession() async {
    Navigator.of(context).maybePop();
    final s = await ref.read(apiProvider).createOafSession();
    if (!mounted) return;
    setState(() => _sessions = [s, ..._sessions]);
    await _choose(SessionPick(s.id));
  }

  Future<void> _rename(OafSession s) async {
    final ctl = TextEditingController(text: s.name);
    final name = await showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: const Text('Rename session'),
        content: TextField(controller: ctl, autofocus: true, onSubmitted: (v) => Navigator.pop(ctx, v)),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx), child: const Text('Cancel')),
          FilledButton(onPressed: () => Navigator.pop(ctx, ctl.text), child: const Text('Save')),
        ],
      ),
    );
    if (name == null || name.trim().isEmpty) return;
    final updated = await ref.read(apiProvider).updateOafSession(s.id, name: name.trim());
    _replace(updated);
  }

  Future<void> _pin(OafSession s) async {
    _replace(await ref.read(apiProvider).updateOafSession(s.id, pinned: !s.pinned));
  }

  Future<void> _delete(OafSession s) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Delete “${s.name}”?'),
        content: const Text('Its messages go with it. Goals and loops in it stop.'),
        actions: [
          TextButton(onPressed: () => Navigator.pop(ctx, false), child: const Text('Cancel')),
          FilledButton.tonal(onPressed: () => Navigator.pop(ctx, true), child: const Text('Delete')),
        ],
      ),
    );
    if (ok != true) return;
    await ref.read(apiProvider).deleteOafSession(s.id);
    if (!mounted) return;
    setState(() => _sessions = _sessions.where((x) => x.id != s.id).toList());
    if (_pick is SessionPick && (_pick as SessionPick).id == s.id) await _choose(const FleetPick());
  }

  void _replace(OafSession updated) {
    if (!mounted) return;
    setState(() => _sessions = [for (final x in _sessions) x.id == updated.id ? updated : x]);
  }

  @override
  Widget build(BuildContext context) {
    final role = ref.watch(meProvider).valueOrNull?.role ?? '';
    final readOnly = role == 'viewer' || role == 'auditor';
    final pick = _pick;
    if (pick is SessionPick) {
      final s = _sessions.where((x) => x.id == pick.id).firstOrNull;
      if (s != null) {
        return OafChatScreen(
          key: ValueKey(s.id),
          session: s,
          devices: _devices,
          providers: _providers,
          readOnly: readOnly,
          onSessionChanged: _replace,
          onOpenSessions: _openSessions,
        );
      }
      if (_sessions.isNotEmpty) {
        // The session was deleted elsewhere.
        WidgetsBinding.instance.addPostFrameCallback((_) => _choose(const FleetPick()));
      }
    }
    if (pick is ThreadPick) {
      final c = _threads.where((x) => x.id == pick.id).firstOrNull;
      if (c != null) {
        return Scaffold(
          body: ConversationScreen(
            key: ValueKey(c.id),
            conversation: c,
            title: c.title.isNotEmpty ? c.title : c.members.where((m) => m != 'operator').map(_nameOf).join(' & '),
            siblings: _threads,
          ),
          floatingActionButton: FloatingActionButton.small(
            heroTag: 'sessions-fab',
            tooltip: 'Sessions',
            onPressed: _openSessions,
            child: const Icon(Icons.menu_rounded),
          ),
        );
      }
    }
    return HomeChatScreen(onOpenSessions: _openSessions);
  }
}
