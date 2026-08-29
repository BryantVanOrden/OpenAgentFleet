import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../main.dart';

/// Sign-in, plus the server address. A self-hosted platform means there is no
/// well-known endpoint to assume, so the URL is a first-class field rather than
/// something buried in settings.
class LoginScreen extends ConsumerStatefulWidget {
  const LoginScreen({super.key, this.onSignedIn});

  final VoidCallback? onSignedIn;

  @override
  ConsumerState<LoginScreen> createState() => _LoginScreenState();
}

class _LoginScreenState extends ConsumerState<LoginScreen> {
  final _server = TextEditingController();
  final _email = TextEditingController();
  final _password = TextEditingController();
  bool _busy = false;
  String? _error;

  /// Prefilled orchestrator endpoint. The `10.0.2.2` fallback is the Android
  /// emulator's alias for the host loopback; it resolves to nothing on a real
  /// handset, so anyone shipping an APK to a physical phone should bake in a
  /// reachable address at build time instead:
  ///   flutter build apk --dart-define=AGENTFLEET_SERVER=http://host:8080
  /// A tailnet name works from any network with no port forward and no pinned
  /// LAN address, which makes it a good choice for that value.
  static const _defaultServer = String.fromEnvironment(
    'AGENTFLEET_SERVER',
    defaultValue: 'http://10.0.2.2:8080',
  );

  @override
  void initState() {
    super.initState();
    final api = ref.read(apiProvider);
    _server.text = api.baseUrl.isEmpty ? _defaultServer : api.baseUrl;
  }

  @override
  void dispose() {
    _server.dispose();
    _email.dispose();
    _password.dispose();
    super.dispose();
  }

  Future<void> _submit() async {
    setState(() {
      _busy = true;
      _error = null;
    });
    final api = ref.read(apiProvider);
    try {
      await api.setBaseUrl(_server.text);
      await api.login(_email.text.trim(), _password.text);
      ref.read(sessionProvider.notifier).state++;
      widget.onSignedIn?.call();
      if (!mounted) return;
      Navigator.of(context).pushReplacement(
        MaterialPageRoute(builder: (_) => const HomeShell()),
      );
    } catch (err) {
      setState(() => _error = err.toString());
    } finally {
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      body: SafeArea(
        child: Center(
          child: SingleChildScrollView(
            padding: const EdgeInsets.all(24),
            child: ConstrainedBox(
              constraints: const BoxConstraints(maxWidth: 420),
              child: Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [
                  Center(
                    child: Container(
                      width: 56,
                      height: 56,
                      decoration: BoxDecoration(
                        color: Fleet.live,
                        borderRadius: BorderRadius.circular(16),
                      ),
                      child: Center(
                        child: Text(
                          'AF',
                          style: TextStyle(
                            color: Fleet.ink950,
                            fontWeight: FontWeight.bold,
                            fontSize: 20,
                          ),
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(height: 20),
                  const Text(
                    'AgentFleet',
                    textAlign: TextAlign.center,
                    style: TextStyle(fontSize: 22, fontWeight: FontWeight.w600),
                  ),
                  const SizedBox(height: 4),
                  Text(
                    'Watch, talk to, and take over your agents.',
                    textAlign: TextAlign.center,
                    style: TextStyle(color: Fleet.ink400),
                  ),
                  const SizedBox(height: 28),
                  TextField(
                    controller: _server,
                    keyboardType: TextInputType.url,
                    autocorrect: false,
                    decoration: const InputDecoration(
                      labelText: 'Orchestrator URL',
                      hintText: 'https://agents.example.com',
                    ),
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    controller: _email,
                    keyboardType: TextInputType.emailAddress,
                    autocorrect: false,
                    decoration: const InputDecoration(labelText: 'Email'),
                  ),
                  const SizedBox(height: 12),
                  TextField(
                    controller: _password,
                    obscureText: true,
                    onSubmitted: (_) => _submit(),
                    decoration: const InputDecoration(labelText: 'Password'),
                  ),
                  if (_error != null) ...[
                    const SizedBox(height: 16),
                    Container(
                      padding: const EdgeInsets.all(12),
                      decoration: BoxDecoration(
                        color: Fleet.bad.withValues(alpha: 0.1),
                        borderRadius: BorderRadius.circular(12),
                        border: Border.all(
                            color: Fleet.bad.withValues(alpha: 0.25)),
                      ),
                      child: Text(_error!, style: TextStyle(color: Fleet.bad)),
                    ),
                  ],
                  const SizedBox(height: 20),
                  FilledButton(
                    onPressed: _busy ? null : _submit,
                    child: _busy
                        ? const SizedBox(
                            width: 20,
                            height: 20,
                            child: CircularProgressIndicator(strokeWidth: 2),
                          )
                        : const Text('Sign in'),
                  ),
                  const SizedBox(height: 12),
                  Text(
                    'On an Android emulator, 10.0.2.2 is the host machine.',
                    textAlign: TextAlign.center,
                    style: TextStyle(color: Fleet.ink400, fontSize: 12),
                  ),
                ],
              ),
            ),
          ),
        ),
      ),
    );
  }
}
