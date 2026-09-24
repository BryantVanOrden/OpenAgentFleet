import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../core/theme/theme.dart';

/// Settings → "Support this project".
///
/// The same donation details as the README's support section, including its
/// QR codes (copies of docs/assets/{xrp,btc}_qr.png). Deliberately quiet: one
/// card near the bottom of Settings, no badge, no prompt, nothing that
/// reappears.
class SupportCard extends StatelessWidget {
  const SupportCard({super.key});

  static const intro =
      'OpenAgentFleet is free and open source. If it is useful to you, you can '
      'support development with XRP or Bitcoin.';

  static const xrpAddress = 'rf82s1CDagppvM6ATqc1nSrL6GackzHJrm';
  static const xrpTag = '796343731';
  static const xrpWarning =
      'A destination tag is required when sending XRP to this address '
      '(796343731). Without it the transfer will not be credited.';
  static const btcAddress = 'bc1qvre807vxh08puxwc2z5adnm59tta7v5mqmky45';

  /// At or above this width the two coins sit side by side.
  static const wideBreakpoint = 700.0;

  @override
  Widget build(BuildContext context) {
    final c = FleetColors.of(context);
    final wide = MediaQuery.sizeOf(context).width >= wideBreakpoint;

    const xrp = _CoinPanel(
      title: 'XRP (Ripple)',
      qrAsset: 'assets/support/xrp_qr.png',
      qrLabel: 'QR code for the XRP address, including the destination tag',
      address: xrpAddress,
      copyAddressLabel: 'Copy XRP address',
      warning: xrpWarning,
      tag: xrpTag,
      copyTagLabel: 'Copy XRP destination tag',
    );
    const btc = _CoinPanel(
      title: 'Bitcoin (BTC)',
      qrAsset: 'assets/support/btc_qr.png',
      qrLabel: 'QR code for the Bitcoin address',
      address: btcAddress,
      copyAddressLabel: 'Copy Bitcoin address',
      note: 'No memo required.',
    );

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('Support this project',
                style: TextStyle(fontWeight: FontWeight.w600)),
            const SizedBox(height: 8),
            Text(
              intro,
              style: TextStyle(color: c.ink400, fontSize: 12, height: 1.4),
            ),
            const SizedBox(height: 14),
            if (wide)
              const IntrinsicHeight(
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.stretch,
                  children: [
                    Expanded(child: xrp),
                    SizedBox(width: 12),
                    Expanded(child: btc),
                  ],
                ),
              )
            else
              const Column(
                crossAxisAlignment: CrossAxisAlignment.stretch,
                children: [xrp, SizedBox(height: 12), btc],
              ),
          ],
        ),
      ),
    );
  }
}

class _CoinPanel extends StatelessWidget {
  const _CoinPanel({
    required this.title,
    required this.qrAsset,
    required this.qrLabel,
    required this.address,
    required this.copyAddressLabel,
    this.warning,
    this.tag,
    this.copyTagLabel,
    this.note,
  });

  final String title;
  final String qrAsset;
  final String qrLabel;
  final String address;
  final String copyAddressLabel;
  final String? warning;
  final String? tag;
  final String? copyTagLabel;
  final String? note;

  @override
  Widget build(BuildContext context) {
    final c = FleetColors.of(context);
    return Container(
      padding: const EdgeInsets.all(14),
      decoration: BoxDecoration(
        color: c.ink850,
        borderRadius: BorderRadius.circular(12),
        border: Border.all(color: c.ink700),
      ),
      child: Column(
        crossAxisAlignment: CrossAxisAlignment.stretch,
        children: [
          Text(title,
              style:
                  const TextStyle(fontSize: 13, fontWeight: FontWeight.w600)),
          if (warning != null) ...[
            const SizedBox(height: 10),
            _Warning(text: warning!),
          ],
          const SizedBox(height: 14),
          Center(child: _Qr(asset: qrAsset, label: qrLabel)),
          const SizedBox(height: 14),
          _CopyField(
            label: 'Address',
            value: address,
            copyLabel: copyAddressLabel,
          ),
          if (tag != null) ...[
            const SizedBox(height: 10),
            _CopyField(
              label: 'Destination tag / memo (required)',
              value: tag!,
              copyLabel: copyTagLabel ?? 'Copy destination tag',
            ),
          ],
          if (note != null) ...[
            const SizedBox(height: 10),
            Text(note!,
                style: TextStyle(color: c.ink400, fontSize: 12, height: 1.4)),
          ],
        ],
      ),
    );
  }
}

