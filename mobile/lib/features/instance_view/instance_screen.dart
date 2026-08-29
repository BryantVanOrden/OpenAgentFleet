import 'dart:async';
import 'dart:convert';
import 'dart:io' show Platform;

import 'package:flutter/foundation.dart' show Factory, kIsWeb;
import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:url_launcher/url_launcher.dart';
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
    final instances =
        ref.watch(instancesProvider).valueOrNull ?? const <Instance>[];
    final instance =
        instances.where((i) => i.id == widget.instanceId).firstOrNull;

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
            child: Center(
                child:
                    StateChip(state: instance.state, live: instance.isRunning)),
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
      final navigator = Navigator.of(context);

      // Destroying an agent takes its disk with it, so it is the one action
      // here that asks first. The others are all reversible.
      if (action == 'delete') {
        final confirmed = await showDialog<bool>(
          context: context,
          builder: (context) => AlertDialog(
            backgroundColor: Fleet.ink850,
            title: const Text('Delete this agent?'),
            content: Text(
              '"${instance.name}" and everything on its disk will be removed. '
              'This cannot be undone.',
              style: TextStyle(color: Fleet.ink300),
            ),
            actions: [
              TextButton(
                onPressed: () => Navigator.pop(context, false),
                child: const Text('Cancel'),
              ),
              FilledButton(
                style: FilledButton.styleFrom(backgroundColor: Fleet.bad),
                onPressed: () => Navigator.pop(context, true),
                child: const Text('Delete'),
              ),
            ],
          ),
        );
        if (confirmed != true) return;
        try {
          await api.deleteInstance(instance.id);
          ref.invalidate(instancesProvider);
          // The screen it was showing no longer exists.
          navigator.pop();
        } catch (err) {
          messenger.showSnackBar(SnackBar(content: Text('$err')));
        }
        return;
      }

      if (action == 'shell') {
        try {
          final updated =
              await api.setShellAccess(instance.id, !instance.shellAccess);
          ref.invalidate(instancesProvider);
          messenger.showSnackBar(SnackBar(
            content: Text(updated.shellAccess
                ? 'Shell access allowed'
                : 'Shell access revoked'),
          ));
        } catch (err) {
          messenger.showSnackBar(SnackBar(content: Text('$err')));
        }
        return;
      }

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
            child: ListTile(
                dense: true,
                leading: Icon(Icons.play_arrow),
                title: Text('Resume')),
          )
        else
          const PopupMenuItem(
            value: 'start',
            child: ListTile(
                dense: true,
                leading: Icon(Icons.power_settings_new),
                title: Text('Start')),
          ),
        const PopupMenuDivider(),
        PopupMenuItem(
          value: 'shell',
          child: ListTile(
            dense: true,
            leading: Icon(
              instance.shellAccess ? Icons.terminal : Icons.terminal_outlined,
              color: instance.shellAccess ? Fleet.warn : null,
            ),
            title: Text(
                instance.shellAccess ? 'Revoke shell access' : 'Allow shell access'),
            subtitle: Text(
              instance.shellAccess
                  ? 'Takes effect on the next step'
                  : 'Lets this agent run commands directly',
            ),
          ),
        ),
        PopupMenuItem(
          enabled: false,
          child: ListTile(
            dense: true,
            leading: Icon(Icons.admin_panel_settings_outlined,
                color: Fleet.ink500),
            title: Text('Sudo: ${instance.sudoAccess ? "on" : "off"}',
                style: TextStyle(color: Fleet.ink400)),
            // Not a toggle, and saying so beats a switch that does nothing:
            // sudo depends on a container option the kernel applies at
            // creation, so it can only be chosen when the agent is built.
            subtitle: Text('Fixed when the agent was created',
                style: TextStyle(color: Fleet.ink500)),
          ),
        ),
        const PopupMenuDivider(),
        PopupMenuItem(
          value: 'delete',
          child: ListTile(
            dense: true,
            leading: Icon(Icons.delete_outline, color: Fleet.bad),
            title: Text('Delete', style: TextStyle(color: Fleet.bad)),
            subtitle: const Text('Removes the agent and its disk'),
          ),
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

  /// Keeps the still frame current so the Desktop tab shows what the agent is
  /// doing right now rather than a snapshot from whenever you opened it. Two
  /// seconds is frequent enough to follow a run and cheap enough on mobile
  /// data: /observe returns one downscaled webp, not a video stream.
  Timer? _refresh;
  bool _live = true;
  String? _error;
  bool _recording = false;
  WebViewController? _webView;

  @override
  void initState() {
    super.initState();
    if (widget.instance.isRunning) _setLive(true);
  }

  @override
  void dispose() {
    // A timer left running after the tab is gone keeps polling the API for a
    // screen nobody is looking at.
    _refresh?.cancel();
    super.dispose();
  }

  void _setLive(bool on) {
    setState(() => _live = on);
    _refresh?.cancel();
    if (on && !_streaming) {
      _refresh = Timer.periodic(
          const Duration(seconds: 2), (_) => _loadFrame(quiet: true));
      _loadFrame(quiet: true);
    }
  }

  /// [quiet] suppresses the spinner: a refresh every two seconds that flashes
  /// a loading state would be unreadable.
  Future<void> _loadFrame({bool quiet = false}) async {
    if (_loading) return;
    setState(() {
      _loading = !quiet;
      if (!quiet) _error = null;
    });
    try {
      final shot = await ref.read(apiProvider).observe(widget.instance.id);
      if (mounted) {
        setState(() {
          _frame = shot;
          _error = null;
        });
      }
    } catch (err) {
      // A failed poll keeps the last good frame; only a manual refresh with
      // nothing to show is worth an error state.
      if (mounted && (!quiet || _frame == null)) {
        setState(() => _error = '$err');
      }
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  /// webview_flutter has no Linux or Windows implementation, so the desktop
  /// cannot be embedded there.
  static bool get _canEmbedWebView =>
      !kIsWeb && (Platform.isAndroid || Platform.isIOS || Platform.isMacOS);

  /// ...but "no embedded view" is not "no takeover". On a desktop there is a
  /// real browser one launch away, and it is a better VNC client than an
  /// embedded webview: a proper window, a real keyboard, a real mouse. Only
  /// the web build has nowhere to send it.
  static bool get _canOpenExternally => !kIsWeb;

  bool get _canTakeOver => _canEmbedWebView || _canOpenExternally;

  /// Hand the desktop to the system browser.
  Future<void> _openInBrowser() async {
    final url = ref.read(apiProvider).desktopUrl(widget.instance.id);
    final messenger = ScaffoldMessenger.of(context);
    try {
      final ok = await launchUrl(Uri.parse(url),
          mode: LaunchMode.externalApplication);
      if (!ok && mounted) await _offerUrl(url);
    } catch (err) {
      // A desktop whose default browser is missing or misconfigured should not
      // be a dead end: the URL carries its own auth token, so handing it over
      // is the whole of what the button was going to do anyway.
      if (mounted) {
        await _offerUrl(url);
      } else {
        messenger.showSnackBar(SnackBar(content: Text('$err')));
      }
    }
  }

  /// Shown when no browser could be launched: the address, ready to copy.
  Future<void> _offerUrl(String url) async {
    await showDialog<void>(
      context: context,
      builder: (context) => AlertDialog(
        backgroundColor: Fleet.ink850,
        title: const Text('Open the desktop manually'),
        content: Column(
          mainAxisSize: MainAxisSize.min,
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            Text(
              'No browser could be launched automatically. Paste this into one:',
              style: TextStyle(color: Fleet.ink300, fontSize: 13),
            ),
            const SizedBox(height: 12),
            SelectableText(url, style: const TextStyle(fontSize: 11)),
          ],
        ),
        actions: [
          TextButton(
            onPressed: () async {
              await Clipboard.setData(ClipboardData(text: url));
              if (context.mounted) Navigator.pop(context);
            },
            child: const Text('Copy'),
          ),
          FilledButton(
            onPressed: () => Navigator.pop(context),
            child: const Text('Close'),
          ),
        ],
      ),
    );
  }

  /// Raise the phone's keyboard for the remote desktop.
  ///
  /// noVNC's UI is an ES module, so its API is not reachable from injected
  /// script; its control bar buttons are ordinary DOM elements, and clicking
  /// the keyboard one focuses the hidden input that makes Android show the
  /// soft keyboard. Keystrokes then go to the desktop, not to the page.
  Future<void> _toggleRemoteKeyboard() async {
    final web = _webView;
    if (web == null) return;
    try {
      await web.runJavaScript(
        "document.getElementById('noVNC_keyboard_button')?.click();",
      );
    } catch (err) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Could not open the remote keyboard: $err')),
        );
      }
    }
  }

  void _startStream() {
    if (!_canEmbedWebView) return;
    // The interactive stream is already live; polling stills on top of it
    // would be pure waste.
    _refresh?.cancel();
    final api = ref.read(apiProvider);
    setState(() {
      _streaming = true;
      _webView = WebViewController()
        ..setJavaScriptMode(JavaScriptMode.unrestricted)
        ..setBackgroundColor(Colors.black)
        ..loadRequest(Uri.parse(api.desktopUrl(widget.instance.id)));
    });
  }

  Future<void> _toggleRecording() async {
    final api = ref.read(apiProvider);
    if (_recording) {
      try {
        final skill = await api.stopRecording(widget.instance.id);
        setState(() => _recording = false);
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(
              content: Text('✅ Compiled demonstration into skill: "${skill.name}" (${skill.stepCount} steps)!'),
              backgroundColor: Fleet.live,
            ),
          );
        }
      } catch (err) {
        if (mounted) {
          ScaffoldMessenger.of(context).showSnackBar(
            SnackBar(content: Text('Error: $err'), backgroundColor: Fleet.bad),
          );
        }
      }
    } else {
      final ctrl = TextEditingController();
      final name = await showDialog<String>(
        context: context,
        builder: (ctx) => AlertDialog(
          title: const Text('🎬 Record Demonstration'),
          content: TextField(
            controller: ctrl,
            autofocus: true,
            decoration: const InputDecoration(
              hintText: 'e.g. Export CRM Invoices to PDF',
              labelText: 'What task is this teaching the AI?',
            ),
          ),
          actions: [
            TextButton(
              onPressed: () => Navigator.pop(ctx),
              child: const Text('Cancel'),
            ),
            FilledButton(
              onPressed: () => Navigator.pop(ctx, ctrl.text.trim()),
              child: const Text('Start Recording'),
            ),
          ],
        ),
      );

      if (name != null && name.isNotEmpty) {
        try {
          await api.startRecording(widget.instance.id, name);
          setState(() => _recording = true);
          if (!_streaming && _canEmbedWebView) _startStream();
          if (mounted) {
            ScaffoldMessenger.of(context).showSnackBar(
              const SnackBar(content: Text('🔴 Recording started. Interact with desktop now.')),
            );
          }
        } catch (err) {
          if (mounted) {
            ScaffoldMessenger.of(context).showSnackBar(
              SnackBar(content: Text('Error: $err'), backgroundColor: Fleet.bad),
            );
          }
        }
      }
    }
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
        if (_recording)
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 8),
            color: Fleet.bad.withValues(alpha: 0.2),
            child: Row(
              children: [
                Container(
                  width: 8,
                  height: 8,
                  decoration: BoxDecoration(
                    color: Fleet.bad,
                    shape: BoxShape.circle,
                  ),
                ),
                const SizedBox(width: 8),
                const Expanded(
                  child: Text(
                    '🔴 Recording Actions for AI...',
                    style: TextStyle(fontSize: 12, fontWeight: FontWeight.bold),
                  ),
                ),
                TextButton.icon(
                  onPressed: _toggleRecording,
                  icon: const Icon(Icons.stop, size: 16, color: Colors.white),
                  label: const Text('Stop & Compile', style: TextStyle(color: Colors.white, fontSize: 12)),
                  style: TextButton.styleFrom(
                    backgroundColor: Fleet.bad,
                    padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 4),
                  ),
                ),
              ],
            ),
          ),
        Expanded(
          child: Container(
            color: Colors.black,
            width: double.infinity,
            child: _streaming && _webView != null
                ? WebViewWidget(
                    controller: _webView!,
                    // Without this the enclosing TabBarView wins the arena for
                    // every horizontal drag, so dragging on the desktop swipes
                    // to the next tab instead of moving the mouse. Eager
                    // recognition hands all touch straight to noVNC, which
                    // does its own touch-to-mouse translation.
                    gestureRecognizers: {
                      Factory<OneSequenceGestureRecognizer>(
                          EagerGestureRecognizer.new),
                    },
                  )
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
            child: Column(
              children: [
                Row(
                  children: [
                    Expanded(
                      child: OutlinedButton.icon(
                        // Live by default so the tab shows what is happening
                        // now; switchable off for a still on metered data.
                        onPressed: () {
                          setState(() => _streaming = false);
                          _setLive(!_live);
                        },
                        icon: Icon(
                          _live && !_streaming
                              ? Icons.pause_circle_outline
                              : Icons.play_circle_outline,
                          size: 18,
                        ),
                        label: Text(
                          _live && !_streaming ? 'Pause live' : 'Go live',
                        ),
                      ),
                    ),
                    const SizedBox(width: 10),
                    Expanded(
                      child: Tooltip(
                        message: _canEmbedWebView
                            ? 'Drive the desktop directly'
                            : _canOpenExternally
                                ? 'Opens the desktop in your browser'
                                : 'Takeover is unavailable on this platform',
                        child: FilledButton.icon(
                          onPressed: !_canTakeOver || (_streaming && _canEmbedWebView)
                              ? null
                              : _canEmbedWebView
                                  ? _startStream
                                  : _openInBrowser,
                          icon: Icon(
                            _canEmbedWebView
                                ? Icons.cast_connected
                                : _canOpenExternally
                                    ? Icons.open_in_new
                                    : Icons.desktop_access_disabled_outlined,
                            size: 18,
                          ),
                          label: Text(_canTakeOver ? 'Take over' : 'Unavailable'),
                        ),
                      ),
                    ),
                    // Only useful once the stream is up, and only reachable
                    // there — noVNC's own keyboard button lives in a control
                    // bar that collapses to a thin handle on a phone.
                    if (_streaming) ...[
                      const SizedBox(width: 8),
                      IconButton(
                        onPressed: _toggleRemoteKeyboard,
                        tooltip: 'Keyboard',
                        icon: const Icon(Icons.keyboard_alt_outlined),
                      ),
                    ],
                  ],
                ),
                const SizedBox(height: 8),
                SizedBox(
                  width: double.infinity,
                  child: FilledButton.icon(
                    onPressed: _toggleRecording,
                    style: FilledButton.styleFrom(
                      backgroundColor: _recording ? Fleet.bad : Fleet.ink800,
                    ),
                    icon: Icon(_recording ? Icons.stop_circle : Icons.fiber_manual_record, size: 18),
                    label: Text(_recording ? 'Stop & Compile Demonstration' : '🎬 Teach Bot (Record Demonstration)'),
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
      error: (err, _) =>
          Center(child: Text('$err', style: TextStyle(color: Fleet.bad))),
      data: (list) {
        if (list.isEmpty) {
          return Center(
            child: Text('Nothing has run here yet.',
                style: TextStyle(color: Fleet.ink400)),
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
                          child: Text(task.goal,
                              style: const TextStyle(fontSize: 14)),
                        ),
                        const SizedBox(width: 10),
                        StateChip(
                            state: task.state, live: task.state == 'running'),
                      ],
                    ),
                    const SizedBox(height: 8),
                    Text(
                      'step ${task.step}/${task.maxSteps} · ${humanAgo(task.createdAt)}',
                      style: TextStyle(color: Fleet.ink400, fontSize: 11),
                    ),
                    if (task.error.isNotEmpty) ...[
                      const SizedBox(height: 8),
                      Text(task.error,
                          style: TextStyle(color: Fleet.bad, fontSize: 12)),
                    ],
                    if (task.result.isNotEmpty) ...[
                      const SizedBox(height: 8),
                      Text(task.result,
                          style: TextStyle(color: Fleet.good, fontSize: 12)),
                    ],
                    if (task.isLive) ...[
                      const SizedBox(height: 10),
                      Align(
                        alignment: Alignment.centerRight,
                        child: TextButton.icon(
                          style:
                              TextButton.styleFrom(foregroundColor: Fleet.bad),
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
