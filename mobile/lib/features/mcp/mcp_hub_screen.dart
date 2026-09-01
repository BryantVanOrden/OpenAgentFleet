import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';

/// MCP tool servers mounted on the fleet, and the tools discovered on them.
///
/// A server is registered once and its tools become available to every agent,
/// which is why this lives under Admin: mounting one changes what the whole
/// fleet can do, not what one bot prefers.
class McpHubScreen extends ConsumerStatefulWidget {
  const McpHubScreen({super.key});

  @override
  ConsumerState<McpHubScreen> createState() => _McpHubScreenState();
}

class _McpHubScreenState extends ConsumerState<McpHubScreen> {
  List<McpServer> _servers = const [];
  List<McpTool> _tools = const [];
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
      final api = ref.read(apiProvider);
      final results = await Future.wait([api.mcpServers(), api.mcpTools()]);
      if (mounted) {
        setState(() {
          _servers = results[0] as List<McpServer>;
          _tools = results[1] as List<McpTool>;
        });
      }
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _disconnect(McpServer s) async {
    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => AlertDialog(
        title: Text('Disconnect "${s.name}"?'),
        content: Text(
          'Its ${s.toolsCount} tool${s.toolsCount == 1 ? '' : 's'} disappear '
          'from every agent immediately. A task mid-run that reaches for one '
          'will fail that step.',
          style: TextStyle(color: Fleet.ink300),
        ),
        actions: [
          TextButton(
              onPressed: () => Navigator.pop(ctx, false),
              child: const Text('Cancel')),
          FilledButton(
            style: FilledButton.styleFrom(backgroundColor: Fleet.bad),
            onPressed: () => Navigator.pop(ctx, true),
            child: const Text('Disconnect'),
          ),
        ],
      ),
    );
    if (ok != true) return;
    try {
      await ref.read(apiProvider).deleteMcpServer(s.id);
      await _load();
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: const Text('MCP hub'),
        actions: [
          IconButton(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: _load,
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: () async {
          final created = await _AddServerSheet.show(context);
          if (created == true) _load();
        },
        icon: const Icon(Icons.add),
        label: const Text('Connect server'),
      ),
      body: _loading && _servers.isEmpty
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
                  Text(
                    'Model Context Protocol servers mount external tools — '
                    'GitHub, Postgres, Slack, search — onto every agent, '
                    'beyond the sandbox\'s own shell and browser.',
                    style: TextStyle(
                        color: Fleet.ink400, fontSize: 12, height: 1.4),
                  ),
                  const SizedBox(height: 12),
                  _sectionLabel('Connected servers (${_servers.length})'),
                  if (_servers.isEmpty && !_loading)
                    Padding(
                      padding: const EdgeInsets.symmetric(vertical: 16),
                      child: Text(
                        'No MCP servers connected. Mount one to expand what '
                        'your agents can reach.',
                        style: TextStyle(color: Fleet.ink400, fontSize: 12),
                      ),
                    ),
                  for (final s in _servers) _serverCard(s),
                  if (_tools.isNotEmpty) ...[
                    const SizedBox(height: 20),
                    _sectionLabel('Discovered tools (${_tools.length})'),
                    for (final t in _tools) _toolTile(t),
                  ],
                ],
              ),
            ),
    );
  }

  Widget _sectionLabel(String label) => Padding(
        padding: const EdgeInsets.only(bottom: 4),
        child: Text(
          label.toUpperCase(),
          style: TextStyle(
              color: Fleet.ink400,
              fontSize: 10,
              fontWeight: FontWeight.w700,
              letterSpacing: 0.7),
        ),
      );

  Widget _serverCard(McpServer s) => Container(
        margin: const EdgeInsets.only(top: 8),
        padding: const EdgeInsets.all(12),
        decoration: BoxDecoration(
          color: Fleet.ink900,
          borderRadius: BorderRadius.circular(10),
          border: Border.all(color: Fleet.ink800),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Row(
              children: [
                Container(
                  width: 8,
                  height: 8,
                  decoration: BoxDecoration(
                    color: s.active ? Fleet.good : Fleet.ink500,
                    shape: BoxShape.circle,
                  ),
                ),
                const SizedBox(width: 8),
                Expanded(
                  child: Text(s.name,
                      style: const TextStyle(
                          fontSize: 14, fontWeight: FontWeight.w600)),
                ),
                Container(
                  padding:
                      const EdgeInsets.symmetric(horizontal: 7, vertical: 2),
                  decoration: BoxDecoration(
                    color: Fleet.ink800,
                    borderRadius: BorderRadius.circular(5),
                  ),
                  child: Text(s.transport,
                      style: TextStyle(
                          fontFamily: 'monospace',
                          fontSize: 10,
                          color: Fleet.ink300)),
                ),
              ],
            ),
            const SizedBox(height: 8),
            Container(
              width: double.infinity,
              padding: const EdgeInsets.all(8),
              decoration: BoxDecoration(
                color: Fleet.ink950,
                borderRadius: BorderRadius.circular(6),
                border: Border.all(color: Fleet.ink850),
              ),
              child: SelectableText(
                s.endpoint,
                style: TextStyle(
                    fontFamily: 'monospace',
                    fontSize: 11,
                    color: Fleet.ink300),
              ),
            ),
            const SizedBox(height: 8),
            Row(
              children: [
                Expanded(
                  child: Text(
                    '${s.toolsCount} tool${s.toolsCount == 1 ? '' : 's'} available',
                    style: TextStyle(
                        fontFamily: 'monospace',
                        fontSize: 10,
                        color: Fleet.ink500),
                  ),
                ),
                TextButton(
                  style: TextButton.styleFrom(foregroundColor: Fleet.bad),
                  onPressed: () => _disconnect(s),
                  child:
                      const Text('Disconnect', style: TextStyle(fontSize: 12)),
                ),
              ],
            ),
          ],
        ),
      );

  Widget _toolTile(McpTool t) => Container(
        margin: const EdgeInsets.only(top: 8),
        padding: const EdgeInsets.all(10),
        decoration: BoxDecoration(
          color: Fleet.ink950,
          borderRadius: BorderRadius.circular(8),
          border: Border.all(color: Fleet.ink800),
        ),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(t.name,
                style: TextStyle(
                    fontFamily: 'monospace',
                    fontSize: 12,
                    fontWeight: FontWeight.w700,
                    color: Fleet.live)),
            if (t.description.isNotEmpty) ...[
              const SizedBox(height: 3),
              Text(t.description,
                  style: TextStyle(
                      color: Fleet.ink400, fontSize: 11, height: 1.35)),
            ],
          ],
        ),
      );
}

