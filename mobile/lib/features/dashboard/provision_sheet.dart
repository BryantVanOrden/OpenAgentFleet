import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';

/// Provision a new agent from the phone.
///
/// The settings screen used to say provisioning belonged in the web console
/// because "a phone is the wrong place" for administration. That holds for
/// editing model configuration or access control; it does not hold for
/// starting an agent, which is the thing you most want to do when you are away
/// from the desk and reading an alert.
///
/// Tiers and archetypes are fetched, never hardcoded: the orchestrator rejects
/// an unknown tier rather than quietly giving you a smaller box, so a stale
/// local list would fail at the worst moment.
class ProvisionSheet extends ConsumerStatefulWidget {
  const ProvisionSheet({super.key});

  static Future<bool?> show(BuildContext context) => showModalBottomSheet<bool>(
        context: context,
        isScrollControlled: true,
        backgroundColor: Fleet.ink900,
        shape: const RoundedRectangleBorder(
          borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
        ),
        builder: (_) => const ProvisionSheet(),
      );

  @override
  ConsumerState<ProvisionSheet> createState() => _ProvisionSheetState();
}

class _ProvisionSheetState extends ConsumerState<ProvisionSheet> {
  final _name = TextEditingController();
  String? _tier;
  String? _archetype;

  /// The archetype's tools, with the ones to actually install ticked. Rebuilt
  /// whenever the archetype changes, since it belongs to that choice.
  final Set<String> _tools = {};
  List<String> _offered = const [];

  /// Tools the operator added by hand.
  final List<CustomTool> _custom = [];

