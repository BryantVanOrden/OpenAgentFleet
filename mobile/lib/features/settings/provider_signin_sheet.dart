import 'dart:async';

import 'package:flutter/material.dart';
import 'package:flutter/services.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:url_launcher/url_launcher.dart';

import 'oauth_webview_screen.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';

/// Sign in to a provider with a Google account.
///
/// You are shown a short code and sent to a Google page to approve it, the same
/// shape as signing a TV into an account. It works this way rather than through
/// a browser redirect because the server runs in a container and this app is
/// often a phone — neither can reliably catch a redirect back to localhost, and
/// a code you type on whatever device is handy works from both.
class ProviderSignInSheet extends ConsumerStatefulWidget {
  const ProviderSignInSheet({super.key, required this.provider});

  final AIProvider provider;

  static Future<bool?> show(BuildContext context, AIProvider provider) =>
      showModalBottomSheet<bool>(
        context: context,
        isScrollControlled: true,
        isDismissible: false,
        builder: (_) => ProviderSignInSheet(provider: provider),
      );

  @override
  ConsumerState<ProviderSignInSheet> createState() =>
      _ProviderSignInSheetState();
}

class _ProviderSignInSheetState extends ConsumerState<ProviderSignInSheet> {
  late final _clientId =
      TextEditingController(text: widget.provider.oauthClientId);
  final _clientSecret = TextEditingController();

  String _userCode = '';
  String _verifyUrl = '';
  String? _error;
  bool _starting = false;
  Timer? _poll;

  @override
  void initState() {
    super.initState();
    _loadRedirectUri();
  }

  Future<void> _loadRedirectUri() async {
    try {
      final uri = await ref.read(apiProvider).oauthRedirectUri();
      if (mounted) setState(() => _redirectUri = uri);
    } catch (_) {
      // Falls back to the app's own address below, which is right whenever the
      // server is not behind a different public URL.
      if (mounted) {
        setState(() => _redirectUri =
            '${ref.read(apiProvider).baseUrl}/api/providers/oauth/callback');
      }
    }
  }

  @override
  void dispose() {
    _poll?.cancel();
    _clientId.dispose();
    _clientSecret.dispose();
    super.dispose();
  }

  /// Sign in inside the app: load the provider's consent page in a webview and
  /// let the server handle the redirect. This is the path that finishes without
  /// leaving AgentFleet.
  Future<void> _startInApp() async {
    if (_clientId.text.trim().isEmpty) {
      setState(() => _error = 'An OAuth client ID is required.');
      return;
    }
    setState(() {
      _starting = true;
      _error = null;
    });
    try {
      final res = await ref.read(apiProvider).startInAppSignIn(
            widget.provider.id,
            clientId: _clientId.text.trim(),
            clientSecret: _clientSecret.text.trim(),
          );
      if (!mounted) return;
      setState(() => _starting = false);

      final ok = await OAuthWebViewScreen.show(
        context,
        authorizeUrl: res.authorizeUrl,
        state: res.state,
        providerName: widget.provider.name,
      );
      if (!mounted) return;
      if (ok == true) {
        Navigator.pop(context, true);
      } else {
        setState(() => _error = 'Sign-in was not completed.');
      }
    } catch (err) {
      if (!mounted) return;
      setState(() {
        _starting = false;
        _error = '$err';
      });
    }
  }

  Future<void> _start() async {
    if (_clientId.text.trim().isEmpty) {
      setState(() => _error = 'An OAuth client ID is required.');
      return;
    }
    setState(() {
      _starting = true;
      _error = null;
    });
    try {
      final res = await ref.read(apiProvider).startProviderSignIn(
            widget.provider.id,
            clientId: _clientId.text.trim(),
            clientSecret: _clientSecret.text.trim(),
          );
      if (!mounted) return;
      setState(() {
        _userCode = res.userCode;
        _verifyUrl = res.verificationUrl;
        _starting = false;
      });
      _poll = Timer.periodic(
          Duration(seconds: res.interval.clamp(3, 30)), (_) => _check());
    } catch (err) {
      if (!mounted) return;
      setState(() {
        _starting = false;
        _error = '$err';
      });
    }
  }

  Future<void> _check() async {
    try {
      final done =
          await ref.read(apiProvider).providerSignInComplete(widget.provider.id);
      if (done && mounted) {
        _poll?.cancel();
        Navigator.pop(context, true);
      }
    } catch (err) {
      // A declined or expired sign-in is terminal; stop polling and say so
      // rather than retrying every few seconds forever.
      if (!mounted) return;
      _poll?.cancel();
      setState(() => _error = '$err');
    }
  }