/// The QR on a white tile in either theme: the codes are dark-on-light with no
/// quiet zone of their own, and a scanner will not read them inverted.
class _Qr extends StatelessWidget {
  const _Qr({required this.asset, required this.label});

  final String asset;
  final String label;

  @override
  Widget build(BuildContext context) {
    final c = FleetColors.of(context);
    return Semantics(
      image: true,
      label: label,
      child: Container(
        padding: const EdgeInsets.all(16),
        decoration: BoxDecoration(
          color: Colors.white,
          borderRadius: BorderRadius.circular(10),
          border: Border.all(color: c.ink700),
        ),
        // 250px masters; nearest-neighbour keeps the module edges hard at any
        // scale, where smoothing would blur them into grey.
        child: Image.asset(
          asset,
          width: 176,
          height: 176,
          filterQuality: FilterQuality.none,
          isAntiAlias: false,
          excludeFromSemantics: true,
        ),
      ),
    );
  }
}

class _Warning extends StatelessWidget {
  const _Warning({required this.text});

  final String text;

  @override
  Widget build(BuildContext context) {
    final c = FleetColors.of(context);
    return Container(
      padding: const EdgeInsets.fromLTRB(10, 8, 10, 8),
      decoration: BoxDecoration(
        color: c.warn.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(8),
        border: Border.all(color: c.warn.withValues(alpha: 0.45)),
      ),
      child: Row(
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          Icon(Icons.warning_amber_rounded, size: 18, color: c.warn),
          const SizedBox(width: 8),
          Expanded(
            child: Text(
              text,
              style: TextStyle(
                color: c.ink100,
                fontSize: 12,
                height: 1.4,
                fontWeight: FontWeight.w500,
              ),
            ),
          ),
        ],
      ),
    );
  }
}

/// A label, a selectable monospace value that wraps, and a copy button.
class _CopyField extends StatelessWidget {
  const _CopyField({
    required this.label,
    required this.value,
    required this.copyLabel,
  });

  final String label;
  final String value;
  final String copyLabel;

  Future<void> _copy(BuildContext context) async {
    // A browser can refuse clipboard access; say so rather than claim a copy
    // that did not happen. The value stays selectable either way.
    var ok = true;
    try {
      await Clipboard.setData(ClipboardData(text: value));
    } catch (_) {
      ok = false;
    }
    if (!context.mounted) return;
    ScaffoldMessenger.of(context)
      ..hideCurrentSnackBar()
      ..showSnackBar(SnackBar(
        content: Text(ok ? 'Copied' : 'Could not copy. Select the text instead.'),
      ));
  }

  @override
  Widget build(BuildContext context) {
    final c = FleetColors.of(context);
    return Column(
      crossAxisAlignment: CrossAxisAlignment.start,
      children: [
        Text(label, style: TextStyle(color: c.ink300, fontSize: 12)),
        const SizedBox(height: 4),
        Container(
          padding: const EdgeInsets.fromLTRB(10, 2, 2, 2),
          decoration: BoxDecoration(
            color: c.ink950,
            borderRadius: BorderRadius.circular(8),
            border: Border.all(color: c.ink700),
          ),
          child: Row(
            children: [
              Expanded(
                child: SelectableText(
                  value,
                  style: TextStyle(
                    color: c.ink100,
                    fontFamily: 'monospace',
                    fontSize: 13,
                    height: 1.4,
                  ),
                ),
              ),
              // The label rides on the icon so a screen reader announces
              // "Copy XRP address, button" once; the hover tooltip says the
              // same thing and is kept out of the semantics tree.
              Tooltip(
                message: copyLabel,
                excludeFromSemantics: true,
                child: IconButton(
                  onPressed: () => _copy(context),
                  icon: Icon(Icons.copy_rounded,
                      size: 18, color: c.ink300, semanticLabel: copyLabel),
                ),
              ),
            ],
          ),
        ),
      ],
    );
  }
}