/// Connect a new MCP server — the same three facts the console asks for.
class _AddServerSheet extends ConsumerStatefulWidget {
  const _AddServerSheet();

  static Future<bool?> show(BuildContext context) => showModalBottomSheet<bool>(
        context: context,
        isScrollControlled: true,
        backgroundColor: Fleet.ink900,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
        ),
        builder: (_) => const _AddServerSheet(),
      );

  @override
  ConsumerState<_AddServerSheet> createState() => _AddServerSheetState();
}

class _AddServerSheetState extends ConsumerState<_AddServerSheet> {
  final _name = TextEditingController();
  final _command = TextEditingController();
  String _transport = 'stdio';
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _command.dispose();
    super.dispose();
  }

  Future<void> _connect() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(apiProvider).registerMcpServer(
            name: _name.text.trim(),
            transport: _transport,
            command: _command.text.trim(),
          );
      if (mounted) Navigator.pop(context, true);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final inset = MediaQuery.of(context).viewInsets.bottom;
    final complete =
        _name.text.trim().isNotEmpty && _command.text.trim().isNotEmpty;

    return Padding(
      padding: EdgeInsets.fromLTRB(20, 18, 20, 18 + inset),
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                const Icon(Icons.power_outlined, size: 20),
                const SizedBox(width: 8),
                Text('Connect MCP server',
                    style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 16),
            TextField(
              controller: _name,
              autofocus: true,
              autocorrect: false,
              decoration: const InputDecoration(
                labelText: 'Server name',
                hintText: 'github',
                helperText: 'e.g. github, postgres, brave_search',
              ),
              onChanged: (_) => setState(() {}),
            ),
            const SizedBox(height: 12),
            DropdownButtonFormField<String>(
              initialValue: _transport,
              isExpanded: true,
              decoration: const InputDecoration(labelText: 'Transport'),
              dropdownColor: Fleet.ink850,
              items: const [
                DropdownMenuItem(
                    value: 'stdio',
                    child: Text('stdio (subprocess / command)')),
                DropdownMenuItem(
                    value: 'sse',
                    child: Text('sse (remote Server-Sent Events URL)')),
              ],
              onChanged: (v) => setState(() => _transport = v ?? 'stdio'),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _command,
              autocorrect: false,
              style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
              decoration: InputDecoration(
                labelText: _transport == 'sse' ? 'URL' : 'Command or URL',
                hintText: _transport == 'sse'
                    ? 'https://mcp.example.com/sse'
                    : 'npx -y @modelcontextprotocol/server-github',
              ),
              onChanged: (_) => setState(() {}),
            ),
            InlineError(_error),
            const SizedBox(height: 14),
            FilledButton(
              onPressed: _busy || !complete ? null : _connect,
              child: Text(_busy ? 'Connecting...' : 'Connect server'),
            ),
          ],
        ),
      ),
    );
  }
}