  Widget _step(int n, String text) => Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Container(
            width: 18,
            height: 18,
            alignment: Alignment.center,
            decoration: BoxDecoration(
              color: Fleet.ink800,
              borderRadius: BorderRadius.circular(9),
            ),
            child: Text('$n',
                style: TextStyle(
                    fontSize: 10,
                    fontWeight: FontWeight.w700,
                    color: Fleet.ink300)),
          ),
          const SizedBox(width: 8),
          Expanded(
            child: Padding(
              padding: const EdgeInsets.only(top: 1),
              child: Text(text,
                  style: TextStyle(color: Fleet.ink300, fontSize: 12)),
            ),
          ),
        ],
      );

  @override
  Widget build(BuildContext context) {
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
            Row(
              children: [
                Expanded(
                  child: Text('Sign in to ${widget.provider.name}',
                      style: const TextStyle(
                          fontSize: 16, fontWeight: FontWeight.w700)),
                ),
                IconButton(
                  icon: const Icon(Icons.close),
                  onPressed: () => Navigator.pop(context, false),
                ),
              ],
            ),
            const SizedBox(height: 8),
            if (_userCode.isEmpty) ..._setupFields() else ..._codeStep(),
            if (_error != null) ...[
              const SizedBox(height: 12),
              Text(_error!,
                  style: TextStyle(color: Fleet.bad, fontSize: 12, height: 1.4)),
            ],
          ],
        ),
      ),
    );
  }

  /// Where Google must send the browser back, as the server itself reports it.
  /// It has to be registered on the OAuth client exactly, so it is shown here
  /// to copy rather than described.
  String _redirectUri = '';

  List<Widget> _setupFields() => [
        Text(
          'Uses your own OAuth client, created in Google Cloud Console. Your '
          'own client is the supported way to do this — borrowing another '
          'product\'s credentials to inherit its subscription breaks whenever '
          'they rotate.',
          style: TextStyle(color: Fleet.ink400, fontSize: 11.5, height: 1.45),
        ),
        const SizedBox(height: 12),
        // Three steps, in order, with the two values that are easy to get
        // wrong made copyable rather than described.
        _step(1, 'Create a Web application OAuth client'),
        Padding(
          padding: const EdgeInsets.only(left: 26, top: 2, bottom: 8),
          child: OutlinedButton.icon(
            onPressed: () => launchUrl(
                Uri.parse('https://console.cloud.google.com/apis/credentials'),
                mode: LaunchMode.externalApplication),
            icon: const Icon(Icons.open_in_new, size: 14),
            label: const Text('Open Google Cloud credentials',
                style: TextStyle(fontSize: 11.5)),
          ),
        ),
        _step(2, 'Add this as an authorised redirect URI'),
        Padding(
          padding: const EdgeInsets.only(left: 26, top: 4, bottom: 8),
          child: InkWell(
            onTap: () {
              Clipboard.setData(ClipboardData(text: _redirectUri));
              ScaffoldMessenger.of(context).showSnackBar(
                const SnackBar(content: Text('Redirect URI copied')),
              );
            },
            child: Container(
              padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 8),
              decoration: BoxDecoration(
                color: Fleet.ink900,
                borderRadius: BorderRadius.circular(7),
                border: Border.all(color: Fleet.ink800),
              ),
              child: Row(
                children: [
                  Expanded(
                    child: Text(_redirectUri,
                        style: TextStyle(
                            fontSize: 10.5,
                            fontFamily: 'monospace',
                            color: Fleet.ink200)),
                  ),
                  Icon(Icons.copy, size: 14, color: Fleet.ink400),
                ],
              ),
            ),
          ),
        ),
        _step(3, 'Paste the client ID and secret below'),
        const SizedBox(height: 12),
        TextField(
          controller: _clientId,
          autocorrect: false,
          decoration: const InputDecoration(
            labelText: 'OAuth client ID',
            hintText: '....apps.googleusercontent.com',
          ),
        ),
        const SizedBox(height: 12),
        TextField(
          controller: _clientSecret,
          obscureText: true,
          autocorrect: false,
          decoration: const InputDecoration(
            labelText: 'Client secret',
            helperText: 'Sealed into the server vault with the sign-in.',
          ),
        ),
        const SizedBox(height: 16),
        SizedBox(
          width: double.infinity,
          child: FilledButton.icon(
            onPressed: _starting ? null : _startInApp,
            icon: _starting
                ? const SizedBox(
                    width: 15,
                    height: 15,
                    child: CircularProgressIndicator(strokeWidth: 2))
                : const Icon(Icons.login, size: 18),
            label: const Text('Sign in'),
          ),
        ),
        const SizedBox(height: 6),
        Center(
          child: TextButton(
            onPressed: _starting ? null : _start,
            child: const Text('Use a code on another device instead',
                style: TextStyle(fontSize: 11.5)),
          ),
        ),
      ];

  List<Widget> _codeStep() => [
        Text('Open the page below and enter this code:',
            style: TextStyle(color: Fleet.ink300, fontSize: 12.5)),
        const SizedBox(height: 14),
        Center(
          child: SelectableText(
            _userCode,
            style: const TextStyle(
              fontSize: 30,
              fontWeight: FontWeight.w700,
              letterSpacing: 5,
              fontFamily: 'monospace',
            ),
          ),
        ),
        const SizedBox(height: 6),
        Center(
          child: TextButton.icon(
            onPressed: () {
              Clipboard.setData(ClipboardData(text: _userCode));
              ScaffoldMessenger.of(context).showSnackBar(
                const SnackBar(content: Text('Code copied')),
              );
            },
            icon: const Icon(Icons.copy, size: 15),
            label: const Text('Copy code', style: TextStyle(fontSize: 12)),
          ),
        ),
        const SizedBox(height: 10),
        SizedBox(
          width: double.infinity,
          child: FilledButton.icon(
            onPressed: () => launchUrl(Uri.parse(_verifyUrl),
                mode: LaunchMode.externalApplication),
            icon: const Icon(Icons.open_in_new, size: 17),
            label: const Text('Open Google sign-in'),
          ),
        ),
        const SizedBox(height: 10),
        Center(
          child: SelectableText(_verifyUrl,
              style: TextStyle(color: Fleet.ink400, fontSize: 11)),
        ),
        const SizedBox(height: 16),
        Row(
          children: [
            const SizedBox(
                width: 14,
                height: 14,
                child: CircularProgressIndicator(strokeWidth: 2)),
            const SizedBox(width: 10),
            Text('Waiting for you to approve…',
                style: TextStyle(color: Fleet.ink400, fontSize: 12)),
          ],
        ),
      ];
}
