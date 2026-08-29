import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';

/// Add or configure one AI connection.
///
/// The model is picked from what the engine actually serves rather than typed
/// from memory: a misremembered tag fails at the first request, long after the
/// point where it could have been noticed.
class ProviderEditSheet extends ConsumerStatefulWidget {
  const ProviderEditSheet({super.key, this.existing});

  final AIProvider? existing;

  static Future<bool?> show(BuildContext context, {AIProvider? existing}) =>
      showModalBottomSheet<bool>(
        context: context,
        isScrollControlled: true,
        builder: (_) => ProviderEditSheet(existing: existing),
      );

  @override
  ConsumerState<ProviderEditSheet> createState() => _ProviderEditSheetState();
}

class _ProviderEditSheetState extends ConsumerState<ProviderEditSheet> {
  static const _kinds = ['ollama', 'openai', 'anthropic', 'gemini', 'openrouter'];

  late final _name =
      TextEditingController(text: widget.existing?.name ?? '');
  late final _baseUrl = TextEditingController(
      text: widget.existing?.baseUrl ?? 'http://host.docker.internal:11434');
  late final _model =
      TextEditingController(text: widget.existing?.model ?? '');

  late String _kind = widget.existing?.kind ?? 'ollama';
  late bool _vision = widget.existing?.vision ?? true;
  late bool _enabled = widget.existing?.enabled ?? true;

  List<String> _available = const [];
  bool _loadingModels = false;
  bool _busy = false;

  @override
  void initState() {
    super.initState();
    if (_kind == 'ollama') _loadModels();
  }

  @override
  void dispose() {
    _name.dispose();
    _baseUrl.dispose();
    _model.dispose();
    super.dispose();
  }

  /// Ask the engine what it can actually serve.
  Future<void> _loadModels() async {
    setState(() => _loadingModels = true);
    try {
      final list =
          await ref.read(apiProvider).ollamaModels(baseUrl: _baseUrl.text.trim());
      if (!mounted) return;
      setState(() {
        _available = list;
        _loadingModels = false;
      });
    } catch (_) {
      // A connection that is not up yet is normal while you are still typing
      // its address; the field stays free text so it can still be saved.
      if (!mounted) return;
      setState(() {
        _available = const [];
        _loadingModels = false;
      });
    }
  }

  Future<void> _save() async {
    if (_model.text.trim().isEmpty) {
      ScaffoldMessenger.of(context).showSnackBar(
        const SnackBar(content: Text('Pick a model first')),
      );
      return;
    }
    setState(() => _busy = true);
    final navigator = Navigator.of(context);
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(apiProvider).saveProvider(AIProvider(
            id: widget.existing?.id ?? '',
            name: _name.text.trim().isEmpty
                ? '$_kind · ${_model.text.trim()}'
                : _name.text.trim(),
            kind: _kind,
            model: _model.text.trim(),
            baseUrl: _baseUrl.text.trim(),
            vision: _vision,
            priority: widget.existing?.priority ?? 100,
            enabled: _enabled,
          ));
      navigator.pop(true);
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final isOllama = _kind == 'ollama';

    return Padding(
      padding: EdgeInsets.only(
        left: 16,
        right: 16,
        top: 16,
        bottom: MediaQuery.of(context).viewInsets.bottom + 16,
      ),
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              widget.existing == null ? 'Add AI connection' : 'Configure',
              style: const TextStyle(fontSize: 16, fontWeight: FontWeight.w700),
            ),
            const SizedBox(height: 14),
            DropdownButtonFormField<String>(
              initialValue: _kind,
              decoration: const InputDecoration(labelText: 'Engine'),
              dropdownColor: Fleet.ink850,
              items: [
                for (final k in _kinds)
                  DropdownMenuItem(value: k, child: Text(k)),
              ],
              onChanged: (v) {
                setState(() => _kind = v ?? 'ollama');
                if (_kind == 'ollama') _loadModels();
              },
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _name,
              decoration: const InputDecoration(
                labelText: 'Name (optional)',
                hintText: 'What you will call this in the list',
              ),
            ),
            const SizedBox(height: 12),
            TextField(
              controller: _baseUrl,
              decoration: InputDecoration(
                labelText: isOllama ? 'Address' : 'Address (optional)',
                helperText: isOllama
                    ? 'Where ollama is listening'
                    : 'Leave empty for the official endpoint',
              ),
              onEditingComplete: isOllama ? _loadModels : null,
            ),
            const SizedBox(height: 12),
            Row(
              children: [
                Expanded(
                  child: Text('MODEL',
                      style: TextStyle(
                          color: Fleet.ink400,
                          fontSize: 10,
                          letterSpacing: 0.6,
                          fontWeight: FontWeight.w700)),
                ),
                if (isOllama)
                  TextButton.icon(
                    onPressed: _loadingModels ? null : _loadModels,
                    icon: _loadingModels
                        ? const SizedBox(
                            width: 12,
                            height: 12,
                            child: CircularProgressIndicator(strokeWidth: 2))
                        : const Icon(Icons.refresh, size: 15),
                    label: const Text('Refresh', style: TextStyle(fontSize: 11)),
                  ),
              ],
            ),
            if (_available.isNotEmpty)
              Wrap(
                spacing: 6,
                runSpacing: 6,
                children: [
                  for (final m in _available)
                    ChoiceChip(
                      label: Text(m, style: const TextStyle(fontSize: 11)),
                      selected: _model.text.trim() == m,
                      onSelected: (_) => setState(() => _model.text = m),
                    ),
                ],
              ),
            const SizedBox(height: 8),
            TextField(
              controller: _model,
              decoration: InputDecoration(
                labelText: 'Model',
                hintText: isOllama ? 'e.g. qwen3.5:4b' : 'e.g. claude-opus-5',
                helperText: _available.isEmpty && isOllama
                    ? 'Could not reach that address to list models — you can '
                        'still type one'
                    : null,
              ),
              onChanged: (_) => setState(() {}),
            ),
            const SizedBox(height: 6),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              dense: true,
              value: _vision,
              onChanged: (v) => setState(() => _vision = v),
              title: const Text('Can see the screen',
                  style: TextStyle(fontSize: 13)),
              subtitle: Text(
                'Agents drive a desktop from screenshots. A model without '
                'vision is skipped for those turns.',
                style: TextStyle(color: Fleet.ink400, fontSize: 11),
              ),
            ),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              dense: true,
              value: _enabled,
              onChanged: (v) => setState(() => _enabled = v),
              title: const Text('Enabled', style: TextStyle(fontSize: 13)),
            ),
            const SizedBox(height: 14),
            SizedBox(
              width: double.infinity,
              child: FilledButton(
                onPressed: _busy ? null : _save,
                child: _busy
                    ? const SizedBox(
                        width: 16,
                        height: 16,
                        child: CircularProgressIndicator(strokeWidth: 2))
                    : const Text('Save'),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
