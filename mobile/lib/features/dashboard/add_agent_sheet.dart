import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/agent_kind_badge.dart';
import '../../core/widgets/inline_error.dart';
import 'external_agent_logic.dart';
import 'provision_sheet.dart';

/// Adding an agent: first what kind, then what that kind needs.
///
/// A desktop goes on to the provisioning sheet it always had. Every other
/// kind runs somewhere else -- a CLI on your PC, an OpenClaw gateway, any
/// webhook -- so it is a name, a place in the org chart and a connection,
/// and nothing is provisioned.
class AddAgentSheet extends ConsumerStatefulWidget {
  const AddAgentSheet({super.key});

  /// True when an agent was created.
  static Future<bool?> show(BuildContext context) async {
    final result = await showModalBottomSheet<Object>(
      context: context,
      isScrollControlled: true,
      backgroundColor: Fleet.ink900,
      shape: const RoundedRectangleBorder(
        borderRadius: BorderRadius.vertical(top: Radius.circular(18)),
      ),
      builder: (_) => const AddAgentSheet(),
    );
    if (result == AgentKind.desktop) {
      if (!context.mounted) return null;
      return ProvisionSheet.show(context);
    }
    return result == true;
  }

  @override
  ConsumerState<AddAgentSheet> createState() => _AddAgentSheetState();
}

class _AddAgentSheetState extends ConsumerState<AddAgentSheet> {
  String? _kind;

  final _name = TextEditingController();
  final _title = TextEditingController();
  final _sub = TextEditingController();
  final _model = TextEditingController();
  final _url = TextEditingController();
  final _token = TextEditingController();
  final _agentId = TextEditingController(text: 'main');
  String _reportsTo = '';
  String? _deviceId;
  String? _root;
  String _autonomy = 'edits';
  bool _busy = false;
  bool _showToken = false;
  String? _error;

  @override
  void dispose() {
    for (final c in [_name, _title, _sub, _model, _url, _token, _agentId]) {
      c.dispose();
    }
    super.dispose();
  }

  void _pick(String kind) {
    if (kind == AgentKind.desktop) {
      Navigator.pop(context, AgentKind.desktop);
      return;
    }
    setState(() {
      _kind = kind;
      _error = null;
      if (_name.text.trim().isEmpty) _name.text = AgentKind.label(kind);
    });
  }

