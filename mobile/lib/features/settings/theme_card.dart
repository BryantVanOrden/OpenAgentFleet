import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/theme/theme.dart';
import '../../core/theme/theme_controller.dart';

/// Appearance controls.
///
/// Matches the web console exactly — same two axes, same five accents, same
/// palette values — so an operator moving between phone and desktop does not
/// have to relearn what a colour means.
class ThemeCard extends ConsumerWidget {
  const ThemeCard({super.key});

  @override
  Widget build(BuildContext context, WidgetRef ref) {
    final c = FleetColors.of(context);
    final choice = ref.watch(themeControllerProvider);
    final controller = ref.read(themeControllerProvider.notifier);

    // The picker swatches must show the palette that WILL be used, which for
    // "system" means asking the platform rather than guessing dark.
    final effective = switch (choice.mode) {
      ThemeMode.light => Brightness.light,
      ThemeMode.dark => Brightness.dark,
      ThemeMode.system => MediaQuery.platformBrightnessOf(context),
    };

    return Card(
      child: Padding(
        padding: const EdgeInsets.all(16),
        child: Column(
          crossAxisAlignment: CrossAxisAlignment.start,
          children: [
            const Text('Appearance', style: TextStyle(fontWeight: FontWeight.w600)),
            const SizedBox(height: 12),

            SegmentedButton<ThemeMode>(
              segments: const [
                ButtonSegment(
                  value: ThemeMode.light,
                  icon: Icon(Icons.light_mode_outlined, size: 18),
                  label: Text('Light'),
                ),
                ButtonSegment(
                  value: ThemeMode.dark,
                  icon: Icon(Icons.dark_mode_outlined, size: 18),
                  label: Text('Dark'),
                ),
                ButtonSegment(
                  value: ThemeMode.system,
                  icon: Icon(Icons.brightness_auto_outlined, size: 18),
                  label: Text('Auto'),
                ),
              ],
              selected: {choice.mode},
              showSelectedIcon: false,
              onSelectionChanged: (s) => controller.setMode(s.first),
              style: ButtonStyle(
                visualDensity: VisualDensity.compact,
                textStyle: WidgetStateProperty.all(const TextStyle(fontSize: 12)),
              ),
            ),

            const SizedBox(height: 18),
            Text('Accent', style: TextStyle(color: c.ink300, fontSize: 12)),
            const SizedBox(height: 10),

            Row(
              children: [
                for (final accent in FleetAccent.values)
                  Padding(
                    padding: const EdgeInsets.only(right: 12),
                    child: _Swatch(
                      color: accent.swatch(effective),
                      selected: choice.accent == accent,
                      label: accent.label,
                      onTap: () => controller.setAccent(accent),
                    ),
                  ),
              ],
            ),

            const SizedBox(height: 12),
            Text(
              'Auto follows your device. Accent is handy for telling one '
              'deployment from another at a glance.',
              style: TextStyle(color: c.ink400, fontSize: 12, height: 1.4),
            ),
          ],
        ),
      ),
    );
  }
}

class _Swatch extends StatelessWidget {
  const _Swatch({
    required this.color,
    required this.selected,
    required this.label,
    required this.onTap,
  });

  final Color color;
  final bool selected;
  final String label;
  final VoidCallback onTap;

  @override
  Widget build(BuildContext context) {
    final c = FleetColors.of(context);
    return Semantics(
      label: label,
      selected: selected,
      button: true,
      child: GestureDetector(
        onTap: onTap,
        child: AnimatedContainer(
          duration: const Duration(milliseconds: 160),
          width: 36,
          height: 36,
          decoration: BoxDecoration(
            color: color,
            shape: BoxShape.circle,
            border: Border.all(
              color: selected ? c.ink100 : c.ink600,
              width: selected ? 3 : 1,
            ),
          ),
          child: selected
              // Contrast against the swatch itself, not the page — the same
              // tick has to read on deep blue and on bright amber.
              ? Icon(Icons.check,
                  size: 18,
                  color: ThemeData.estimateBrightnessForColor(color) == Brightness.dark
                      ? Colors.white
                      : Colors.black)
              : null,
        ),
      ),
    );
  }
}
