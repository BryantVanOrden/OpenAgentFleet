import 'dart:convert';

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:webview_flutter/webview_flutter.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../agent_chat/chat_screen.dart';

/// One machine, three views: what it looks like, what it is doing, and talking
/// to it. Everything an operator needs while standing somewhere else.
class InstanceScreen extends ConsumerStatefulWidget {
  const InstanceScreen({super.key, required this.instanceId});

  final String instanceId;

  @override
  ConsumerState<InstanceScreen> createState() => _InstanceScreenState();
}

class _InstanceScreenState extends ConsumerState<InstanceScreen>
    with SingleTickerProviderStateMixin {
  late final TabController _tabs = TabController(length: 3, vsync: this);

  @override
  void dispose() {
    _tabs.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final instances = ref.watch(instancesProvider).valueOrNull ?? const <Instance>[];
    final instance = instances.where((i) => i.id == widget.instanceId).firstOrNull;

    if (instance == null) {
      return const Scaffold(body: Center(child: CircularProgressIndicator()));
    }

    return Scaffold(
      appBar: AppBar(
        title: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(instance.name, overflow: TextOverflow.ellipsis),
            Text(
              '${instance.tier} · ${instance.shellAccess ? "shell on" : "shell off"}',
              style: TextStyle(fontSize: 11, color: Fleet.ink400),
            ),
          ],
        ),
        actions: [
          Padding(
            padding: const EdgeInsets.only(right: 12),
            child: Center(child: StateChip(state: instance.state, live: instance.isRunning)),
          ),
          _ControlMenu(instance: instance),
        ],
        bottom: TabBar(
          controller: _tabs,
          indicatorColor: Fleet.live,
          labelColor: Fleet.ink100,
          unselectedLabelColor: Fleet.ink400,
          tabs: const [
            Tab(text: 'Desktop'),
            Tab(text: 'Activity'),
            Tab(text: 'Chat'),
          ],
        ),
      ),
      body: TabBarView(
        controller: _tabs,
        children: [
          _DesktopTab(instance: instance),
          _ActivityTab(instanceId: instance.id),
          ChatScreen(instanceId: instance.id, enabled: instance.isRunning),
        ],
      ),
    );
  }
}

// ---------------------------------------------------------------- controls ---

class _ControlMenu extends ConsumerWidget {
  const _ControlMenu({required this.instance});
  final Instance instance;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    Future<void> act(String action) async {
      final api = ref.read(apiProvider);
      final messenger = ScaffoldMessenger.of(context);
      try {
        await api.instanceAction(instance.id, action);
        ref.invalidate(instancesProvider);
      } catch (err) {
        messenger.showSnackBar(SnackBar(content: Text('$err')));
      }
    }

    return PopupMenuButton<String>(
      onSelected: act,
      color: Fleet.ink850,
      itemBuilder: (_) => [
        if (instance.isRunning) ...[
          const PopupMenuItem(
            value: 'pause',
            child: ListTile(
              dense: true,
              leading: Icon(Icons.pause_circle_outline),
              title: Text('Freeze'),
              subtitle: Text('Stops every process instantly'),
            ),
          ),
          const PopupMenuItem(
            value: 'stop',
            child: ListTile(
              dense: true,
              leading: Icon(Icons.stop_circle_outlined),
              title: Text('Shut down'),
            ),
          ),
        ] else if (instance.state == 'paused')
          const PopupMenuItem(
            value: 'resume',
            child: ListTile(dense: true, leading: Icon(Icons.play_arrow), title: Text('Resume')),
          )
        else
          const PopupMenuItem(
            value: 'start',
            child: ListTile(dense: true, leading: Icon(Icons.power_settings_new), title: Text('Start')),
          ),
      ],
    );
  }
}

// ----------------------------------------------------------------- desktop ---

/// Two ways to look at a machine, because bandwidth is not free on a phone:
/// a single frame on demand, or the full interactive stream.
class _DesktopTab extends ConsumerStatefulWidget {
  const _DesktopTab({required this.instance});
  final Instance instance;

  @override
  ConsumerState<_DesktopTab> createState() => _DesktopTabState();
}

class _DesktopTabState extends ConsumerState<_DesktopTab> {
  bool _streaming = false;
  String? _frame;
  bool _loading = false;
  String? _error;
  WebViewController? _webView;

  @override
  void initState() {
    super.initState();
    if (widget.instance.isRunning) _loadFrame();
  }