  /// The bot's personality, prefilled from the archetype's own — the one
  /// written for that job — and editable before the bot is built. Left alone
  /// it ships the recommended personality; nothing read it at all before, so
  /// every bot started life with no idea what it was for.
  final _persona = TextEditingController();
  bool _personaEdited = false;
  bool _shell = false;
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    _persona.dispose();
    super.dispose();
  }

  /// The personality this bot will be built with.
  ///
  /// Prefilled from the archetype — each one carries a personality written for
  /// its job — and editable here, so a bot can be given its own character
  /// before it ever runs rather than after.
  Widget _personaField() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            Icon(Icons.psychology_outlined, size: 16, color: Fleet.ink400),
            const SizedBox(width: 6),
            Text('Personality',
                style: TextStyle(
                    color: Fleet.ink300,
                    fontSize: 12,
                    fontWeight: FontWeight.w600)),
            const Spacer(),
            if (_personaEdited)
              TextButton(
                onPressed: () {
                  final list = ref.read(templatesProvider).valueOrNull ??
                      const <BotTemplate>[];
                  final t =
                      list.where((e) => e.id == _archetype).firstOrNull;
                  setState(() {
                    _persona.text = t?.specializedPrompt ?? '';
                    _personaEdited = false;
                  });
                },
                child: const Text('Reset'),
              ),
          ],
        ),
        const SizedBox(height: 6),
        TextField(
          controller: _persona,
          enabled: !_busy,
          maxLines: 5,
          minLines: 3,
          style: const TextStyle(fontSize: 12, height: 1.4),
          onChanged: (_) {
            if (!_personaEdited) setState(() => _personaEdited = true);
          },
          decoration: InputDecoration(
            filled: true,
            fillColor: Fleet.ink850,
            isDense: true,
            hintText: _archetype == null
                ? 'Pick a role above and its recommended personality appears here.'
                : 'How this bot thinks and talks.',
            hintStyle:
                TextStyle(color: Fleet.ink500, fontSize: 11, height: 1.4),
            border: OutlineInputBorder(
              borderRadius: BorderRadius.circular(10),
              borderSide: BorderSide.none,
            ),
          ),
        ),
      ],
    );
  }

  Future<void> _create() async {
    final name = _name.text.trim();
    final tier = _tier;
    if (name.isEmpty || tier == null) return;

    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(apiProvider).createInstance(
            name: name,
            tier: tier,
            archetypeId: _archetype,
            // Only sent when the archetype's list was actually changed —
            // otherwise the server applies the template's own list.
            tools: _offered.isEmpty || _tools.length == _offered.length
                ? null
                : _tools.toList(),
            customTools: _custom,
            shellAccess: _shell,
            systemPrompt: _persona.text.trim(),
          );
      if (mounted) Navigator.pop(context, true);
    } catch (err) {
      // Provisioning can fail for reasons worth reading — the host at
      // capacity, an image still pulling — so the message stays on the sheet
      // rather than vanishing with it.
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  /// Tools the archetype does not know about.
  ///
  /// Asks for the install method explicitly rather than guessing from the
  /// string: "pandas" is a pip package and "pandoc" is an apt one, and getting
  /// that wrong is a bot that silently lacks the tool.
  Widget _customToolsSection() {
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Row(
          children: [
            Expanded(
              child: Text('YOUR OWN TOOLS',
                  style: TextStyle(
                      color: Fleet.ink400,
                      fontSize: 10,
                      letterSpacing: 0.6,
                      fontWeight: FontWeight.w700)),
            ),
            TextButton.icon(
              onPressed: _addCustomTool,
              icon: const Icon(Icons.add, size: 15),
              label: const Text('Add', style: TextStyle(fontSize: 11)),
            ),
          ],
        ),
        if (_custom.isEmpty)
          Text('An apt package, a pip or npm package, a Go module, a binary '
              'URL, or a GitHub repository to clone.',
              style: TextStyle(
                  color: Fleet.ink500, fontSize: 10.5, height: 1.35))
        else
          Wrap(
            spacing: 6,
            runSpacing: 6,
            children: [
              for (final c in _custom)
                InputChip(
                  label: Text('${c.name} · ${c.method}',
                      style: const TextStyle(fontSize: 11)),
                  onDeleted: () => setState(() => _custom.remove(c)),
                ),
            ],
          ),
      ],
    );
  }

  Future<void> _addCustomTool() async {
    final name = TextEditingController();
    final spec = TextEditingController();
    String method = 'apt';

    final ok = await showDialog<bool>(
      context: context,
      builder: (ctx) => StatefulBuilder(
        builder: (ctx, setLocal) => AlertDialog(
          title: const Text('Add a tool'),
          content: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              TextField(
                controller: name,
                autofocus: true,
                decoration: const InputDecoration(
                  labelText: 'Command name',
                  hintText: 'what you type to run it',
                ),
              ),
              const SizedBox(height: 10),
              DropdownButtonFormField<String>(
                initialValue: method,
                isExpanded: true,
                decoration: const InputDecoration(labelText: 'Install with'),
                dropdownColor: Fleet.ink850,
                items: [
                  for (final m in CustomTool.methods)
                    DropdownMenuItem(value: m, child: Text(m)),
                ],
                onChanged: (v) => setLocal(() => method = v ?? 'apt'),
              ),
              const SizedBox(height: 10),
              TextField(
                controller: spec,
                decoration: InputDecoration(
                  labelText: 'Where from',
                  helperText: CustomTool.methodHints[method],
                  helperMaxLines: 2,
                ),
              ),
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

    if (ok != true) return;
    final n = name.text.trim();
    final sp = spec.text.trim();
    if (n.isEmpty || sp.isEmpty) return;
    setState(() => _custom.add(CustomTool(name: n, method: method, spec: sp)));
  }

  @override
  Widget build(BuildContext context) {
    final tiers = ref.watch(tiersProvider);
    final templates = ref.watch(templatesProvider);
    final inset = MediaQuery.of(context).viewInsets.bottom;

    return Padding(
      padding: EdgeInsets.fromLTRB(20, 18, 20, 18 + inset),
      child: SingleChildScrollView(
        child: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.stretch,
          children: [
            Row(
              children: [
                const Icon(Icons.add_circle_outline, size: 20),
                const SizedBox(width: 8),
                Text('New agent',
                    style: Theme.of(context).textTheme.titleMedium),
              ],
            ),
            const SizedBox(height: 16),
            TextField(
              controller: _name,
              autofocus: true,
              textCapitalization: TextCapitalization.words,
              decoration: const InputDecoration(
                labelText: 'Name',
                hintText: 'invoice-runner',
              ),
              onChanged: (_) => setState(() {}),
            ),
            const SizedBox(height: 14),
            tiers.when(
              loading: () => const LinearProgressIndicator(minHeight: 2),
              error: (e, _) => _Problem('Could not load tiers: $e'),
              data: (list) {
                // Default to the server's own default rather than the first
                // row, so the sheet opens on the sensible choice.
                _tier ??= list.any((t) => t.name == 'standard')
                    ? 'standard'
                    : (list.isEmpty ? null : list.first.name);
                return DropdownButtonFormField<String>(
                  initialValue: _tier,
                  decoration: const InputDecoration(labelText: 'Size'),
                  items: [
                    for (final t in list)
                      DropdownMenuItem(
                        value: t.name,
                        child: Text('${t.name}  ·  '
                            '${t.vcpu.toStringAsFixed(0)} vCPU, '
                            '${(t.memoryMb / 1024).toStringAsFixed(0)} GB'
                            '${t.gpu ? ', GPU' : ''}'),
                      ),
                  ],
                  onChanged: (v) => setState(() => _tier = v),
                );
              },
            ),
            const SizedBox(height: 14),
            templates.when(
              loading: () => const SizedBox.shrink(),
              error: (_, __) => const SizedBox.shrink(),
              data: (list) => list.isEmpty
                  ? const SizedBox.shrink()
                  : DropdownButtonFormField<String>(
                      initialValue: _archetype,
                      isExpanded: true,
                      decoration:
                          const InputDecoration(labelText: 'Start from'),
                      items: [
                        const DropdownMenuItem(
                            value: null, child: Text('Blank agent')),
                        for (final t in list)
                          DropdownMenuItem(
                            value: t.id,
                            child: Text(t.name, overflow: TextOverflow.ellipsis),
                          ),
                      ],
                      onChanged: (v) => setState(() {
                        _archetype = v;
                        // The tool list belongs to the archetype, so picking a
                        // different one starts from its list rather than
                        // carrying the last one's ticks across.
                        final t = list.where((e) => e.id == v).firstOrNull;
                        _offered = t?.tools ?? const [];
                        _tools
                          ..clear()
                          ..addAll(_offered);
                        if (t != null && t.defaultShellAccess) _shell = true;
                        // Prefill the personality the same way, unless the
                        // operator has already written their own — changing
                        // archetype should not silently discard what they
                        // typed.
                        if (!_personaEdited) {
                          _persona.text = t?.specializedPrompt ?? '';
                        }
                      }),
                    ),
            ),
            if (_offered.isNotEmpty) ...[
              const SizedBox(height: 12),
              Row(
                children: [
                  Expanded(
                    child: Text('TOOLS',
                        style: TextStyle(
                            color: Fleet.ink400,
                            fontSize: 10,
                            letterSpacing: 0.6,
                            fontWeight: FontWeight.w700)),
                  ),
                  TextButton(
                    onPressed: () => setState(() => _tools
                      ..clear()
                      ..addAll(_offered)),
                    child: const Text('All', style: TextStyle(fontSize: 11)),
                  ),
                  TextButton(
                    onPressed: () => setState(_tools.clear),
                    child: const Text('None', style: TextStyle(fontSize: 11)),
                  ),
                ],
              ),
              Text(
                'Anything that cannot be installed is dropped after '
                'provisioning, so the agent is never told it has a tool it '
                'does not.',
                style:
                    TextStyle(color: Fleet.ink500, fontSize: 10.5, height: 1.35),
              ),
              const SizedBox(height: 6),
              Wrap(
                spacing: 6,
                runSpacing: 6,
                children: [
                  for (final t in _offered)
                    FilterChip(
                      label: Text(t, style: const TextStyle(fontSize: 11)),
                      selected: _tools.contains(t),
                      onSelected: (on) => setState(() {
                        if (on) {
                          _tools.add(t);
                        } else {
                          _tools.remove(t);
                        }
                      }),
                    ),
                ],
              ),
            ],
            const SizedBox(height: 10),
            _customToolsSection(),
            const SizedBox(height: 12),
            _personaField(),
            const SizedBox(height: 6),
            SwitchListTile(
              contentPadding: EdgeInsets.zero,
              value: _shell,
              onChanged: (v) => setState(() => _shell = v),
              title: const Text('Allow shell access'),
              subtitle: Text(
                'Lets this agent run commands directly, not just drive the GUI.',
                style: TextStyle(color: Fleet.ink300, fontSize: 12),
              ),
            ),
            if (_error != null) ...[
              const SizedBox(height: 8),
              _Problem(_error!),
            ],
            const SizedBox(height: 14),
            FilledButton.icon(
              onPressed: _busy || _name.text.trim().isEmpty || _tier == null
                  ? null
                  : _create,
              icon: _busy
                  ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2),
                    )
                  : const Icon(Icons.play_arrow_rounded),
              label: Text(_busy ? 'Provisioning...' : 'Create agent'),
            ),
            const SizedBox(height: 6),
            Text(
              'Provisioning pulls an image and waits for the desktop to answer, '
              'so this can take a minute.',
              textAlign: TextAlign.center,
              style: TextStyle(color: Fleet.ink300, fontSize: 12),
            ),
          ],
        ),
      ),
    );
  }
}

class _Problem extends StatelessWidget {
  const _Problem(this.message);
  final String message;

  @override
  Widget build(BuildContext context) => Container(
        padding: const EdgeInsets.all(10),
        decoration: BoxDecoration(
          color: Fleet.bad.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(8),
        ),
        child: Row(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Icon(Icons.error_outline, size: 16, color: Fleet.bad),
            const SizedBox(width: 8),
            Expanded(
              child: Text(message,
                  style: TextStyle(color: Fleet.bad, fontSize: 12)),
            ),
          ],
        ),
      );
}
