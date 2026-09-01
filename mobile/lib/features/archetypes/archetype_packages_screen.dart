import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';
import 'manifest_yaml.dart';

/// Export an archetype as a portable `.agentfleet.yaml` manifest, or install
/// one someone sent you.
///
/// A phone has no natural home for a file the way a desktop does, so export
/// goes to the clipboard as YAML (paste it into a message, a gist, a repo)
/// and import is paste-based for the same reason. Credentials never travel
/// in a package — MCP env comes across as key names only, and the preview
/// says which ones you still owe the servers before they work.
class ArchetypePackagesScreen extends ConsumerStatefulWidget {
  const ArchetypePackagesScreen({super.key});

  @override
  ConsumerState<ArchetypePackagesScreen> createState() =>
      _ArchetypePackagesScreenState();
}

class _ArchetypePackagesScreenState
    extends ConsumerState<ArchetypePackagesScreen> {
  String? _error;
  String? _exporting;

  @override
  Widget build(BuildContext context) {
    final templates = ref.watch(templatesProvider);

    return Scaffold(
      appBar: AppBar(
        title: const Text('Archetype packages'),
        actions: [
          IconButton(
            tooltip: 'Install a package',
            icon: const Icon(Icons.download_outlined),
            onPressed: () => _openImport(context),
          ),
        ],
      ),
      body: templates.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (e, _) => Center(
          child: Padding(
            padding: const EdgeInsets.all(24),
            child: Text('Could not load archetypes: $e',
                style: TextStyle(color: Fleet.bad)),
          ),
        ),
        data: (list) => ListView(
          padding: const EdgeInsets.fromLTRB(16, 12, 16, 32),
          children: [
            if (_error != null)
              Padding(
                padding: const EdgeInsets.only(bottom: 8),
                child: InlineError(_error!),
              ),
            Text(
              'Tap an archetype to copy its manifest as YAML — persona, '
              'hardware profile, recorded skills and MCP registrations, never '
              'credentials. Share it however you share text.',
              style: TextStyle(color: Fleet.ink400, fontSize: 12),
            ),
            const SizedBox(height: 12),
            for (final t in list)
              Card(
                color: Fleet.ink850,
                child: ListTile(
                  leading: Icon(Icons.inventory_2_outlined, color: Fleet.ink300),
                  title: Text(t.name),
                  subtitle: Text(
                    '${t.category.isEmpty ? 'general' : t.category} · ${t.recommendedTier.isEmpty ? 'standard' : t.recommendedTier}',
                    style: TextStyle(color: Fleet.ink400, fontSize: 11),
                  ),
                  trailing: _exporting == t.id
                      ? const SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(strokeWidth: 2))
                      : const Icon(Icons.copy_outlined, size: 18),
                  onTap: _exporting == null ? () => _export(t) : null,
                ),
              ),
          ],
        ),
      ),
    );
  }

  Future<void> _export(BotTemplate t) async {
    setState(() {
      _exporting = t.id;
      _error = null;
    });
    try {
      final manifest = await ref.read(apiProvider).exportArchetype(t.id);
      await Clipboard.setData(ClipboardData(text: manifestToYaml(manifest)));
      if (!mounted) return;
      ScaffoldMessenger.of(context).showSnackBar(SnackBar(
        content: Text('${t.id}.agentfleet.yaml copied to the clipboard'),
      ));
    } catch (e) {
      setState(() => _error = '$e');
    } finally {
      if (mounted) setState(() => _exporting = null);
    }
  }

  Future<void> _openImport(BuildContext context) async {
    await showModalBottomSheet<void>(
      context: context,
      isScrollControlled: true,
      builder: (_) => const _ImportSheet(),
    );
  }
}

/// Paste → preview → install, with the itemised result the console shows.
class _ImportSheet extends ConsumerStatefulWidget {
  const _ImportSheet();

  @override
  ConsumerState<_ImportSheet> createState() => _ImportSheetState();
}

class _ImportSheetState extends ConsumerState<_ImportSheet> {
  final _text = TextEditingController();
  Map<String, dynamic>? _manifest;
  String? _error;
  bool _overwrite = false;
  bool _provision = false;
  final _botName = TextEditingController();
  bool _busy = false;
  ImportArchetypeResult? _result;

