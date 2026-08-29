import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

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
  bool _shell = false;
  bool _busy = false;
  String? _error;

  @override
  void dispose() {
    _name.dispose();
    super.dispose();
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
            shellAccess: _shell,
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
                      onChanged: (v) => setState(() => _archetype = v),
                    ),
            ),
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