  Future<void> _create(List<OafDevice> devices) async {
    final kind = _kind!;
    final name = _name.text.trim();
    if (name.isEmpty) {
      setState(() => _error = 'Give the agent a name.');
      return;
    }
    AgentConnection conn;
    if (AgentKind.onDevice(kind)) {
      final device = devices.where((d) => d.id == _deviceId).firstOrNull;
      if (device == null) {
        setState(() => _error = 'Pick the PC it runs on.');
        return;
      }
      final root = _root ?? (device.roots.isEmpty ? null : device.roots.first);
      if (root == null) {
        setState(() => _error =
            '${device.name} exposes no folders. Restart it with fleetctl host --root <folder>.');
        return;
      }
      final cwd = folderUnder(root, _sub.text);
      if (cwd == null || !underAnyRoot(cwd, device.roots)) {
        setState(() => _error = 'The folder has to be inside $root.');
        return;
      }
      conn = AgentConnection(
        deviceId: device.id,
        cwd: cwd,
        model: _model.text.trim(),
        autonomy: _autonomy,
      );
    } else {
      final problem = checkAgentUrl(kind, _url.text);
      if (problem != null) {
        setState(() => _error = problem);
        return;
      }
      conn = AgentConnection(
        url: _url.text.trim(),
        agentId: kind == AgentKind.openClaw ? _agentId.text.trim() : '',
      );
    }

    setState(() {
      _busy = true;
      _error = null;
    });
    try {
      await ref.read(apiProvider).createExternalInstance(
            kind: kind,
            name: name,
            title: _title.text.trim(),
            reportsTo: _reportsTo,
            connection: conn,
            token: _token.text.trim(),
          );
      ref.invalidate(instancesProvider);
      ref.invalidate(orgProvider);
      if (mounted) Navigator.pop(context, true);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final inset = MediaQuery.viewInsetsOf(context).bottom;
    return Padding(
      padding: EdgeInsets.fromLTRB(20, 14, 20, 18 + inset),
      child: SingleChildScrollView(
        child: AnimatedSize(
          duration: const Duration(milliseconds: 180),
          alignment: Alignment.topCenter,
          child: _kind == null ? _kindPicker() : _form(_kind!),
        ),
      ),
    );
  }

  Widget _kindPicker() {
    final chart = ref.watch(orgProvider).valueOrNull;
    final kinds = chart == null || chart.kinds.isEmpty
        ? AgentKind.all
        : [for (final k in chart.kinds) k.kind];
    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            const Icon(Icons.person_add_alt_outlined, size: 20),
            const SizedBox(width: 8),
            Text('Add an agent',
                style: Theme.of(context).textTheme.titleMedium),
          ],
        ),
        const SizedBox(height: 4),
        Text(
          'Every kind sits in the same org chart and takes the same tickets.',
          style: TextStyle(color: Fleet.ink400, fontSize: 12.5),
        ),
        const SizedBox(height: 12),
        for (final k in kinds)
          Padding(
            padding: const EdgeInsets.only(bottom: 8),
            child: Material(
              color: Fleet.ink850,
              shape: RoundedRectangleBorder(
                borderRadius: BorderRadius.circular(12),
                side: BorderSide(color: Fleet.ink800),
              ),
              child: InkWell(
                borderRadius: BorderRadius.circular(12),
                onTap: () => _pick(k),
                child: Padding(
                  padding: const EdgeInsets.all(12),
                  child: Row(
                    children: [
                      Container(
                        width: 38,
                        height: 38,
                        decoration: BoxDecoration(
                          color: agentKindColor(k).withValues(alpha: 0.14),
                          borderRadius: BorderRadius.circular(10),
                        ),
                        child: Icon(agentKindIcon(k),
                            color: agentKindColor(k), size: 20),
                      ),
                      const SizedBox(width: 12),
                      Expanded(
                        child: Column(
                          crossAxisAlignment: CrossAxisAlignment.start,
                          children: [
                            Text(AgentKind.label(k),
                                style: const TextStyle(
                                    fontSize: 14,
                                    fontWeight: FontWeight.w600)),
                            const SizedBox(height: 2),
                            Text(AgentKind.blurb(k),
                                style: TextStyle(
                                    color: Fleet.ink400, fontSize: 12)),
                          ],
                        ),
                      ),
                      Icon(Icons.chevron_right, color: Fleet.ink500),
                    ],
                  ),
                ),
              ),
            ),
          ),
      ],
    );
  }

  Widget _form(String kind) {
    final chart = ref.watch(orgProvider).valueOrNull;
    final managers = [...?chart?.nodes]
      ..sort((a, b) => a.name.toLowerCase().compareTo(b.name.toLowerCase()));
    final onDevice = AgentKind.onDevice(kind);
    final devicesAsync = onDevice ? ref.watch(oafDevicesProvider) : null;
    final devices = devicesAsync?.valueOrNull ?? const <OafDevice>[];

    return Column(
      mainAxisSize: MainAxisSize.min,
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Row(
          children: [
            IconButton(
              tooltip: 'Back',
              visualDensity: VisualDensity.compact,
              onPressed:
                  _busy ? null : () => setState(() => _kind = null),
              icon: const Icon(Icons.arrow_back),
            ),
            const SizedBox(width: 4),
            Expanded(
              child: Text('New ${AgentKind.label(kind)} agent',
                  style: Theme.of(context).textTheme.titleMedium),
            ),
            AgentKindBadge(kind),
          ],
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _name,
          enabled: !_busy,
          textCapitalization: TextCapitalization.words,
          decoration: const InputDecoration(labelText: 'Name'),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _title,
          enabled: !_busy,
          textCapitalization: TextCapitalization.words,
          decoration: const InputDecoration(
            labelText: 'Title (optional)',
            hintText: 'e.g. Engineer',
          ),
        ),
        const SizedBox(height: 12),
        DropdownButtonFormField<String>(
          key: ValueKey('mgr:${managers.length}'),
          initialValue:
              managers.any((m) => m.id == _reportsTo) ? _reportsTo : '',
          isExpanded: true,
          decoration: const InputDecoration(labelText: 'Reports to'),
          items: [
            const DropdownMenuItem(value: '', child: Text('You')),
            for (final m in managers)
              DropdownMenuItem(
                value: m.id,
                child: Text(m.title.isEmpty ? m.name : '${m.name} · ${m.title}',
                    overflow: TextOverflow.ellipsis),
              ),
          ],
          onChanged: _busy ? null : (v) => setState(() => _reportsTo = v ?? ''),
        ),
        const SizedBox(height: 16),
        if (onDevice)
          ..._deviceFields(kind, devicesAsync!, devices)
        else
          ..._networkFields(kind),
        InlineError(_error),
        const SizedBox(height: 16),
        FilledButton(
          onPressed: _busy ||
                  (onDevice && devicesFor(kind, devices).isEmpty)
              ? null
              : () => _create(devices),
          child: _busy
              ? const SizedBox(
                  width: 18,
                  height: 18,
                  child: CircularProgressIndicator(strokeWidth: 2))
              : const Text('Add agent'),
        ),
      ],
    );
  }

  List<Widget> _deviceFields(
      String kind, AsyncValue<List<OafDevice>> async, List<OafDevice> all) {
    if (async.isLoading && all.isEmpty) {
      return const [LinearProgressIndicator(minHeight: 2)];
    }
    if (async.hasError && all.isEmpty) {
      return [InlineError('Could not load your devices: ${async.error}')];
    }
    final pcs = all.where((d) => d.kind != 'phone').toList();
    if (pcs.isEmpty) return [_NoDevice(onRetry: _recheck)];

    final usable = devicesFor(kind, all);
    if (_deviceId == null || !usable.any((d) => d.id == _deviceId)) {
      final first = usable.where((d) => d.online).firstOrNull ??
          usable.firstOrNull;
      _deviceId = first?.id;
      _root = first == null || first.roots.isEmpty ? null : first.roots.first;
    }
    final device = usable.where((d) => d.id == _deviceId).firstOrNull;

    return [
      Row(
        children: [
          Text('RUNS ON',
              style: TextStyle(
                  color: Fleet.ink400,
                  fontSize: 10.5,
                  letterSpacing: 0.6,
                  fontWeight: FontWeight.w700)),
          const Spacer(),
          TextButton.icon(
            onPressed: _recheck,
            icon: const Icon(Icons.refresh, size: 16),
            label: const Text('Check again'),
          ),
        ],
      ),
      for (final d in pcs) _deviceTile(kind, d),
      if (usable.isEmpty)
        InlineError(
            'None of your PCs reported ${AgentKind.label(kind)}. Install it '
            'there, then restart fleetctl host so it is found.'),
      if (device != null) ...[
        const SizedBox(height: 12),
        if (device.roots.isEmpty)
          InlineError('${device.name} exposes no folders. Restart it with '
              'fleetctl host --root <folder>.')
        else ...[
          if (device.roots.length > 1) ...[
            DropdownButtonFormField<String>(
              key: ValueKey('root:${device.id}'),
              initialValue: device.roots.contains(_root) ? _root : device.roots.first,
              isExpanded: true,
              decoration: const InputDecoration(labelText: 'Folder'),
              items: [
                for (final r in device.roots)
                  DropdownMenuItem(
                    value: r,
                    child: Text(r,
                        overflow: TextOverflow.ellipsis,
                        style: const TextStyle(fontFamily: 'monospace')),
                  ),
              ],
              onChanged: _busy ? null : (v) => setState(() => _root = v),
            ),
            const SizedBox(height: 12),
          ],
          TextField(
            controller: _sub,
            enabled: !_busy,
            autocorrect: false,
            style: const TextStyle(fontFamily: 'monospace', fontSize: 13),
            decoration: InputDecoration(
              labelText: device.roots.length > 1
                  ? 'Subfolder (optional)'
                  : 'Folder inside ${device.roots.first}',
              hintText: 'my-app',
              helperText: 'Where it works: '
                  '${folderUnder(_root ?? device.roots.first, _sub.text) ?? '(not inside the folder)'}',
              helperMaxLines: 2,
            ),
            onChanged: (_) => setState(() {}),
          ),
        ],
      ],
      const SizedBox(height: 12),
      TextField(
        controller: _model,
        enabled: !_busy,
        autocorrect: false,
        decoration: InputDecoration(
          labelText: 'Model (optional)',
          hintText: switch (kind) {
            AgentKind.claudeCode => 'sonnet, opus',
            AgentKind.codex => 'gpt-5-codex',
            _ => 'the CLI default',
          },
          helperText: 'Blank uses whatever the CLI is set to.',
        ),
      ),
      const SizedBox(height: 14),
      Text('Autonomy',
          style: TextStyle(
              color: Fleet.ink300, fontSize: 12, fontWeight: FontWeight.w600)),
      const SizedBox(height: 8),
      SegmentedButton<String>(
        segments: const [
          ButtonSegment(
              value: 'edits',
              label: Text('Edits'),
              icon: Icon(Icons.edit_note_rounded)),
          ButtonSegment(
              value: 'full',
              label: Text('Full'),
              icon: Icon(Icons.bolt_rounded)),
        ],
        selected: {_autonomy},
        onSelectionChanged:
            _busy ? null : (s) => setState(() => _autonomy = s.first),
      ),
      const SizedBox(height: 6),
      Text(
        _autonomy == 'full'
            ? 'It can do anything inside its folder, commands included.'
            : 'It can change files in its folder but not run commands.',
        style: TextStyle(color: Fleet.ink400, fontSize: 11.5),
      ),
      const SizedBox(height: 4),
      Text(
        'Each run asks in the host\'s terminal before it starts, unless the '
        'host was started with --yes.',
        style: TextStyle(color: Fleet.ink500, fontSize: 11.5),
      ),
    ];
  }

  void _recheck() => ref.invalidate(oafDevicesProvider);

  Widget _deviceTile(String kind, OafDevice d) {
    final ok = d.canRun(kind);
    final selected = d.id == _deviceId;
    return Padding(
      padding: const EdgeInsets.only(bottom: 8),
      child: Material(
        color: selected ? Fleet.live.withValues(alpha: 0.08) : Fleet.ink850,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(12),
          side: BorderSide(
              color: selected ? Fleet.live : Fleet.ink800,
              width: selected ? 1.5 : 1),
        ),
        child: InkWell(
          borderRadius: BorderRadius.circular(12),
          onTap: !ok || _busy
              ? null
              : () => setState(() {
                    _deviceId = d.id;
                    _root = d.roots.isEmpty ? null : d.roots.first;
                  }),
          child: Padding(
            padding: const EdgeInsets.all(12),
            child: Row(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Icon(
                  selected
                      ? Icons.radio_button_checked
                      : Icons.radio_button_unchecked,
                  size: 18,
                  color: ok ? (selected ? Fleet.live : Fleet.ink400) : Fleet.ink600,
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: Column(
                    crossAxisAlignment: CrossAxisAlignment.start,
                    children: [
                      Row(
                        children: [
                          Flexible(
                            child: Text(d.name.isEmpty ? 'PC' : d.name,
                                overflow: TextOverflow.ellipsis,
                                style: TextStyle(
                                    color: ok ? Fleet.ink100 : Fleet.ink400,
                                    fontWeight: FontWeight.w600)),
                          ),
                          const SizedBox(width: 8),
                          Container(
                            width: 7,
                            height: 7,
                            decoration: BoxDecoration(
                              color: d.online ? Fleet.good : Fleet.ink500,
                              shape: BoxShape.circle,
                            ),
                          ),
                          const SizedBox(width: 4),
                          Text(d.online ? 'online' : 'offline',
                              style: TextStyle(
                                  color: Fleet.ink400, fontSize: 11)),
                        ],
                      ),
                      const SizedBox(height: 4),
                      Text(
                        [
                          if (d.platform.isNotEmpty) d.platform,
                          d.roots.isEmpty
                              ? 'no folders'
                              : d.roots.length == 1
                                  ? d.roots.first
                                  : '${d.roots.length} folders',
                        ].join(' · '),
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(color: Fleet.ink400, fontSize: 11.5),
                      ),
                      const SizedBox(height: 6),
                      Wrap(
                        spacing: 6,
                        runSpacing: 4,
                        children: d.runtimes.isEmpty
                            ? [
                                Text('Did not say which CLIs it has',
                                    style: TextStyle(
                                        color: Fleet.ink500, fontSize: 11)),
                              ]
                            : [
                                for (final r in d.runtimes)
                                  AgentKindBadge(r, dense: true),
                              ],
                      ),
                      if (!ok) ...[
                        const SizedBox(height: 4),
                        Text(
                            '${AgentKind.label(kind)} is not installed here.',
                            style: TextStyle(
                                color: Fleet.warn, fontSize: 11.5)),
                      ],
                    ],
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
    );
  }

  List<Widget> _networkFields(String kind) {
    final openClaw = kind == AgentKind.openClaw;
    return [
      TextField(
        controller: _url,
        enabled: !_busy,
        autocorrect: false,
        keyboardType: TextInputType.url,
        decoration: InputDecoration(
          labelText: openClaw ? 'Gateway address' : 'Webhook URL',
          hintText: openClaw
              ? 'wss://gateway.example:18789'
              : 'https://example.com/agent',
        ),
      ),
      const SizedBox(height: 12),
      TextField(
        controller: _token,
        enabled: !_busy,
        autocorrect: false,
        obscureText: !_showToken,
        enableSuggestions: false,
        decoration: InputDecoration(
          labelText: openClaw ? 'Gateway token' : 'Token (optional)',
          helperText: openClaw
              ? 'Needs operator.admin the first time, so the agent can approve '
                  'its own pairing. Kept in the vault.'
              : 'Sent with each run so your service knows it is the fleet. '
                  'Kept in the vault.',
          helperMaxLines: 3,
          suffixIcon: IconButton(
            tooltip: _showToken ? 'Hide' : 'Show',
            icon: Icon(_showToken
                ? Icons.visibility_off_outlined
                : Icons.visibility_outlined),
            onPressed: () => setState(() => _showToken = !_showToken),
          ),
        ),
      ),
      if (openClaw) ...[
        const SizedBox(height: 12),
        TextField(
          controller: _agentId,
          enabled: !_busy,
          autocorrect: false,
          decoration: const InputDecoration(
            labelText: 'Agent id',
            helperText: 'The agent inside OpenClaw. Usually main.',
          ),
        ),
      ] else ...[
        const SizedBox(height: 10),
        Text(
          'Each run is a POST with the brief and a callback. Answer 200 with '
          '{"status":"done","result":"…"} to finish at once, or 202 and call '
          'back later.',
          style: TextStyle(color: Fleet.ink400, fontSize: 11.5, height: 1.4),
        ),
      ],
    ];
  }
}

/// No PC has connected yet: say how to attach one.
class _NoDevice extends StatefulWidget {
  const _NoDevice({required this.onRetry});
  final VoidCallback onRetry;

  @override
  State<_NoDevice> createState() => _NoDeviceState();
}

class _NoDeviceState extends State<_NoDevice> {
  /// A SnackBar would land behind this sheet, so the button says it instead.
  bool _copied = false;

  @override
  Widget build(BuildContext context) {
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: Fleet.ink850,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: Fleet.ink800),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Row(
            children: [
              Icon(Icons.computer_outlined, size: 18, color: Fleet.ink300),
              const SizedBox(width: 8),
              const Expanded(
                child: Text('No PC is connected yet',
                    style: TextStyle(fontWeight: FontWeight.w600)),
              ),
            ],
          ),
          const SizedBox(height: 6),
          Text(
            'This kind runs on your PC. On it, install the fleet tools and '
            'start the host in the folder the agent should work in:',
            style: TextStyle(color: Fleet.ink300, fontSize: 12.5, height: 1.4),
          ),
          const SizedBox(height: 10),
          Container(
            width: double.infinity,
            padding: const EdgeInsets.fromLTRB(12, 10, 4, 10),
            decoration: BoxDecoration(
              color: Fleet.ink950,
              borderRadius: BorderRadius.circular(8),
              border: Border.all(color: Fleet.ink700),
            ),
            child: Row(
              children: [
                Expanded(
                  child: SelectableText(
                    fleetctlHostCommands,
                    style: TextStyle(
                        fontFamily: 'monospace',
                        fontSize: 12.5,
                        height: 1.5,
                        color: Fleet.ink100),
                  ),
                ),
                IconButton(
                  tooltip: _copied ? 'Copied' : 'Copy',
                  icon: Icon(
                      _copied ? Icons.check_rounded : Icons.copy_rounded,
                      size: 18,
                      color: _copied ? Fleet.good : null),
                  onPressed: () async {
                    await Clipboard.setData(
                        const ClipboardData(text: fleetctlHostCommands));
                    if (!mounted) return;
                    setState(() => _copied = true);
                    await Future<void>.delayed(const Duration(seconds: 2));
                    if (mounted) setState(() => _copied = false);
                  },
                ),
              ],
            ),
          ),
          const SizedBox(height: 8),
          Text(
            'It reports which CLIs it finds. Once it says it is connected, '
            'check again here.',
            style: TextStyle(color: Fleet.ink400, fontSize: 12),
          ),
          const SizedBox(height: 4),
          Align(
            alignment: Alignment.centerRight,
            child: TextButton.icon(
              onPressed: widget.onRetry,
              icon: const Icon(Icons.refresh, size: 16),
              label: const Text('Check again'),
            ),
          ),
        ],
      ),
    );
  }
}
