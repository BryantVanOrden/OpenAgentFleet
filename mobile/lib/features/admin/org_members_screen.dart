import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';

/// Who is in a department, and what their standing lets them do.
///
/// The role is the whole point: it answers "what does someone at this level
/// normally do" once, rather than per person per bot. Each role's summary is
/// shown beside it, because "member" and "admin" mean nothing on their own and
/// getting it wrong hands someone the ability to delete other people's work.
class OrgMembersScreen extends ConsumerStatefulWidget {
  const OrgMembersScreen({super.key, required this.org});

  final Org org;

  @override
  ConsumerState<OrgMembersScreen> createState() => _OrgMembersScreenState();
}

class _OrgMembersScreenState extends ConsumerState<OrgMembersScreen> {
  List<OrgMember> _members = const [];
  List<FleetUser> _users = const [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    try {
      final api = ref.read(apiProvider);
      final members = await api.orgMembers(widget.org.id);
      // The user list is admin-only; without it we can still show members, so
      // a department owner who is not a deployment admin is not locked out of
      // their own screen.
      List<FleetUser> users = const [];
      try {
        users = await api.users();
      } catch (_) {}
      if (!mounted) return;
      setState(() {
        _members = members;
        _users = users;
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

  Future<void> _setRole(String userId, String role) async {
    try {
      await ref.read(apiProvider).setOrgMember(widget.org.id, userId, role);
      await _refresh();
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    }
  }

  Future<void> _remove(OrgMember m) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Remove ${m.email}?'),
        content: Text(
          'They lose access to every bot and secret in ${widget.org.name}, '
          'unless a per-bot exception grants it back.',
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Fleet.bad),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Remove'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    try {
      await ref.read(apiProvider).removeOrgMember(widget.org.id, m.userId);
      await _refresh();
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    }
  }

  Future<void> _add() async {
    final inMembers = _members.map((m) => m.userId).toSet();
    final candidates =
        _users.where((u) => !inMembers.contains(u.id)).toList();

    if (candidates.isEmpty) {
      setState(() => _error = _users.isEmpty
          ? 'Only a deployment administrator can list users to add.'
          : 'Everyone is already a member of this department.');
      return;
    }

    String? picked;
    String role = 'member';
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setLocal) => AlertDialog(
          title: const Text('Add someone'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              DropdownButtonFormField<String>(
                initialValue: picked,
                isExpanded: true,
                decoration: const InputDecoration(labelText: 'Person'),
                dropdownColor: Fleet.ink850,
                items: [
                  for (final u in candidates)
                    DropdownMenuItem(value: u.id, child: Text(u.email)),
                ],
                onChanged: (v) => setLocal(() => picked = v),
              ),
              const SizedBox(height: 12),
              DropdownButtonFormField<String>(
                initialValue: role,
                isExpanded: true,
                decoration: const InputDecoration(labelText: 'Role'),
                dropdownColor: Fleet.ink850,
                items: [
                  for (final r in OrgMember.roles)
                    DropdownMenuItem(value: r, child: Text(r)),
                ],
                onChanged: (v) => setLocal(() => role = v ?? 'member'),
              ),
              const SizedBox(height: 8),
              Text(OrgMember.roleSummary[role] ?? '',
                  style: TextStyle(color: Fleet.ink400, fontSize: 11.5)),
            ],
          ),
          actions: [
            TextButton(
                onPressed: () => Navigator.pop(ctx, false),
                child: const Text('Cancel')),
            FilledButton(
                onPressed: () => Navigator.pop(ctx, true),
                child: const Text('Add')),
          ],
        ),
      ),
    );
    if (ok != true || picked == null) return;
    await _setRole(picked!, role);
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(widget.org.name, style: const TextStyle(fontSize: 15)),
            Text('${_members.length} member${_members.length == 1 ? '' : 's'}',
                style: TextStyle(color: Fleet.ink400, fontSize: 11)),
          ],
        ),
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _add,
        icon: const Icon(Icons.person_add_alt),
        label: const Text('Add'),
      ),
      body: _loading
          ? const Center(child: CircularProgressIndicator())
          : ListView(
              padding: const EdgeInsets.fromLTRB(12, 12, 12, 88),
              children: [
                InlineError(_error),
                if (_members.isEmpty)
                  Padding(
                    padding: const EdgeInsets.all(24),
                    child: Text(
                      'Nobody is in this department yet, so only a deployment '
                      'administrator can see its bots.',
                      textAlign: TextAlign.center,
                      style: TextStyle(
                          color: Fleet.ink400, fontSize: 13, height: 1.4),
                    ),
                  ),
                for (final m in _members) _tile(m),
              ],
            ),
    );
  }

  Widget _tile(OrgMember m) {
    return Card(
      color: Fleet.ink850,
      margin: const EdgeInsets.only(bottom: 8),
      child: ListTile(
        leading: Icon(Icons.person_outline, size: 20, color: Fleet.ink300),
        title: Text(m.email.isEmpty ? m.userId : m.email,
            style: const TextStyle(fontSize: 13.5)),
        subtitle: Text(OrgMember.roleSummary[m.orgRole] ?? m.orgRole,
            style: TextStyle(color: Fleet.ink400, fontSize: 11)),
        trailing: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            DropdownButton<String>(
              value: m.orgRole,
              underline: const SizedBox.shrink(),
              dropdownColor: Fleet.ink850,
              style: TextStyle(color: Fleet.ink200, fontSize: 12),
              items: [
                for (final r in OrgMember.roles)
                  DropdownMenuItem(value: r, child: Text(r)),
              ],
              onChanged: (v) => v == null ? null : _setRole(m.userId, v),
            ),
            IconButton(
              icon: Icon(Icons.person_remove_outlined,
                  size: 18, color: Fleet.ink400),
              onPressed: () => _remove(m),
            ),
          ],
        ),
      ),
    );
  }
}