  Future<void> _loadFrame() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final shot = await ref.read(apiProvider).observe(widget.instance.id);
      if (mounted) setState(() => _frame = shot);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  void _startStream() {
    final api = ref.read(apiProvider);
    setState(() {
      _streaming = true;
      _webView = WebViewController()
        ..setJavaScriptMode(JavaScriptMode.unrestricted)
        ..setBackgroundColor(Colors.black)
        ..loadRequest(Uri.parse(api.desktopUrl(widget.instance.id)));
    });
  }

  @override
  Widget build(BuildContext context) {
    if (!widget.instance.isRunning) {
      return Center(
        child: Text(
          'This instance is ${widget.instance.state}.',
          style: TextStyle(color: Fleet.ink400),
        ),
      );
    }

    return Column(
      children: [
        Expanded(
          child: Container(
            color: Colors.black,
            width: double.infinity,
            child: _streaming && _webView != null
                ? WebViewWidget(controller: _webView!)
                : _error != null
                    ? Center(
                        child: Padding(
                          padding: const EdgeInsets.all(24),
                          child: Text(
                            _error!,
                            textAlign: TextAlign.center,
                            style: TextStyle(color: Fleet.bad, fontSize: 13),
                          ),
                        ),
                      )
                    : _frame != null
                        ? InteractiveViewer(
                            maxScale: 4,
                            child: Image.memory(
                              base64Decode(_frame!),
                              fit: BoxFit.contain,
                              gaplessPlayback: true,
                            ),
                          )
                        : const Center(child: CircularProgressIndicator()),
          ),
        ),
        SafeArea(
          top: false,
          child: Padding(
            padding: const EdgeInsets.all(12),
            child: Row(
              children: [
                Expanded(
                  child: OutlinedButton.icon(
                    onPressed: _loading
                        ? null
                        : () {
                            setState(() => _streaming = false);
                            _loadFrame();
                          },
                    icon: const Icon(Icons.photo_camera_outlined, size: 18),
                    label: Text(_streaming ? 'Single frame' : 'Refresh frame'),
                  ),
                ),
                const SizedBox(width: 10),
                Expanded(
                  child: FilledButton.icon(
                    onPressed: _streaming ? null : _startStream,
                    icon: const Icon(Icons.cast_connected, size: 18),
                    label: const Text('Take over'),
                  ),
                ),
              ],
            ),
          ),
        ),
      ],
    );
  }
}

// ---------------------------------------------------------------- activity ---

class _ActivityTab extends ConsumerWidget {
  const _ActivityTab({required this.instanceId});
  final String instanceId;

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final tasks = ref.watch(tasksProvider(instanceId));

    return tasks.when(
      loading: () => const Center(child: CircularProgressIndicator()),
      error: (err, _) => Center(child: Text('$err', style: TextStyle(color: Fleet.bad))),
      data: (list) {
        if (list.isEmpty) {
          return Center(
            child: Text('Nothing has run here yet.', style: TextStyle(color: Fleet.ink400)),
          );
        }
        return ListView.separated(
          padding: const EdgeInsets.all(16),
          itemCount: list.length,
          separatorBuilder: (_, __) => const SizedBox(height: 10),
          itemBuilder: (context, i) {
            final task = list[i];
            return Card(
              child: Padding(
                padding: const EdgeInsets.all(14),
                child: Column(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Row(
                      children: [
                        Expanded(
                          child: Text(task.goal, style: const TextStyle(fontSize: 14)),
                        ),
                        const SizedBox(width: 10),
                        StateChip(state: task.state, live: task.state == 'running'),
                      ],
                    ),
                    const SizedBox(height: 8),
                    Text(
                      'step ${task.step}/${task.maxSteps} · ${humanAgo(task.createdAt)}',
                      style: TextStyle(color: Fleet.ink400, fontSize: 11),
                    ),
                    if (task.error.isNotEmpty) ...[
                      const SizedBox(height: 8),
                      Text(task.error, style: TextStyle(color: Fleet.bad, fontSize: 12)),
                    ],
                    if (task.result.isNotEmpty) ...[
                      const SizedBox(height: 8),
                      Text(task.result, style: TextStyle(color: Fleet.good, fontSize: 12)),
                    ],
                    if (task.isLive) ...[
                      const SizedBox(height: 10),
                      Align(
                        alignment: Alignment.centerRight,
                        child: TextButton.icon(
                          style: TextButton.styleFrom(foregroundColor: Fleet.bad),
                          onPressed: () async {
                            await ref.read(apiProvider).cancelTask(task.id);
                            ref.invalidate(tasksProvider(instanceId));
                          },
                          icon: const Icon(Icons.stop, size: 18),
                          label: const Text('Stop this run'),
                        ),
                      ),
                    ],
                  ],
                ),
              ),
            );
          },
        );
      },
    );
  }
}
