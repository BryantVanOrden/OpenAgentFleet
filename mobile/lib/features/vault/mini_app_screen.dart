import 'dart:io';

import 'package:flutter/material.dart';
import 'package:webview_flutter/webview_flutter.dart';

import '../../core/models.dart';
import '../../core/theme/theme.dart';

/// Runs a mini-app an agent published.
///
/// The document is loaded as a string rather than from a URL, so it runs on an
/// opaque origin with no server behind it. That is the containment: the page
/// has no cookies, no access to this app's token, and nothing to reach even if
/// it tried. Anything an agent wants its app to have must be inside the
/// document, which is also why the prompt tells them to inline everything.
///
/// webview_flutter has no desktop implementation, so on Linux and Windows this
/// says so rather than showing an empty box that looks broken.
class MiniAppScreen extends StatefulWidget {
  const MiniAppScreen({super.key, required this.item});

  final WorkItem item;

  @override
  State<MiniAppScreen> createState() => _MiniAppScreenState();
}

class _MiniAppScreenState extends State<MiniAppScreen> {
  WebViewController? _controller;
  bool _loading = true;

  bool get _supported => Platform.isAndroid || Platform.isIOS;

  @override
  void initState() {
    super.initState();
    if (!_supported) return;
    _controller = WebViewController()
      ..setJavaScriptMode(JavaScriptMode.unrestricted)
      // The page paints its own background; without this an opaque white
      // frame flashes over a dark game every time it loads.
      ..setBackgroundColor(Colors.black)
      ..setNavigationDelegate(NavigationDelegate(
        onPageFinished: (_) {
          if (mounted) setState(() => _loading = false);
        },
        // A published app is meant to be self-contained. If one tries to
        // navigate somewhere, refuse rather than let the view become a
        // browser pointed at whatever an agent decided to open.
        onNavigationRequest: (req) => req.url.startsWith('about:')
            ? NavigationDecision.navigate
            : NavigationDecision.prevent,
      ))
      // A Content-Security-Policy is what actually stops the page reaching the
      // network. The navigation delegate only sees top-level navigation, so on
      // its own it does nothing about fetch, XMLHttpRequest, a WebSocket, or
      // an <img> src assigned at runtime -- and a publish-time scan for
      // src="http can be walked around with string concatenation. Enforcing it
      // in the document is the only place that holds.
      //
      // default-src 'self' with no origin to be 'self' of, plus 'unsafe-inline'
      // and 'unsafe-eval' so an inline game still runs, and connect-src 'none'
      // so nothing can call out.
      ..loadHtmlString(_sandboxed(widget.item.content));
  }

  /// Prepends a Content-Security-Policy that denies the page any network.
  ///
  /// Inserted rather than required of the author: an agent writing a game
  /// should not have to remember a security header, and one that forgot would
  /// otherwise be trusted. Put first inside `<head>` so it applies before
  /// anything in the document can act.
  static String _sandboxed(String html) {
    const csp = '<meta http-equiv="Content-Security-Policy" '
        "content=\"default-src 'none'; "
        "img-src data: blob:; media-src data: blob:; "
        "style-src 'unsafe-inline'; "
        "script-src 'unsafe-inline' 'unsafe-eval'; "
        "font-src data:; "
        'connect-src \'none\'; form-action \'none\'; base-uri \'none\'">';

    final head = RegExp(r'<head[^>]*>', caseSensitive: false).firstMatch(html);
    if (head != null) {
      return html.replaceRange(head.end, head.end, csp);
    }
    // No head element: the browser will make one, so put the policy at the top
    // where it still lands inside it.
    return csp + html;
  }

  @override
  Widget build(BuildContext context) {
    return Scaffold(
      backgroundColor: Colors.black,
      appBar: AppBar(
        title: Text(widget.item.name),
        actions: [
          if (_supported)
            IconButton(
              tooltip: 'Restart',
              icon: const Icon(Icons.refresh),
              onPressed: () {
                setState(() => _loading = true);
                _controller?.loadHtmlString(widget.item.content);
              },
            ),
        ],
      ),
      body: !_supported
          ? Center(
              child: Padding(
                padding: const EdgeInsets.all(32),
                child: Text(
                  'Mini-apps run on the phone app.\n\nThis desktop build has no '
                  'web view for them — open it on Android to play.',
                  textAlign: TextAlign.center,
                  style: TextStyle(color: Fleet.ink300, height: 1.5),
                ),
              ),
            )
          : Stack(
              children: [
                WebViewWidget(controller: _controller!),
                if (_loading)
                  const Center(child: CircularProgressIndicator()),
              ],
            ),
    );
  }
}
