import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import 'mini_app_screen.dart';
import 'work_editor_screen.dart';

class VaultScreen extends ConsumerStatefulWidget {
  const VaultScreen({super.key});

  @override
  ConsumerState<VaultScreen> createState() => _VaultScreenState();
}

class _VaultScreenState extends ConsumerState<VaultScreen> with SingleTickerProviderStateMixin {
  // Two tabs now: comms moved to Fleet, where the agents are.
  late final TabController _tabs = TabController(length: 3, vsync: this);

  List<SharedSecret> _secrets = [];
  List<SharedSession> _sessions = [];
  List<WorkItem> _work = [];

  /// The folder being looked at; empty is the top level.
  String _cwd = '';
  bool _loading = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _load();
  }

  @override
  void dispose() {
    _tabs.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final api = ref.read(apiProvider);
      final secs = await api.sharedSecrets();
      final sess = await api.sharedSessions();
      final work = await api.workItems();
      if (mounted) {
        setState(() {
          _secrets = secs;
          _sessions = sess;
          _work = work;
        });
      }
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _showAddSecretDialog() async {
    final keyCtrl = TextEditingController();
    final valCtrl = TextEditingController();
    final noteCtrl = TextEditingController();
    String scope = 'fleet';

    await showModalBottomSheet(
      context: context,
      isScrollControlled: true,
      backgroundColor: Fleet.ink900,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(16)),
      ),
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setModalState) => Padding(
          padding: EdgeInsets.only(
            left: 20,
            right: 20,
            top: 20,
            bottom: MediaQuery.of(ctx).viewInsets.bottom + 20,
          ),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              const Text('🔑 Publish Shared Secret / Variable',
                  style: TextStyle(fontSize: 16, fontWeight: FontWeight.bold)),
              const SizedBox(height: 12),
              TextField(
                controller: keyCtrl,
                decoration: const InputDecoration(
                  labelText: 'Key Name (e.g. STRIPE_API_KEY)',
                  hintText: 'KEY_NAME',
                ),
              ),
              const SizedBox(height: 8),
              TextField(
                controller: valCtrl,
                maxLines: 2,
                decoration: const InputDecoration(
                  labelText: 'Secret Value',
                  hintText: 'Value or API token...',
                ),
              ),
              const SizedBox(height: 8),
              TextField(
                controller: noteCtrl,
                decoration: const InputDecoration(
                  labelText: 'Note (optional)',
                  hintText: 'What is this used for?',
                ),
              ),
              const SizedBox(height: 16),
              Row(
                mainAxisAlignment: MainAxisAlignment.end,
                children: [
                  TextButton(
                    onPressed: () => Navigator.pop(ctx),
                    child: const Text('Cancel'),
                  ),
                  const SizedBox(width: 8),
                  FilledButton(
                    onPressed: () async {
                      if (keyCtrl.text.trim().isEmpty || valCtrl.text.trim().isEmpty) return;
                      final key = keyCtrl.text.trim();
                      final val = valCtrl.text.trim();
                      final note = noteCtrl.text.trim();
                      Navigator.pop(ctx);
                      try {
                        final api = ref.read(apiProvider);
                        await api.putSharedSecret(
                          key: key,
                          value: val,
                          scope: scope,
                          note: note,
                        );
                        _load();
                      } catch (err) {
                        if (mounted) {
                          ScaffoldMessenger.of(context).showSnackBar(
                            SnackBar(content: Text('Error: $err'), backgroundColor: Fleet.bad),
                          );
                        }
                      }
                    },
                    child: const Text('Save to Vault'),
                  ),
                ],
              ),
            ],
          ),
        ),
      ),
    );
  }

  @override
  Widget build(BuildContext context) {
    // Reload when this tab is opened. The shell keeps every tab alive in an
    // IndexedStack, so loading in initState alone meant showing whatever was
    // fetched when the app started, for the rest of the session.
    ref.listen(tabRefreshProvider(Tabs.vault), (_, __) => _load());

    return Scaffold(
      appBar: AppBar(
        title: const Text('Vault'),
        actions: [
          IconButton(
            icon: const Icon(Icons.refresh),
            onPressed: _load,
          ),
          IconButton(
            icon: const Icon(Icons.add_circle_outline),
            tooltip: 'Add Secret',
            onPressed: _showAddSecretDialog,
          ),
        ],
        bottom: TabBar(
          controller: _tabs,
          indicatorColor: Fleet.live,
          labelColor: Fleet.ink100,
          unselectedLabelColor: Fleet.ink400,
          tabs: [
            Tab(text: 'Secrets (${_secrets.length})'),
            Tab(text: 'Sessions (${_sessions.length})'),
            Tab(text: 'Work (${_work.length})'),
          ],
        ),
      ),
      body: _loading && _secrets.isEmpty && _sessions.isEmpty
          ? const Center(child: CircularProgressIndicator())
          : _error != null && _secrets.isEmpty && _sessions.isEmpty
              ? Center(child: Text(_error!, style: TextStyle(color: Fleet.bad)))
              : TabBarView(
                  controller: _tabs,
                  children: [
                    _buildSecretsTab(),
                    _buildSessionsTab(),
                    _buildWorkTab(),
                  ],
                ),
    );
  }

  Widget _buildSecretsTab() {
    if (_secrets.isEmpty) {
      return Center(
        child: Text('No shared secrets in vault.', style: TextStyle(color: Fleet.ink400)),
      );
    }
    return ListView.builder(
      padding: const EdgeInsets.all(12),
      itemCount: _secrets.length,
      itemBuilder: (ctx, i) {
        final sec = _secrets[i];
        return Card(
          color: Fleet.ink900,
          margin: const EdgeInsets.only(bottom: 8),
          child: ListTile(
            title: Text(sec.key,
                style: TextStyle(
                    fontFamily: 'monospace',
                    fontWeight: FontWeight.bold,
                    color: Fleet.live)),
            subtitle: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                const SizedBox(height: 4),
                // The value is never sent to clients — show that one exists.
                Text(
                    sec.hasValue
                        ? '•' * 12 + '  value stored, never displayed'
                        : 'no value set',
                    maxLines: 1,
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(
                        fontFamily: 'monospace',
                        color: Fleet.ink400,
                        fontSize: 12)),
                if (sec.note.isNotEmpty)
                  Text(sec.note, style: TextStyle(color: Fleet.ink400, fontSize: 11)),
              ],
            ),
            trailing: IconButton(
              icon: const Icon(Icons.delete_outline, size: 18, color: Colors.redAccent),
              onPressed: () async {
                final api = ref.read(apiProvider);
                await api.deleteSharedSecret(sec.key);
                _load();
              },
            ),
          ),
        );
      },
    );
  }

  /// The catalog, as a file system.
  ///
  /// It used to be one flat list with workspaces as headings, which was fine
  /// while the agents had published a dozen things and unreadable by fifty.
  /// This walks folders one level at a time, the way a person expects, and
  /// every item can be renamed, moved, edited or deleted -- including the
  /// runnable ones, which need a long press because a tap plays them.
  Widget _buildWorkTab() {
    final here = _work.where((w) => w.parentId == _cwd).toList()
      ..sort((a, b) {
        if (a.isWorkspace != b.isWorkspace) return a.isWorkspace ? -1 : 1;
        return a.name.toLowerCase().compareTo(b.name.toLowerCase());
      });

    return Column(
      children: [
        _breadcrumb(),
        Expanded(
          child: RefreshIndicator(
            onRefresh: _load,
            child: here.isEmpty
                ? ListView(
                    padding: const EdgeInsets.fromLTRB(16, 60, 16, 32),
                    children: [
                      Text(
                        _cwd.isEmpty
                            ? 'Nothing published yet.\n\nAgents put work here for '
                                'each other — files to build on, and mini-apps you '
                                'can run from this tab.'
                            : 'This folder is empty.',
                        textAlign: TextAlign.center,
                        style: TextStyle(color: Fleet.ink400, height: 1.5),
                      ),
                    ],
                  )
                : ListView(
                    padding: const EdgeInsets.fromLTRB(16, 8, 16, 32),
                    children: [for (final item in here) _workTile(item)],
                  ),
          ),
        ),
      ],
    );
  }

  /// Where you are, and the way back out.
  Widget _breadcrumb() {
    final path = _pathTo(_cwd);
    return Material(
      color: Fleet.ink900,
      child: Padding(
        padding: const EdgeInsets.fromLTRB(8, 6, 8, 6),
        child: Row(
          children: [
            if (_cwd.isNotEmpty)
              IconButton(
                tooltip: 'Up',
                icon: const Icon(Icons.arrow_upward, size: 18),
                onPressed: () => setState(() =>
                    _cwd = path.length > 1 ? path[path.length - 2].id : ''),
              ),
            Expanded(
              child: SingleChildScrollView(
                scrollDirection: Axis.horizontal,
                reverse: true,
                child: Row(
                  children: [
                    InkWell(
                      onTap: () => setState(() => _cwd = ''),
                      child: Padding(
                        padding: const EdgeInsets.symmetric(
                            horizontal: 6, vertical: 6),
                        child: Row(children: [
                          Icon(Icons.inventory_2_outlined,
                              size: 14, color: Fleet.ink400),
                          const SizedBox(width: 5),
                          Text('Shared work',
                              style: TextStyle(
                                  color: _cwd.isEmpty
                                      ? Fleet.ink200
                                      : Fleet.ink400,
                                  fontSize: 12,
                                  fontWeight: FontWeight.w600)),
                        ]),
                      ),
                    ),
                    for (final crumb in path) ...[
                      Icon(Icons.chevron_right, size: 14, color: Fleet.ink400),
                      InkWell(
                        onTap: () => setState(() => _cwd = crumb.id),
                        child: Padding(
                          padding: const EdgeInsets.symmetric(
                              horizontal: 6, vertical: 6),
                          child: Text(crumb.name,
                              style: TextStyle(
                                  color: crumb.id == _cwd
                                      ? Fleet.ink200
                                      : Fleet.ink400,
                                  fontSize: 12,
                                  fontWeight: FontWeight.w600)),
                        ),
                      ),
                    ],
                  ],
                ),
              ),
            ),
            IconButton(
              tooltip: 'New folder',
              icon: const Icon(Icons.create_new_folder_outlined, size: 20),
              onPressed: _createFolder,
            ),
            IconButton(
              tooltip: 'New file',
              icon: const Icon(Icons.note_add_outlined, size: 20),
              onPressed: _createFile,
            ),
          ],
        ),
      ),
    );
  }

  /// The chain of folders from the root down to [id].
  List<WorkItem> _pathTo(String id) {
    final out = <WorkItem>[];
    var at = id;
    // Bounded: a cycle here would hang the tab, and the server refuses to
    // create one, but a listing can still arrive mid-move.
    for (var hops = 0; at.isNotEmpty && hops < 64; hops++) {
      final match = _work.where((w) => w.id == at);
      if (match.isEmpty) break;
      out.insert(0, match.first);
      at = match.first.parentId;
    }
    return out;
  }

  Widget _workTile(WorkItem item) {
    final runnable = item.runnable;
    final folder = item.isWorkspace;
    final childCount =
        folder ? _work.where((w) => w.parentId == item.id).length : 0;

    return Card(
      color: Fleet.ink850,
      margin: const EdgeInsets.only(bottom: 8),
      child: ListTile(
        leading: Icon(
          folder
              ? Icons.folder_rounded
              : runnable
                  ? Icons.videogame_asset_outlined
                  : Icons.description_outlined,
          color: folder
              ? Fleet.cool
              : runnable
                  ? Fleet.live
                  : Fleet.ink300,
          size: 20,
        ),
        title: Text(item.name, style: const TextStyle(fontSize: 14)),
        subtitle: Text(
          folder
              ? '$childCount item${childCount == 1 ? '' : 's'}'
              : [
                  if (item.createdByName.isNotEmpty) 'by ${item.createdByName}',
                  'v${item.version}',
                  if (item.description.isNotEmpty) item.description,
                ].join(' · '),
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
          style: TextStyle(color: Fleet.ink400, fontSize: 11),
        ),
        trailing: folder
            ? Icon(Icons.chevron_right, size: 18, color: Fleet.ink400)
            : IconButton(
                tooltip: 'More',
                icon: Icon(Icons.more_vert, size: 18, color: Fleet.ink400),
                onPressed: () => _itemMenu(item),
              ),
        onTap: () => _openItem(item),
        // A tap on a game plays it, so everything else lives behind a hold.
        onLongPress: () => _itemMenu(item),
      ),
    );
  }

  void _openItem(WorkItem item) {
    if (item.isWorkspace) {
      setState(() => _cwd = item.id);
      return;
    }
    if (item.runnable) {
      Navigator.of(context).push(MaterialPageRoute(
        builder: (_) => MiniAppScreen(item: item),
      ));
      return;
    }
    _editItem(item);
  }

  Future<void> _editItem(WorkItem item) async {
    final saved = await Navigator.of(context).push<bool>(MaterialPageRoute(
      builder: (_) => WorkEditorScreen(api: ref.read(apiProvider), item: item),
    ));
    if (saved == true) await _load();
  }

  /// Everything you can do to one item.
  Future<void> _itemMenu(WorkItem item) async {
    final runnable = item.runnable;
    await showModalBottomSheet<void>(
      context: context,
      backgroundColor: Fleet.ink900,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
      ),
      builder: (sheet) => SafeArea(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 16, 20, 8),
              child: Row(
                children: [
                  Icon(
                    item.isWorkspace
                        ? Icons.folder_rounded
                        : runnable
                            ? Icons.videogame_asset_outlined
                            : Icons.description_outlined,
                    size: 18,
                    color: Fleet.ink300,
                  ),
                  const SizedBox(width: 10),
                  Expanded(
                    child: Text(item.name,
                        style: const TextStyle(
                            fontSize: 15, fontWeight: FontWeight.w600)),
                  ),
                ],
              ),
            ),
            if (runnable)
              ListTile(
                leading: const Icon(Icons.play_arrow_rounded),
                title: const Text('Play'),
                onTap: () {
                  Navigator.pop(sheet);
                  Navigator.of(context).push(MaterialPageRoute(
                    builder: (_) => MiniAppScreen(item: item),
                  ));
                },
              ),
            if (!item.isWorkspace)
              ListTile(
                leading: const Icon(Icons.edit_outlined),
                // The point of a hold on a game: its source is still a file.
                title: Text(runnable ? 'Edit source' : 'Edit'),
                onTap: () {
                  Navigator.pop(sheet);
                  _editItem(item);
                },
              ),
            ListTile(
              leading: const Icon(Icons.drive_file_rename_outline),
              title: const Text('Rename'),
              onTap: () {
                Navigator.pop(sheet);
                _renameWork(item);
              },
            ),
            ListTile(
              leading: const Icon(Icons.drive_file_move_outline),
              title: const Text('Move to folder'),
              onTap: () {
                Navigator.pop(sheet);
                _moveWork(item);
              },
            ),
            ListTile(
              leading: Icon(Icons.delete_outline, color: Fleet.bad),
              title: Text('Delete', style: TextStyle(color: Fleet.bad)),
              onTap: () {
                Navigator.pop(sheet);
                _deleteWork(item);
              },
            ),
            const SizedBox(height: 8),
          ],
        ),
      ),
    );
  }

  Future<String?> _askName(String title, {String initial = ''}) {
    final controller = TextEditingController(text: initial);
    return showDialog<String>(
      context: context,
      builder: (ctx) => AlertDialog(
        backgroundColor: Fleet.ink850,
        title: Text(title),
        content: TextField(
          controller: controller,
          autofocus: true,
          decoration: const InputDecoration(hintText: 'Name'),
          onSubmitted: (v) => Navigator.pop(ctx, v.trim()),
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx), child: const Text('Cancel')),
          FilledButton(
            onPressed: () => Navigator.pop(ctx, controller.text.trim()),
            child: const Text('OK'),
          ),
        ],
      ),
    );
  }

  Future<void> _createFolder() async {
    final name = await _askName('New folder');
    if (name == null || name.isEmpty) return;
    await _guard(() async {
      await ref.read(apiProvider).putWorkItem(
            name: name,
            kind: WorkItem.kindWorkspace,
            parentId: _cwd,
          );
    });
  }

  Future<void> _createFile() async {
    final name = await _askName('New file');
    if (name == null || name.isEmpty) return;
    await _guard(() async {
      final created = await ref.read(apiProvider).putWorkItem(
            name: name,
            kind: WorkItem.kindFile,
            content: '',
            parentId: _cwd,
          );
      if (mounted) await _editItem(created);
    });
  }

  Future<void> _renameWork(WorkItem item) async {
    final name = await _askName('Rename', initial: item.name);
    if (name == null || name.isEmpty || name == item.name) return;
    await _guard(() async {
      await ref.read(apiProvider).moveWorkItem(item.id, name: name);
    });
  }

  /// Move [item] into another folder, or back out to the top level.
  Future<void> _moveWork(WorkItem item) async {
    // A folder cannot go inside itself or anything it contains. The server
    // refuses either way; leaving them out of the list means never offering a
    // choice that will only be rejected.
    bool insideItem(WorkItem candidate) {
      var at = candidate.id;
      for (var hops = 0; at.isNotEmpty && hops < 64; hops++) {
        if (at == item.id) return true;
        final match = _work.where((w) => w.id == at);
        if (match.isEmpty) return false;
        at = match.first.parentId;
      }
      return false;
    }

    final folders = _work
        .where((w) => w.isWorkspace && w.id != item.id && !insideItem(w))
        .toList()
      ..sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));

    final target = await showModalBottomSheet<String>(
      context: context,
      backgroundColor: Fleet.ink900,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
      ),
      builder: (sheet) => SafeArea(
        child: ListView(
          shrinkWrap: true,
          children: [
            Padding(
              padding: const EdgeInsets.fromLTRB(20, 16, 20, 8),
              child: Text('Move "${item.name}" to',
                  style: const TextStyle(
                      fontSize: 15, fontWeight: FontWeight.w600)),
            ),
            ListTile(
              leading: const Icon(Icons.inventory_2_outlined),
              title: const Text('Top level'),
              enabled: item.parentId.isNotEmpty,
              onTap: () => Navigator.pop(sheet, ''),
            ),
            for (final f in folders)
              ListTile(
                leading: const Icon(Icons.folder_rounded),
                title: Text(f.name),
                enabled: f.id != item.parentId,
                onTap: () => Navigator.pop(sheet, f.id),
              ),
          ],
        ),
      ),
    );
    if (target == null) return;
    await _guard(() async {
      await ref.read(apiProvider).moveWorkItem(item.id, parentId: target);
    });
  }

  /// Run a catalog change, then reload — and put the reason on screen when the
  /// server refuses, because it refuses for reasons worth reading.
  Future<void> _guard(Future<void> Function() action) async {
    try {
      await action();
      await _load();
    } catch (err) {
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(backgroundColor: Fleet.bad, content: Text('$err')),
      );
    }
  }

  Future<void> _deleteWork(WorkItem item) async {
    final childCount = _work.where((w) => w.parentId == item.id).length;
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        backgroundColor: Fleet.ink850,
        title: Text('Delete "${item.name}"?'),
        content: Text(
          item.isWorkspace
              ? (childCount == 0
                  ? 'The folder is empty.'
                  : 'The $childCount item${childCount == 1 ? '' : 's'} inside '
                      'go with it.')
              : 'The agents lose what they published here.',
          style: TextStyle(color: Fleet.ink300),
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
    if (ok != true) return;
    await _guard(() async {
      await ref.read(apiProvider).deleteWorkItem(item.id);
      // Standing inside something that no longer exists shows an empty folder
      // with a breadcrumb to nowhere.
      if (item.id == _cwd) setState(() => _cwd = item.parentId);
    });
  }


  Widget _buildSessionsTab() {
    if (_sessions.isEmpty) {
      return Center(
        child: Text('No shared browser sessions yet.', style: TextStyle(color: Fleet.ink400)),
      );
    }
    return ListView.builder(
      padding: const EdgeInsets.all(12),
      itemCount: _sessions.length,
      itemBuilder: (ctx, i) {
        final sess = _sessions[i];
        return Card(
          color: Fleet.ink900,
          margin: const EdgeInsets.only(bottom: 8),
          child: ListTile(
            leading: const Icon(Icons.cookie_outlined, color: Colors.amberAccent),
            title: Text(sess.domain, style: const TextStyle(fontWeight: FontWeight.bold)),
            subtitle: Text(
              '${sess.title.isNotEmpty ? sess.title : sess.domain}\nCookies: ${sess.cookiesJson.length} bytes',
              style: TextStyle(color: Fleet.ink300, fontSize: 11),
            ),
          ),
        );
      },
    );
  }
}
