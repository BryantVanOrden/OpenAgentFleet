import 'package:flutter/material.dart';

import '../../core/models.dart';
import '../../core/theme/theme.dart';

/// What the home tab can show: the fleet channel, one session with Oaf, or a
/// thread the bots keep between themselves.
sealed class HomePick {
  const HomePick();
  String get key;
}

class FleetPick extends HomePick {
  const FleetPick();
  @override
  String get key => 'fleet';
}

class SessionPick extends HomePick {
  const SessionPick(this.id);
  final String id;
  @override
  String get key => 'session:$id';
}

class ThreadPick extends HomePick {
  const ThreadPick(this.id);
  final String id;
  @override
  String get key => 'thread:$id';
}

HomePick pickFromKey(String? key) {
  if (key == null || key == 'fleet') return const FleetPick();
  if (key.startsWith('session:')) return SessionPick(key.substring(8));
  if (key.startsWith('thread:')) return ThreadPick(key.substring(7));
  return const FleetPick();
}

/// The list of everything you can open: sessions (with rename, pin, delete
/// under a long press), the fleet channel, and bot-to-bot threads. What the
/// console keeps in a rail, the phone keeps in a sheet.
class SessionsSheet extends StatelessWidget {
  const SessionsSheet({
    super.key,
    required this.current,
    required this.sessions,
    required this.threads,
    required this.nameOf,
    required this.readOnly,
    required this.onPick,
    required this.onNew,
    required this.onRename,
    required this.onPin,
    required this.onDelete,
  });

  final HomePick current;
  final List<OafSession> sessions;
  final List<Conversation> threads;
  final String Function(String memberId) nameOf;
  final bool readOnly;
  final ValueChanged<HomePick> onPick;
  final VoidCallback onNew;
  final void Function(OafSession) onRename;
  final void Function(OafSession) onPin;
  final void Function(OafSession) onDelete;

  @override
  Widget build(BuildContext context) {
    return SafeArea(
      child: DraggableScrollableSheet(
        expand: false,
        initialChildSize: 0.7,
        minChildSize: 0.4,
        maxChildSize: 0.95,
        builder: (context, scroll) => ListView(
          controller: scroll,
          padding: const EdgeInsets.fromLTRB(12, 8, 12, 24),
          children: [
            Center(
              child: Container(
                width: 36,
                height: 4,
                margin: const EdgeInsets.only(bottom: 12),
                decoration: BoxDecoration(color: Fleet.ink600, borderRadius: BorderRadius.circular(2)),
              ),
            ),
            _tile(
              context,
              selected: current is FleetPick,
              leading: Container(
                width: 36,
                height: 36,
                alignment: Alignment.center,
                decoration: BoxDecoration(color: Fleet.live.withValues(alpha: 0.15), borderRadius: BorderRadius.circular(10)),
                child: Icon(Icons.grid_view_rounded, size: 18, color: Fleet.live),
              ),
              title: 'Fleet',
              subtitle: 'everyone, every bot',
              onTap: () => onPick(const FleetPick()),
            ),
            const SizedBox(height: 14),
            Row(
              children: [
                Expanded(child: _sectionLabel('Sessions')),
                if (!readOnly)
                  TextButton.icon(
                    onPressed: onNew,
                    icon: const Icon(Icons.add, size: 18),
                    label: const Text('New'),
                  ),
              ],
            ),
            if (sessions.isEmpty)
              Padding(
                padding: const EdgeInsets.fromLTRB(4, 4, 4, 8),
                child: Text(
                  'A session is a chat with Oaf that can act on your PC or this phone, in a folder you choose. Start one.',
                  style: TextStyle(fontSize: 12, height: 1.4, color: Fleet.ink400),
                ),
              ),
            for (final s in sessions)
              _tile(
                context,
                selected: current is SessionPick && (current as SessionPick).id == s.id,
                leading: Image.asset('assets/branding/mascot.png', width: 30, height: 30,
                    errorBuilder: (_, __, ___) => Icon(Icons.smart_toy_outlined, color: Fleet.live)),
                title: (s.pinned ? '★ ' : '') + s.name,
                subtitle: s.deviceName.isNotEmpty
                    ? '${s.deviceName}${s.cwd.isNotEmpty ? ' · ${_shortPath(s.cwd)}' : ''}'
                    : 'no device',
                onTap: () => onPick(SessionPick(s.id)),
                onLongPress: readOnly ? null : () => _sessionMenu(context, s),
              ),
            if (threads.isNotEmpty) ...[
              const SizedBox(height: 14),
              _sectionLabel('Between bots'),
              for (final c in threads)
                _tile(
                  context,
                  selected: current is ThreadPick && (current as ThreadPick).id == c.id,
                  leading: Container(
                    width: 36,
                    height: 36,
                    alignment: Alignment.center,
                    decoration: BoxDecoration(color: Fleet.cool.withValues(alpha: 0.15), borderRadius: BorderRadius.circular(10)),
                    child: Icon(Icons.swap_horiz_rounded, size: 18, color: Fleet.cool),
                  ),
                  title: c.title.isNotEmpty
                      ? c.title
                      : c.members.where((m) => m != 'operator').map(nameOf).join(' & '),
                  subtitle: c.members.where((m) => m != 'operator').map(nameOf).join(', '),
                  onTap: () => onPick(ThreadPick(c.id)),
                ),
            ],
          ],
        ),
      ),
    );
  }

