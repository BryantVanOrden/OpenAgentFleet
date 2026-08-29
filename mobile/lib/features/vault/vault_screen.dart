import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import 'mini_app_screen.dart';

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

  /// What the agents have made, for each other and for you.
  ///
  /// Workspaces come first with their contents nested under them, because a
  /// flat list of thirty files from four bots tells you nothing about which
  /// of them belong to the same piece of work.
  Widget _buildWorkTab() {
    if (_work.isEmpty) {
      return Center(
        child: Padding(
          padding: const EdgeInsets.all(32),
          child: Text(
            'Nothing published yet.\n\nAgents put work here for each other — '
            'files to build on, and mini-apps you can run from this tab.',
            textAlign: TextAlign.center,
            style: TextStyle(color: Fleet.ink400, height: 1.5),
          ),
        ),
      );
    }

    final workspaces = _work.where((w) => w.isWorkspace).toList();
    final loose = _work
        .where((w) => !w.isWorkspace && w.parentId.isEmpty)
        .toList();

    return RefreshIndicator(
      onRefresh: _load,
      child: ListView(
        padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
        children: [
          for (final ws in workspaces) ...[
            Padding(
              padding: const EdgeInsets.only(top: 8, bottom: 6, left: 4),
              child: Row(
                children: [
                  Icon(Icons.folder_outlined, size: 16, color: Fleet.ink400),
                  const SizedBox(width: 6),
                  Expanded(
                    child: Text(
                      ws.name,
                      style: TextStyle(
                          color: Fleet.ink300,
                          fontSize: 12,
                          fontWeight: FontWeight.w700),
                    ),
                  ),
                ],
              ),
            ),
            for (final item in _work.where((w) => w.parentId == ws.id))
              _workTile(item),
          ],
          if (loose.isNotEmpty) ...[
            if (workspaces.isNotEmpty)
              Padding(
                padding: const EdgeInsets.only(top: 12, bottom: 6, left: 4),
                child: Text('LOOSE ITEMS',
                    style: TextStyle(
                        color: Fleet.ink400,
                        fontSize: 10,
                        fontWeight: FontWeight.w700,
                        letterSpacing: 0.6)),
              ),
            for (final item in loose) _workTile(item),
          ],
        ],
      ),
    );
  }

  Widget _workTile(WorkItem item) {
    final runnable = item.runnable;
    return Card(
      color: Fleet.ink850,
      margin: const EdgeInsets.only(bottom: 8),
      child: ListTile(
        leading: Icon(
          runnable ? Icons.videogame_asset_outlined : Icons.description_outlined,
          color: runnable ? Fleet.live : Fleet.ink300,
          size: 20,
        ),
        title: Text(item.name, style: const TextStyle(fontSize: 14)),
        subtitle: Text(
          [
            if (item.createdByName.isNotEmpty) 'by ${item.createdByName}',
            'v${item.version}',
            if (item.description.isNotEmpty) item.description,
          ].join(' · '),
          maxLines: 2,
          overflow: TextOverflow.ellipsis,
          style: TextStyle(color: Fleet.ink400, fontSize: 11),
        ),
        trailing: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            if (runnable)
              TextButton.icon(
                onPressed: () => Navigator.of(context).push(MaterialPageRoute(
                  builder: (_) => MiniAppScreen(item: item),
                )),
                icon: const Icon(Icons.play_arrow_rounded, size: 18),
                label: const Text('Play'),
              ),
            IconButton(
              tooltip: 'Delete',
              icon: Icon(Icons.delete_outline, size: 18, color: Fleet.bad),
              onPressed: () => _deleteWork(item),
            ),
          ],
        ),
        onTap: runnable
            ? () => Navigator.of(context).push(MaterialPageRoute(
                  builder: (_) => MiniAppScreen(item: item),
                ))
            : () => _showWork(item),
      ),
    );
  }

  Future<void> _showWork(WorkItem item) => showModalBottomSheet<void>(
        context: context,
        isScrollControlled: true,
        backgroundColor: Fleet.ink900,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
        ),
        builder: (_) => DraggableScrollableSheet(
          expand: false,
          initialChildSize: 0.7,
          builder: (_, scroll) => Padding(
            padding: const EdgeInsets.fromLTRB(20, 18, 20, 18),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.stretch,
              children: [
                Text(item.name,
                    style: Theme.of(context).textTheme.titleMedium),
                const SizedBox(height: 4),
                Text('by ${item.createdByName} · v${item.version}',
                    style: TextStyle(color: Fleet.ink400, fontSize: 11)),
                const SizedBox(height: 12),
                Expanded(
                  child: SingleChildScrollView(
                    controller: scroll,
                    child: SelectableText(
                      item.content,
                      style: const TextStyle(
                          fontFamily: 'monospace', fontSize: 11, height: 1.4),
                    ),
                  ),
                ),
              ],
            ),
          ),
        ),
      );

  Future<void> _deleteWork(WorkItem item) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        backgroundColor: Fleet.ink850,
        title: Text('Delete "${item.name}"?'),
        content: Text(
          item.isWorkspace
              ? 'Everything inside this workspace goes with it.'
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
    try {
      await ref.read(apiProvider).deleteWorkItem(item.id);
      await _load();
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    }
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
