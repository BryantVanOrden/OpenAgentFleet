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
      ..loadHtmlString(widget.item.content);
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