  @override
  void dispose() {
    _text.dispose();
    _botName.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final bottom = MediaQuery.of(context).viewInsets.bottom;
    return Padding(
      padding: EdgeInsets.fromLTRB(16, 16, 16, 16 + bottom),
      child: SingleChildScrollView(
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.stretch,
          mainAxisSize: MainAxisSize.min,
          children: [
            Text('Install a package',
                style: Theme.of(context).textTheme.titleMedium),
            const SizedBox(height: 10),
            if (_result != null)
              _resultView()
            else ...[
              TextField(
                controller: _text,
                minLines: 5,
                maxLines: 10,
                style: const TextStyle(fontFamily: 'monospace', fontSize: 12),
                decoration: const InputDecoration(
                  labelText: 'Paste the .agentfleet.yaml (or JSON) here',
                  alignLabelWithHint: true,
                ),
                onChanged: (_) => _preview(),
              ),
              if (_error != null)
                Padding(
                  padding: const EdgeInsets.only(top: 8),
                  child: InlineError(_error!),
                ),
              if (_manifest != null) ...[
                const SizedBox(height: 10),
                _previewView(_manifest!),
                SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('Replace skills that already exist'),
                  value: _overwrite,
                  onChanged: (v) => setState(() => _overwrite = v),
                ),
                SwitchListTile(
                  contentPadding: EdgeInsets.zero,
                  title: const Text('Also provision a bot from it'),
                  value: _provision,
                  onChanged: (v) => setState(() => _provision = v),
                ),
                if (_provision)
                  TextField(
                    controller: _botName,
                    decoration: const InputDecoration(labelText: 'Bot name'),
                  ),
                const SizedBox(height: 12),
                FilledButton(
                  onPressed: _busy ? null : _install,
                  child: _busy
                      ? const SizedBox(
                          width: 18,
                          height: 18,
                          child: CircularProgressIndicator(strokeWidth: 2))
                      : const Text('Install'),
                ),
              ],
            ],
          ],
        ),
      ),
    );
  }

  void _preview() {
    final raw = _text.text;
    if (raw.trim().isEmpty) {
      setState(() {
        _manifest = null;
        _error = null;
      });
      return;
    }
    try {
      final m = parseManifest(raw);
      setState(() {
        _manifest = m;
        _error = null;
      });
    } catch (e) {
      setState(() {
        _manifest = null;
        _error = 'Not a readable manifest: $e';
      });
    }
  }

  Widget _previewView(Map<String, dynamic> m) {
    List<dynamic> listOf(String key) =>
        (m[key] as List?) ?? const <dynamic>[];
    final servers = listOf('mcp_servers');
    // The env keys an installer still has to supply — credentials never
    // travel in a package, only the names of the holes they left.
    final needed = <String>[
      for (final s in servers)
        if (s is Map)
          for (final k in (s['env_keys'] as List? ?? const []))
            '${s['name']}.$k',
    ];
    Widget stat(String label, String value) => Expanded(
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(label.toUpperCase(),
                  style: TextStyle(
                      color: Fleet.ink400,
                      fontSize: 9,
                      fontWeight: FontWeight.w700)),
              Text(value, style: const TextStyle(fontSize: 13)),
            ],
          ),
        );
    return Container(
      padding: const EdgeInsets.all(12),
      decoration: BoxDecoration(
        color: Fleet.ink850,
        borderRadius: BorderRadius.circular(10),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Text('${m['name'] ?? m['id'] ?? 'unnamed'}',
              style: const TextStyle(fontWeight: FontWeight.w600)),
          Text(
            '${m['category'] ?? 'general'} · ${m['tier'] ?? 'standard'}',
            style: TextStyle(color: Fleet.ink400, fontSize: 11),
          ),
          const SizedBox(height: 8),
          Row(children: [
            stat('Tools', '${listOf('tools').length}'),
            stat('Skills', '${listOf('skills').length}'),
            stat('MCP servers', '${servers.length}'),
            stat('Shell', m['shell_access'] == true ? 'yes' : 'no'),
          ]),
          if (needed.isNotEmpty)
            Padding(
              padding: const EdgeInsets.only(top: 8),
              child: Text(
                'You will need to supply these yourself before the MCP '
                'servers work: ${needed.join(', ')}',
                style: TextStyle(color: Fleet.warn, fontSize: 11),
              ),
            ),
        ],
      ),
    );
  }

  Future<void> _install() async {
    final m = _manifest;
    if (m == null) return;
    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      final r = await ref.read(apiProvider).importArchetype(
            manifest: m,
            overwrite: _overwrite,
            createInstance: _provision,
            instanceName: _botName.text.trim(),
          );
      setState(() => _result = r);
    } catch (e) {
      setState(() => _error = '$e');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  Widget _resultView() {
    final r = _result!;
    final lines = <(String, Color?)>[
      if (r.skillsCreated.isNotEmpty)
        ('Skills created: ${r.skillsCreated.join(', ')}', Fleet.good),
      if (r.skillsSkipped.isNotEmpty)
        ('Skills skipped (already present): ${r.skillsSkipped.join(', ')}', null),
      if (r.mcpRegistered.isNotEmpty)
        ('MCP servers registered: ${r.mcpRegistered.join(', ')}', Fleet.good),
      if (r.mcpFailed.isNotEmpty)
        ('MCP servers not registered: ${r.mcpFailed.join(', ')}', Fleet.warn),
      if (r.needsSecrets.isNotEmpty)
        ('Credentials still needed: ${r.needsSecrets.join(', ')}', Fleet.warn),
      if (r.instanceName.isNotEmpty)
        ('Bot provisioned: ${r.instanceName} (${r.instanceStatus})', Fleet.good),
    ];
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text('Installed ${r.archetype}',
            style: const TextStyle(fontWeight: FontWeight.w600)),
        const SizedBox(height: 6),
        if (lines.isEmpty)
          Text('Nothing was installed: everything in the package was already present.',
              style: TextStyle(color: Fleet.ink400, fontSize: 12))
        else
          for (final (text, color) in lines)
            Padding(
              padding: const EdgeInsets.only(bottom: 3),
              child: Text(text,
                  style: TextStyle(color: color ?? Fleet.ink300, fontSize: 12)),
            ),
        const SizedBox(height: 12),
        FilledButton(
          onPressed: () => Navigator.of(context).pop(),
          child: const Text('Done'),
        ),
      ],
    );
  }
}
