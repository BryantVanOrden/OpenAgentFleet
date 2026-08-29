import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';
import 'org_bots_sheet.dart';
import 'org_members_screen.dart';

/// Departments, and who is in them.
///
/// An org answers "what does someone at this level normally do" once, for a
/// whole department, which is the question an administrator actually has. The
/// exceptions live per bot, on the bot.
class OrgsScreen extends ConsumerStatefulWidget {
  const OrgsScreen({super.key});

  @override
  ConsumerState<OrgsScreen> createState() => _OrgsScreenState();
}

class _OrgsScreenState extends ConsumerState<OrgsScreen> {
  List<Org> _orgs = const [];
  bool _loading = true;
  String? _error;

  @override
  void initState() {
    super.initState();
    _refresh();
  }

  Future<void> _refresh() async {
    try {
      final list = await ref.read(apiProvider).orgs();
      if (!mounted) return;
      setState(() {
        _orgs = list;
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

  Future<void> _edit([Org? existing]) async {
    final name = TextEditingController(text: existing?.name ?? '');
    final desc = TextEditingController(text: existing?.description ?? '');

    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text(existing == null ? 'New department' : 'Rename department'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            TextField(
              controller: name,
              autofocus: true,
              decoration: const InputDecoration(
                labelText: 'Name',
                hintText: 'e.g. Engineering',
              ),
            ),
            const SizedBox(height: 10),
            TextField(
              controller: desc,
              decoration: const InputDecoration(labelText: 'Description'),
            ),
          ],
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel')),
          FilledButton(
              onPressed: () => Navigator.pop(ctx, true),
              child: const Text('Save')),
        ],
      ),
    );
    if (ok != true || !mounted || name.text.trim().isEmpty) return;

    try {
      await ref.read(apiProvider).saveOrg(
            id: existing?.id ?? '',
            name: name.text.trim(),
            description: desc.text.trim(),
          );
      await _refresh();
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    }
  }

  Future<void> _delete(Org o) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Delete ${o.name}?'),
        content: Text(
          'Its ${o.botCount} bot${o.botCount == 1 ? '' : 's'} and any shared '
          'secrets are not deleted — they become unassigned and visible only '
          'to an administrator until moved somewhere else.\n\n'
          'Removing a department should not destroy running machines.',
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Fleet.bad),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Delete'),
          ),
        ],
      ),
    );
    if (ok != true || !mounted) return;
    try {
      await ref.read(apiProvider).deleteOrg(o.id);
      await _refresh();
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('Departments'),
        bottom: PreferredSize(
          preferredSize: const Size.fromHeight(28),
          child: Padding(
            padding: const EdgeInsets.only(left: 16, bottom: 8, right: 16),
            child: Align(
              alignment: Alignment.centerLeft,
              child: Text(
                'Who can see and drive which bots, set once per department.',
                style: TextStyle(color: Fleet.ink400, fontSize: 11),
              ),
            ),
          ),
        ),
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () => _edit(),
        icon: const Icon(Icons.add),
        label: const Text('New'),
      ),
      body: _buildBody(),
    );
  }

  Widget _buildBody() {
    if (_loading) return const Center(child: CircularProgressIndicator());
    if (_orgs.isEmpty && _error == null) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Text(
            'No departments yet.\n\nBots and secrets with no department are '
            'visible only to an administrator.',
            textAlign: TextAlign.center,
            style: TextStyle(color: Fleet.ink400, fontSize: 13, height: 1.4),
          ),
        ),
      );
    }

    return RefreshIndicator(
      onRefresh: _refresh,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(12, 12, 12, 88),
        children: [
          Padding(
            padding: const EdgeInsets.symmetric(horizontal: 4),
            child: InlineError(_error),
          ),
          for (final o in _orgs) _tile(o),
        ],
      ),
    );
  }

  Widget _tile(Org o) {
    return Card(
      color: Fleet.ink850,
      margin: const EdgeInsets.only(bottom: 8),
      child: ListTile(
        leading: Icon(Icons.apartment_outlined, size: 20, color: Fleet.ink300),
        title: Text(o.name,
            style: const TextStyle(fontSize: 14, fontWeight: FontWeight.w600)),
        subtitle: Text(
          '${o.memberCount} member${o.memberCount == 1 ? '' : 's'} · '
          '${o.botCount} bot${o.botCount == 1 ? '' : 's'}'
          '${o.description.isEmpty ? '' : ' · ${o.description}'}',
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
          style: TextStyle(color: Fleet.ink400, fontSize: 11),
        ),
        onTap: () => Navigator.of(context)
            .push(MaterialPageRoute(builder: (_) => OrgMembersScreen(org: o)))
            .then((_) => _refresh()),
        trailing: PopupMenuButton<String>(
          color: Fleet.ink850,
          icon: Icon(Icons.more_vert, size: 19, color: Fleet.ink400),
          onSelected: (a) => switch (a) {
            'edit' => _edit(o),
            'bots' => OrgBotsSheet.show(context, o).then((_) => _refresh()),
            _ => _delete(o),
          },
          itemBuilder: (_) => [
            const PopupMenuItem(
              value: 'bots',
              child: ListTile(
                  dense: true,
                  leading: Icon(Icons.smart_toy_outlined),
                  title: Text('Bots in this department')),
            ),
            const PopupMenuItem(
              value: 'edit',
              child: ListTile(
                  dense: true,
                  leading: Icon(Icons.edit_outlined),
                  title: Text('Rename')),
            ),
            PopupMenuItem(
              value: 'delete',
              child: ListTile(
                dense: true,
                leading: Icon(Icons.delete_outline, color: Fleet.bad),
                title: Text('Delete', style: TextStyle(color: Fleet.bad)),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
