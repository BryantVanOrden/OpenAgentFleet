import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';

/// Accounts on this deployment: who exists, what they may do, and how to stop
/// them.
///
/// Disabling rather than deleting throughout: removing a row cascades that
/// person's API keys away and orphans everything they made, which is the wrong
/// thing to do to someone who has simply left.
class UsersScreen extends ConsumerStatefulWidget {
  const UsersScreen({super.key});

  @override
  ConsumerState<UsersScreen> createState() => _UsersScreenState();
}

class _UsersScreenState extends ConsumerState<UsersScreen> {
  List<AdminUser> _users = const [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final users = await ref.read(apiProvider).adminUsers();
      if (mounted) setState(() => _users = users);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _run(Future<void> Function() action) async {
    try {
      await action();
      await _load();
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    }
  }

  @override
  Widget build(BuildContext context) {
    final me = ref.watch(meProvider).valueOrNull;

    return Scaffold(
      appBar: AppBar(
        title: const Text('Users'),
        actions: [
          IconButton(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: _load,
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _newUser,
        icon: const Icon(Icons.person_add_alt),
        label: const Text('New user'),
      ),
      body: _loading && _users.isEmpty
          ? const Center(child: CircularProgressIndicator())
          : RefreshIndicator(
              onRefresh: _load,
              child: ListView(
                padding: const EdgeInsets.fromLTRB(16, 12, 16, 96),
                children: [
                  if (_error != null) ...[
                    InlineError(_error!),
                    const SizedBox(height: 12),
                  ],
                  for (final u in _users) _tile(u, isMe: u.id == me?.id),
                  if (_users.isEmpty && !_loading)
                    Padding(
                      padding: const EdgeInsets.all(32),
                      child: Text('No accounts yet.',
                          textAlign: TextAlign.center,
                          style: TextStyle(color: Fleet.ink400)),
                    ),
                ],
              ),
            ),
    );
  }

  Widget _tile(AdminUser u, {required bool isMe}) => Card(
        color: Fleet.ink850,
        child: ListTile(
          leading: Icon(
            u.disabled ? Icons.person_off_outlined : Icons.person_outline,
            color: u.disabled ? Fleet.bad : Fleet.ink300,
          ),
          title: Row(
            children: [
              Flexible(
                child: Text(u.email,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                      decoration: u.disabled ? TextDecoration.lineThrough : null,
                    )),
              ),
              if (isMe)
                Padding(
                  padding: const EdgeInsets.only(left: 6),
                  child: Text('(you)',
                      style: TextStyle(color: Fleet.ink500, fontSize: 11)),
                ),
            ],
          ),
          subtitle: Text(
            u.disabled ? '${u.role} · disabled' : u.role,
            style: TextStyle(color: Fleet.ink400, fontSize: 11),
          ),
          trailing: PopupMenuButton<String>(
            onSelected: (a) => switch (a) {
              'role' => _changeRole(u),
              'password' => _setPassword(u),
              _ => _run(() => ref
                  .read(apiProvider)
                  .setUserDisabled(u.id, !u.disabled)),
            },
            itemBuilder: (_) => [
              const PopupMenuItem(
                value: 'role',
                child: ListTile(
                    dense: true,
                    leading: Icon(Icons.badge_outlined),
                    title: Text('Change role')),
              ),
              const PopupMenuItem(
                value: 'password',
                child: ListTile(
                    dense: true,
                    leading: Icon(Icons.password_outlined),
                    title: Text('Set password')),
              ),
              // Disabling yourself locks you out of your own deployment; the
              // server refuses it too, but not offering it is kinder than
              // being told after tapping.
              if (!isMe)
                PopupMenuItem(
                  value: 'disabled',
                  child: ListTile(
                    dense: true,
                    leading: Icon(
                      u.disabled ? Icons.lock_open : Icons.block,
                      color: u.disabled ? Fleet.live : Fleet.bad,
                    ),
                    title: Text(u.disabled ? 'Re-enable' : 'Disable',
                        style: TextStyle(
                            color: u.disabled ? Fleet.live : Fleet.bad)),
                  ),
                ),
            ],
          ),
        ),
      );

  Future<void> _newUser() async {
    final email = TextEditingController();
    final password = TextEditingController();
    String role = 'operator';

    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setLocal) => AlertDialog(
          backgroundColor: Fleet.ink850,
          title: const Text('New user'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: email,
                autofocus: true,
                keyboardType: TextInputType.emailAddress,
                decoration: const InputDecoration(labelText: 'Email'),
              ),
              const SizedBox(height: 8),
              TextField(
                controller: password,
                decoration: const InputDecoration(
                  labelText: 'Password',
                  helperText: 'At least 12 characters',
                ),
              ),
              const SizedBox(height: 12),
              DropdownButtonFormField<String>(
                initialValue: role,
                decoration: const InputDecoration(labelText: 'Role'),
                items: [
                  for (final r in AdminUser.roles)
                    DropdownMenuItem(value: r, child: Text(r)),
                ],
                onChanged: (v) => setLocal(() => role = v ?? role),
              ),
            ],
          ),
          actions: [
            TextButton(
                onPressed: () => Navigator.pop(ctx, false),
                child: const Text('Cancel')),
            FilledButton(
                onPressed: () => Navigator.pop(ctx, true),
                child: const Text('Create')),
          ],
        ),
      ),
    );
    if (ok != true) return;
    await _run(() => ref
        .read(apiProvider)
        .createUser(email.text.trim(), password.text, role));
  }

  Future<void> _changeRole(AdminUser u) async {
    final picked = await showDialog<String>(
      context: context,
      builder: (ctx) => SimpleDialog(
        backgroundColor: Fleet.ink850,
        title: Text('Role for ${u.email}'),
        children: [
          for (final r in AdminUser.roles)
            SimpleDialogOption(
              onPressed: () => Navigator.pop(ctx, r),
              child: Row(
                children: [
                  Icon(
                    r == u.role
                        ? Icons.radio_button_checked
                        : Icons.radio_button_unchecked,
                    size: 18,
                    color: r == u.role ? Fleet.live : Fleet.ink500,
                  ),
                  const SizedBox(width: 10),
                  Text(r),
                ],
              ),
            ),
        ],
      ),
    );
    if (picked == null || picked == u.role) return;
    await _run(() => ref.read(apiProvider).setUserRole(u.id, picked));
  }

  Future<void> _setPassword(AdminUser u) async {
    final c = TextEditingController();
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        backgroundColor: Fleet.ink850,
        title: Text('Password for ${u.email}'),
        content: TextField(
          controller: c,
          autofocus: true,
          decoration: const InputDecoration(
            labelText: 'New password',
            helperText: 'At least 12 characters',
          ),
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel')),
          FilledButton(
              onPressed: () => Navigator.pop(ctx, true),
              child: const Text('Set')),
        ],
      ),
    );
    if (ok != true) return;
    await _run(() => ref.read(apiProvider).setUserPassword(u.id, c.text));
  }
}
