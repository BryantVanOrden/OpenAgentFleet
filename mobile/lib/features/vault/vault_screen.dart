import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';

class VaultScreen extends ConsumerStatefulWidget {
  const VaultScreen({super.key});

  @override
  ConsumerState<VaultScreen> createState() => _VaultScreenState();
}

class _VaultScreenState extends ConsumerState<VaultScreen> with SingleTickerProviderStateMixin {
  late final TabController _tabs = TabController(length: 3, vsync: this);
  final TextEditingController _broadcastCtrl = TextEditingController();

  List<PeerMessage> _messages = [];
  List<SharedSecret> _secrets = [];
  List<SharedSession> _sessions = [];
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
    _broadcastCtrl.dispose();
    super.dispose();
  }

  Future<void> _load() async {
    setState(() {
      _loading = true;
      _error = null;
    });
    try {
      final api = ref.read(apiProvider);
      final msgs = await api.peerMessages();
      final secs = await api.sharedSecrets();
      final sess = await api.sharedSessions();
      if (mounted) {
        setState(() {
          _messages = msgs;
          _secrets = secs;
          _sessions = sess;
        });
      }
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
    } finally {
      if (mounted) setState(() => _loading = false);
    }
  }

  Future<void> _broadcast() async {
    final text = _broadcastCtrl.text.trim();
    if (text.isEmpty) return;
    try {
      final api = ref.read(apiProvider);
      await api.sendPeerMessage(
        content: text,
        fromInstanceName: 'Mobile Operator',
        toInstanceId: 'broadcast',
      );
      _broadcastCtrl.clear();
      _load();
    } catch (err) {
      if (mounted) {
        ScaffoldMessenger.of(context).showSnackBar(
          SnackBar(content: Text('Error: $err'), backgroundColor: Fleet.bad),
        );
      }
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
    return Scaffold(
      appBar: AppBar(
        title: const Text('🔐 Fleet Vault & P2P Comms'),
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
            Tab(text: 'Comms (${_messages.length})'),
            Tab(text: 'Secrets (${_secrets.length})'),
            Tab(text: 'Sessions (${_sessions.length})'),
          ],
        ),
      ),
      body: _loading && _messages.isEmpty
          ? const Center(child: CircularProgressIndicator())
          : _error != null && _messages.isEmpty
              ? Center(child: Text(_error!, style: TextStyle(color: Fleet.bad)))
              : TabBarView(
                  controller: _tabs,
                  children: [
                    _buildCommsTab(),
                    _buildSecretsTab(),
                    _buildSessionsTab(),
                  ],
                ),
    );
  }

  Widget _buildCommsTab() {
    return Column(
      children: [
        Expanded(
          child: _messages.isEmpty
              ? Center(
                  child: Text('No inter-agent messages recorded yet.',
                      style: TextStyle(color: Fleet.ink400)),
                )
              : ListView.builder(
                  padding: const EdgeInsets.all(12),
                  itemCount: _messages.length,
                  itemBuilder: (ctx, i) {
                    final m = _messages[i];
                    final isOperator = m.fromInstanceName.contains('Operator') ||
                        m.fromInstanceName.contains('Admin');
                    return Container(
                      margin: const EdgeInsets.only(bottom: 8),
                      padding: const EdgeInsets.all(12),
                      decoration: BoxDecoration(
                        color: isOperator ? Fleet.live.withValues(alpha: 0.1) : Fleet.ink900,
                        borderRadius: BorderRadius.circular(10),
                        border: Border.all(
                          color: isOperator ? Fleet.live.withValues(alpha: 0.3) : Fleet.ink800,
                        ),
                      ),
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Row(
                            mainAxisAlignment: MainAxisAlignment.spaceBetween,
                            children: [
                              Text(
                                '${m.fromInstanceName} ➔ ${m.toInstanceId}',
                                style: const TextStyle(
                                  fontSize: 11,
                                  fontWeight: FontWeight.bold,
                                  color: Colors.white,
                                ),
                              ),
                              Container(
                                padding: const EdgeInsets.symmetric(horizontal: 6, vertical: 2),
                                decoration: BoxDecoration(
                                  color: Fleet.ink800,
                                  borderRadius: BorderRadius.circular(4),
                                ),
                                child: Text(
                                  m.kind.toUpperCase(),
                                  style: TextStyle(fontSize: 9, color: Fleet.ink300),
                                ),
                              ),
                            ],
                          ),
                          const SizedBox(height: 4),
                          Text(m.content, style: const TextStyle(fontSize: 13)),
                        ],
                      ),
                    );
                  },
                ),
        ),
        Container(
          padding: const EdgeInsets.all(12),
          decoration: BoxDecoration(
            color: Fleet.ink900,
            border: Border(top: BorderSide(color: Fleet.ink800)),
          ),
          child: Row(
            children: [
              Expanded(
                child: TextField(
                  controller: _broadcastCtrl,
                  decoration: const InputDecoration(
                    hintText: 'Broadcast directive to all agents...',
                    contentPadding: EdgeInsets.symmetric(horizontal: 12, vertical: 8),
                  ),
                  onSubmitted: (_) => _broadcast(),
                ),
              ),
              const SizedBox(width: 8),
              FilledButton(
                onPressed: _broadcast,
                child: const Icon(Icons.send, size: 18),
              ),
            ],
          ),
        ),
      ],
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
