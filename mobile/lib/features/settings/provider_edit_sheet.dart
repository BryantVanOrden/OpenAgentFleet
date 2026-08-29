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
  static const _kinds = [
    'ollama',
    'openai',
    'anthropic',
    'gemini',
    'antigravity',
    'anthropic-vertex',
    'openai-compatible',
  ];

  /// Engines that authenticate with a key. Ollama on your own machine does
  /// not, which is why the field is hidden rather than left blank there.
  static const _needsKey = {
    'openai',
    'anthropic',
    'gemini',
    'antigravity',
    'openai-compatible',
  };

  /// Engines that can be signed into with an account instead. Offered only
  /// where it actually works, so the choice is never a dead end.
  static const _canSignIn = {'gemini', 'antigravity', 'anthropic-vertex'};

  /// Vertex serves Claude through Google, so it is the one way to run Claude
  /// on an account sign-in rather than a pasted key.
  static const _vertexKind = 'anthropic-vertex';

  late final _name =
      TextEditingController(text: widget.existing?.name ?? '');
  late final _baseUrl = TextEditingController(
      text: widget.existing?.baseUrl ?? 'http://host.docker.internal:11434');
  late final _model =
      TextEditingController(text: widget.existing?.model ?? '');

  final _apiKey = TextEditingController();

  late String _kind = widget.existing?.kind ?? 'ollama';
  /// True once you choose to replace a key that is already stored. Until then
  /// the field stays closed, because the stored key cannot be read back and an
  /// empty box would look like "no key set".
  bool _replacingKey = false;
  late String _authMode = widget.existing?.authMode ?? 'api_key';
  late bool _vision = widget.existing?.vision ?? true;
  late bool _enabled = widget.existing?.enabled ?? true;

  List<String> _available = const [];
  bool _loadingModels = false;
  bool _busy = false;

  /// False when the list came from the server's built-in catalogue rather than
  /// from the engine itself.
  bool _liveModels = true;
  String _modelsReason = '';

  @override
  void initState() {
    super.initState();
    _loadModels();
  }

  @override
  void dispose() {
    _name.dispose();
    _baseUrl.dispose();
    _model.dispose();
    _apiKey.dispose();
    super.dispose();
  }

  /// Ask the engine what it can actually serve.
  ///
  /// The typed key is sent along so a cloud engine can be queried before the
  /// connection is saved — otherwise you would have to save a blind guess at a
  /// model name first, then come back and fix it.
  Future<void> _loadModels() async {
    setState(() => _loadingModels = true);
    try {
      final res = await ref.read(apiProvider).discoverModels(
            kind: _kind,
            baseUrl: _baseUrl.text.trim(),
            apiKey: _apiKey.text.trim(),
          );
      if (!mounted) return;
      setState(() {
        _available = res.models;
        _liveModels = res.live;
        _modelsReason = res.reason;
        _loadingModels = false;
      });
    } catch (_) {
      // A connection that is not up yet is normal while you are still typing
      // its address; the field stays free text so it can still be saved.
      if (!mounted) return;
      setState(() {
        _available = const [];
        _liveModels = false;
        _modelsReason = '';
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
    if (_authMode == 'api_key' &&
        _needsKey.contains(_kind) &&
        _apiKey.text.trim().isEmpty &&
        !(widget.existing?.hasKey ?? false)) {
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('$_kind needs an API key')),
      );
      return;
    }
    setState(() => _busy = true);
    final navigator = Navigator.of(context);
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref.read(apiProvider).saveProvider(
          apiKey: _apiKey.text.trim(),
          AIProvider(
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
            apiKeyRef: widget.existing?.apiKeyRef ?? '',
            authMode: _authMode,
            oauthClientId: widget.existing?.oauthClientId ?? '',
            signedIn: widget.existing?.signedIn ?? false,
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
                final next = v ?? 'ollama';
                setState(() {
                  // The ollama default address is meaningless for a cloud
                  // engine, and a wrong base URL fails in a way that looks
                  // like a bad key.
                  if (next != 'ollama' && _kind == 'ollama') {
                    _baseUrl.text = '';
                  } else if (next == 'ollama' && _baseUrl.text.isEmpty) {
                    _baseUrl.text = 'http://host.docker.internal:11434';
                  }
                  // Running Claude on a Google sign-in is the only reason to
                  // pick Vertex over the direct API, so start there.
                  if (next == _vertexKind) _authMode = 'oauth';
                  _kind = next;
                });
                _loadModels();
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
                labelText: isOllama || _kind == _vertexKind
                    ? 'Address'
                    : 'Address (optional)',
                helperText: isOllama
                    ? 'Where ollama is listening'
                    : _kind == _vertexKind
                        ? 'https://REGION-aiplatform.googleapis.com/v1/'
                            'projects/YOUR_PROJECT/locations/REGION'
                        : 'Leave empty for the official endpoint',
                helperMaxLines: 2,
              ),
              onEditingComplete: _loadModels,
            ),
            if (_canSignIn.contains(_kind)) ...[
              const SizedBox(height: 14),
              Text('HOW TO AUTHENTICATE',
                  style: TextStyle(
                      color: Fleet.ink400,
                      fontSize: 10,
                      letterSpacing: 0.6,
                      fontWeight: FontWeight.w700)),
              const SizedBox(height: 6),
              SegmentedButton<String>(
                segments: const [
                  ButtonSegment(value: 'api_key', label: Text('API key')),
                  ButtonSegment(value: 'oauth', label: Text('Sign in')),
                ],
                selected: {_authMode},
                onSelectionChanged: (v) =>
                    setState(() => _authMode = v.first),
                showSelectedIcon: false,
              ),
              if (_authMode == 'oauth') ...[
                const SizedBox(height: 12),
                if (widget.existing == null)
                  Text(
                    'Save the connection first, then sign in to it from the '
                    'list. Signing in needs somewhere to store the result.',
                    style: TextStyle(
                        color: Fleet.ink400, fontSize: 11.5, height: 1.45),
                  )
                else if (widget.existing!.signedIn)
                  Row(
                    children: [
                      Icon(Icons.verified_user_outlined,
                          size: 15, color: Fleet.good),
                      const SizedBox(width: 8),
                      Expanded(
                        child: Text('Signed in with a Google account.',
                            style: TextStyle(
                                color: Fleet.ink300, fontSize: 12)),
                      ),
                    ],
                  )
                else
                  Text(
                    'Not signed in yet — use "Sign in" from the connection\'s '
                    'menu in the list.',
                    style: TextStyle(color: Fleet.warn, fontSize: 11.5),
                  ),
              ],
            ],
            if (_needsKey.contains(_kind) && _authMode == 'api_key') ...[
              const SizedBox(height: 12),
              if ((widget.existing?.hasKey ?? false) && !_replacingKey)
                Row(
                  children: [
                    Icon(Icons.lock_outline, size: 15, color: Fleet.good),
                    const SizedBox(width: 8),
                    Expanded(
                      child: Text(
                        'A key is stored for this connection.',
                        style: TextStyle(color: Fleet.ink300, fontSize: 12),
                      ),
                    ),
                    TextButton(
                      onPressed: () => setState(() => _replacingKey = true),
                      child: const Text('Replace', style: TextStyle(fontSize: 12)),
                    ),
                  ],
                )
              else
                TextField(
                  controller: _apiKey,
                  obscureText: true,
                  autocorrect: false,
                  enableSuggestions: false,
                  decoration: InputDecoration(
                    labelText: 'API key',
                    hintText: _kind == 'antigravity' || _kind == 'gemini'
                        ? 'Google AI Studio key'
                        : 'Paste the key',
                    helperText: 'Sealed into the server vault. It is never '
                        'stored on this device and never read back.',
                    helperMaxLines: 2,
                  ),
                ),
            ],
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
                hintText: isOllama
                    ? 'e.g. qwen3.5:4b'
                    : _kind == _vertexKind
                        ? 'e.g. claude-opus-4-5@20251101'
                        : 'e.g. claude-opus-5',
                helperText: !_liveModels
                    ? (_modelsReason.isEmpty
                        ? 'Could not reach the engine to list models — you can '
                            'still type one'
                        : _modelsReason)
                    : null,
                helperMaxLines: 2,
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
