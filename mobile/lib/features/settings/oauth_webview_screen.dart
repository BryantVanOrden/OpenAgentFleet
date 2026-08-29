import 'dart:async';
import 'dart:io' show Platform;

import 'package:flutter/foundation.dart' show kIsWeb;
import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';
import 'package:url_launcher/url_launcher.dart';
import 'package:webview_flutter/webview_flutter.dart';

import '../../core/state.dart';
import '../../core/theme/theme.dart';

/// The provider's own sign-in page, inside the app.
///
/// The consent page is loaded in a webview and the provider redirects back to
/// this server, which does the exchange. Nothing sensitive passes through the
/// app: it displays a page it does not control and is told at the end whether
/// the sign-in worked.
///
/// Loading someone else's login page in a webview you control is exactly the
/// shape of a credential-phishing screen, so this deliberately keeps the real
/// URL visible in a bar the page cannot paint over, and offers a system-browser
/// escape hatch for anyone who would rather type their password there.
class OAuthWebViewScreen extends ConsumerStatefulWidget {
  const OAuthWebViewScreen({
    super.key,
    required this.authorizeUrl,
    required this.state,
    required this.providerName,
  });

  final String authorizeUrl;
  final String state;
  final String providerName;

  /// True once the server reports the sign-in stored.
  static Future<bool?> show(
    BuildContext context, {
    required String authorizeUrl,
    required String state,
    required String providerName,
  }) =>
      Navigator.of(context).push<bool>(MaterialPageRoute(
        fullscreenDialog: true,
        builder: (_) => OAuthWebViewScreen(
          authorizeUrl: authorizeUrl,
          state: state,
          providerName: providerName,
        ),
      ));

  @override
  ConsumerState<OAuthWebViewScreen> createState() => _OAuthWebViewScreenState();
}

class _OAuthWebViewScreenState extends ConsumerState<OAuthWebViewScreen> {
  WebViewController? _controller;
  Timer? _poll;
  String _currentUrl = '';
  String? _error;
  bool _finished = false;

  /// webview_flutter ships no Linux implementation. Rather than fail there,
  /// the desktop build hands the page to the system browser and waits on the
  /// same poll — the flow completes either way because the exchange happens on
  /// the server, not in whatever rendered the page.
  bool get _hasInAppWebView =>
      !kIsWeb && (Platform.isAndroid || Platform.isIOS);

  @override
  void initState() {
    super.initState();
    _currentUrl = widget.authorizeUrl;
    if (_hasInAppWebView) {
      _controller = WebViewController()
        ..setJavaScriptMode(JavaScriptMode.unrestricted)
        ..setNavigationDelegate(NavigationDelegate(
          onUrlChange: (change) {
            if (mounted) setState(() => _currentUrl = change.url ?? '');
          },
        ))
        ..loadRequest(Uri.parse(widget.authorizeUrl));
    } else {
      unawaited(launchUrl(Uri.parse(widget.authorizeUrl),
          mode: LaunchMode.externalApplication));
    }
    // The server knows the outcome the moment the provider redirects, so the
    // app polls rather than trying to parse a redirect URL it may never see.
    _poll = Timer.periodic(const Duration(seconds: 2), (_) => _check());
  }

  @override
  void dispose() {
    _poll?.cancel();
    super.dispose();
  }

  Future<void> _check() async {
    if (_finished) return;
    try {
      final done = await ref.read(apiProvider).inAppSignInComplete(widget.state);
      if (done && mounted) {
        _finished = true;
        _poll?.cancel();
        Navigator.pop(context, true);
      }
    } catch (err) {
      // A declined or expired sign-in is terminal. Retrying every two seconds
      // forever would leave the operator staring at a page that will never
      // change.
      if (!mounted) return;
      final msg = '$err';
      if (msg.contains('no sign-in is in progress')) return; // not started yet
      _finished = true;
      _poll?.cancel();
      setState(() => _error = msg);
    }
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      appBar: AppBar(
        title: Text('Sign in to ${widget.providerName}',
            style: const TextStyle(fontSize: 15)),
        leading: IconButton(
          icon: const Icon(Icons.close),
          onPressed: () => Navigator.pop(context, false),
        ),
        actions: [
          IconButton(
            tooltip: 'Open in browser instead',
            icon: const Icon(Icons.open_in_browser),
            onPressed: () => launchUrl(Uri.parse(widget.authorizeUrl),
                mode: LaunchMode.externalApplication),
          ),
        ],
        bottom: PreferredSize(
          preferredSize: const Size.fromHeight(26),
          child: Container(
            width: double.infinity,
            padding: const EdgeInsets.only(left: 16, right: 16, bottom: 7),
            child: Row(
              children: [
                Icon(Icons.lock_outline, size: 12, color: Fleet.ink500),
                const SizedBox(width: 5),
                Expanded(
                  child: Text(
                    _hostOf(_currentUrl),
                    overflow: TextOverflow.ellipsis,
                    style: TextStyle(color: Fleet.ink400, fontSize: 11),
                  ),
                ),
              ],
            ),
          ),
        ),
      ),
      body: _error != null ? _errorView() : _body(),
    );
  }

  Widget _body() {
    if (_controller != null) {
      return WebViewWidget(controller: _controller!);
    }
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            const CircularProgressIndicator(),
            const SizedBox(height: 20),
            Text(
              'The sign-in page opened in your browser.\n\nApprove it there and '
              'this will finish on its own.',
              textAlign: TextAlign.center,
              style: TextStyle(color: Fleet.ink400, fontSize: 13, height: 1.45),
            ),
          ],
        ),
      ),
    );
  }

  Widget _errorView() => Center(
        child: Padding(
          padding: const EdgeInsets.all(28),
          child: Column(
            mainAxisSize: MainAxisSize.min,
            children: [
              Icon(Icons.error_outline, size: 32, color: Fleet.bad),
              const SizedBox(height: 14),
              Text(_error!,
                  textAlign: TextAlign.center,
                  style:
                      TextStyle(color: Fleet.ink300, fontSize: 13, height: 1.45)),
              const SizedBox(height: 20),
              FilledButton(
                onPressed: () => Navigator.pop(context, false),
                child: const Text('Close'),
              ),
            ],
          ),
        ),
      );

  String _hostOf(String url) {
    final u = Uri.tryParse(url);
    if (u == null || u.host.isEmpty) return url;
    return u.host;
  }
}
