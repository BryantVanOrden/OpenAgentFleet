import 'package:flutter/gestures.dart';
import 'package:flutter/material.dart';
import 'package:markdown/markdown.dart' as md;
import 'package:url_launcher/url_launcher.dart';

import '../theme/theme.dart';

/// Renders the markdown agents actually write — emphasis, inline code, fenced
/// blocks, lists, headings, links, quotes — as native widgets.
///
/// The parsing is `package:markdown` (the Dart team's parser); only the
/// rendering is ours, which is the point: no HTML, no webview, no style that
/// fights the app theme, and an unknown construct degrades to its plain text
/// instead of leaking tags. Wrap in a [SelectionArea] where copyability
/// matters — the bubbles do.
class MarkdownLite extends StatelessWidget {
  const MarkdownLite(this.data, {super.key, this.baseStyle});

  final String data;
  final TextStyle? baseStyle;

  @override
  Widget build(BuildContext context) {
    final base = baseStyle ?? DefaultTextStyle.of(context).style;
    final doc = md.Document(
      extensionSet: md.ExtensionSet.gitHubFlavored,
      encodeHtml: false,
    );
    final nodes = doc.parse(data.replaceAll('\r\n', '\n'));
    final blocks = <Widget>[];
    for (final node in nodes) {
      final w = _block(context, node, base);
      if (w != null) blocks.add(w);
    }
    if (blocks.isEmpty) return Text(data, style: base);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        for (var i = 0; i < blocks.length; i++) ...[
          if (i > 0) const SizedBox(height: 6),
          blocks[i],
        ],
      ],
    );
  }

  Widget? _block(BuildContext context, md.Node node, TextStyle base) {
    if (node is md.Text) {
      final t = node.text.trim();
      return t.isEmpty ? null : Text(t, style: base);
    }
    final el = node as md.Element;
    switch (el.tag) {
      case 'p':
        return Text.rich(TextSpan(children: _inline(el.children, base)), style: base);
      case 'h1':
      case 'h2':
      case 'h3':
      case 'h4':
      case 'h5':
      case 'h6':
        final level = int.parse(el.tag.substring(1));
        final style = base.copyWith(
          fontWeight: FontWeight.w700,
          fontSize: (base.fontSize ?? 14) + (level <= 2 ? 4 : 2),
        );
        return Padding(
          padding: const EdgeInsets.only(top: 2),
          child: Text.rich(TextSpan(children: _inline(el.children, style)), style: style),
        );
      case 'pre':
        // A fenced block: pre > code > text.
        final code = _plainText(el);
        return Container(
          width: double.infinity,
          padding: const EdgeInsets.all(10),
          decoration: BoxDecoration(
            color: Fleet.ink950,
            borderRadius: BorderRadius.circular(8),
            border: Border.all(color: Fleet.ink700),
          ),
          child: Text(
            code.trimRight(),
            style: base.copyWith(fontFamily: 'monospace', fontSize: (base.fontSize ?? 14) - 1),
          ),
        );
      case 'ul':
      case 'ol':
        return _list(context, el, base, ordered: el.tag == 'ol');
      case 'blockquote':
        return Container(
          padding: const EdgeInsets.only(left: 10),
          decoration: BoxDecoration(
            border: Border(left: BorderSide(color: Fleet.ink600, width: 3)),
          ),
          child: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            mainAxisSize: MainAxisSize.min,
            children: [
              for (final c in el.children ?? const <md.Node>[])
                if (_block(context, c, base.copyWith(color: Fleet.ink300))
                    case final w?)
                  w,
            ],
          ),
        );
      case 'hr':
        return Divider(color: Fleet.ink700, height: 12);
      default:
        // Tables and anything else unmodelled degrade to their words.
        final t = _plainText(el).trim();
        return t.isEmpty ? null : Text(t, style: base);
    }
  }

  Widget _list(BuildContext context, md.Element el, TextStyle base, {required bool ordered}) {
    final items = <Widget>[];
    var n = 0;
    for (final child in el.children ?? const <md.Node>[]) {
      if (child is! md.Element || child.tag != 'li') continue;
      n++;
      items.add(Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          SizedBox(
            width: 22,
            child: Text(ordered ? '$n.' : '•',
                style: base.copyWith(color: Fleet.ink400)),
          ),
          Expanded(
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              mainAxisSize: MainAxisSize.min,
              children: [
                for (final c in child.children ?? const <md.Node>[])
                  if (c is md.Element &&
                      (c.tag == 'ul' || c.tag == 'ol' || c.tag == 'p' || c.tag == 'pre'))
                    _block(context, c, base) ?? const SizedBox.shrink()
                  else
                    Text.rich(TextSpan(children: _inline([c], base)), style: base),
              ],
            ),
          ),
        ],
      ));
    }
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      mainAxisSize: MainAxisSize.min,
      children: [
        for (var i = 0; i < items.length; i++) ...[
          if (i > 0) const SizedBox(height: 2),
          items[i],
        ],
      ],
    );
  }

  List<InlineSpan> _inline(List<md.Node>? nodes, TextStyle style) {
    final out = <InlineSpan>[];
    for (final n in nodes ?? const <md.Node>[]) {
      if (n is md.Text) {
        out.add(TextSpan(text: n.text, style: style));
        continue;
      }
      final el = n as md.Element;
      switch (el.tag) {
        case 'strong':
          out.addAll(_inline(el.children, style.copyWith(fontWeight: FontWeight.w700)));
        case 'em':
          out.addAll(_inline(el.children, style.copyWith(fontStyle: FontStyle.italic)));
        case 'del':
          out.addAll(
              _inline(el.children, style.copyWith(decoration: TextDecoration.lineThrough)));
        case 'code':
          out.add(TextSpan(
            text: ' ${_plainText(el)} ',
            style: style.copyWith(
              fontFamily: 'monospace',
              fontSize: (style.fontSize ?? 14) - 1,
              backgroundColor: Fleet.ink950,
            ),
          ));
        case 'a':
          final href = el.attributes['href'] ?? '';
          out.add(TextSpan(
            text: _plainText(el),
            style: style.copyWith(
              color: Fleet.cool,
              decoration: TextDecoration.underline,
            ),
            recognizer: href.isEmpty
                ? null
                : (TapGestureRecognizer()
                  ..onTap = () => launchUrl(Uri.parse(href),
                      mode: LaunchMode.externalApplication)),
          ));
        case 'br':
          out.add(TextSpan(text: '\n', style: style));
        default:
          out.addAll(_inline(el.children, style));
      }
    }
    return out;
  }

  String _plainText(md.Node node) {
    if (node is md.Text) return node.text;
    final el = node as md.Element;
    return (el.children ?? const <md.Node>[]).map(_plainText).join();
  }
}