  Widget _sectionLabel(String text) => Padding(
        padding: const EdgeInsets.fromLTRB(4, 0, 4, 6),
        child: Text(text.toUpperCase(),
            style: TextStyle(fontSize: 10, letterSpacing: 1.6, fontWeight: FontWeight.w600, color: Fleet.ink400)),
      );

  Widget _tile(
    BuildContext context, {
    required bool selected,
    required Widget leading,
    required String title,
    required String subtitle,
    required VoidCallback onTap,
    VoidCallback? onLongPress,
  }) {
    return Material(
      color: selected ? Fleet.ink800 : Colors.transparent,
      borderRadius: BorderRadius.circular(14),
      child: InkWell(
        borderRadius: BorderRadius.circular(14),
        onTap: () {
          Navigator.of(context).maybePop();
          onTap();
        },
        onLongPress: onLongPress,
        child: Padding(
          padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 9),
          child: Row(
            children: [
              leading,
              const SizedBox(width: 12),
              Expanded(
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Text(title, maxLines: 1, overflow: TextOverflow.ellipsis,
                        style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w500)),
                    Text(subtitle, maxLines: 1, overflow: TextOverflow.ellipsis,
                        style: TextStyle(fontSize: 11, color: Fleet.ink400)),
                  ],
                ),
              ),
              if (selected) Icon(Icons.check_rounded, size: 18, color: Fleet.live),
            ],
          ),
        ),
      ),
    );
  }

  Future<void> _sessionMenu(BuildContext context, OafSession s) async {
    final action = await showModalBottomSheet<String>(
      context: context,
      builder: (ctx) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            ListTile(
              leading: const Icon(Icons.edit_outlined),
              title: const Text('Rename'),
              onTap: () => Navigator.pop(ctx, 'rename'),
            ),
            ListTile(
              leading: Icon(s.pinned ? Icons.star : Icons.star_border),
              title: Text(s.pinned ? 'Unpin' : 'Pin'),
              onTap: () => Navigator.pop(ctx, 'pin'),
            ),
            ListTile(
              leading: Icon(Icons.delete_outline, color: Fleet.bad),
              title: Text('Delete', style: TextStyle(color: Fleet.bad)),
              onTap: () => Navigator.pop(ctx, 'delete'),
            ),
          ],
        ),
      ),
    );
    switch (action) {
      case 'rename':
        onRename(s);
      case 'pin':
        onPin(s);
      case 'delete':
        onDelete(s);
    }
  }
}

String _shortPath(String p) {
  final parts = p.replaceAll('\\', '/').split('/').where((x) => x.isNotEmpty).toList();
  return parts.length > 2 ? '…/${parts.sublist(parts.length - 2).join('/')}' : p;
}
