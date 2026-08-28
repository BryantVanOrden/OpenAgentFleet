import 'package:flutter/material.dart';

/// Shared palette with the admin console. A fleet operator switching between the
/// two should not have to relearn what amber means.
class Fleet {
  static const ink950 = Color(0xFF08090C);
  static const ink900 = Color(0xFF0D0F14);
  static const ink850 = Color(0xFF12151C);
  static const ink800 = Color(0xFF171B24);
  static const ink700 = Color(0xFF1F242F);
  static const ink600 = Color(0xFF2B3240);
  static const ink400 = Color(0xFF5B687C);
  static const ink300 = Color(0xFF8A97AB);
  static const ink100 = Color(0xFFE8EDF4);

  static const live = Color(0xFFF5A524);
  static const good = Color(0xFF22C55E);
  static const warn = Color(0xFFF97316);
  static const bad = Color(0xFFEF4444);
  static const cool = Color(0xFF38BDF8);

  /// Colour for an instance or task state, used by badges everywhere.
  static Color forState(String state) => switch (state) {
        'running' || 'succeeded' => good,
        'provisioning' || 'queued' => live,
        'awaiting_human' => warn,
        'paused' => cool,
        'error' || 'failed' => bad,
        _ => ink400,
      };
}

ThemeData buildTheme() {
  const scheme = ColorScheme.dark(
    primary: Fleet.live,
    onPrimary: Fleet.ink950,
    secondary: Fleet.cool,
    surface: Fleet.ink900,
    onSurface: Fleet.ink100,
    error: Fleet.bad,
  );

  return ThemeData(
    useMaterial3: true,
    colorScheme: scheme,
    scaffoldBackgroundColor: Fleet.ink950,
    fontFamily: 'Roboto',
    appBarTheme: const AppBarTheme(
      backgroundColor: Fleet.ink950,
      surfaceTintColor: Colors.transparent,
      elevation: 0,
      centerTitle: false,
      titleTextStyle: TextStyle(
        color: Fleet.ink100,
        fontSize: 18,
        fontWeight: FontWeight.w600,
      ),
    ),
    cardTheme: CardThemeData(
      color: Fleet.ink900,
      surfaceTintColor: Colors.transparent,
      elevation: 0,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(14),
        side: const BorderSide(color: Fleet.ink700),
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: Fleet.ink900,
      contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(12),
        borderSide: const BorderSide(color: Fleet.ink600),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(12),
        borderSide: const BorderSide(color: Fleet.ink600),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(12),
        borderSide: const BorderSide(color: Fleet.live, width: 2),
      ),
      labelStyle: const TextStyle(color: Fleet.ink300),
      hintStyle: const TextStyle(color: Fleet.ink400),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        backgroundColor: Fleet.live,
        foregroundColor: Fleet.ink950,
        minimumSize: const Size(0, 48),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
        textStyle: const TextStyle(fontWeight: FontWeight.w600),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        foregroundColor: Fleet.ink100,
        side: const BorderSide(color: Fleet.ink600),
        minimumSize: const Size(0, 48),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      ),
    ),
    navigationBarTheme: NavigationBarThemeData(
      backgroundColor: Fleet.ink900,
      indicatorColor: Fleet.live.withValues(alpha: 0.15),
      surfaceTintColor: Colors.transparent,
      labelTextStyle: WidgetStateProperty.all(
        const TextStyle(fontSize: 11, color: Fleet.ink300),
      ),
    ),
    dividerTheme: const DividerThemeData(color: Fleet.ink800, space: 1, thickness: 1),
    snackBarTheme: SnackBarThemeData(
      backgroundColor: Fleet.ink800,
      contentTextStyle: const TextStyle(color: Fleet.ink100),
      behavior: SnackBarBehavior.floating,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
    ),
  );
}

/// Small coloured pill used for every state in the app.
class StateChip extends StatelessWidget {
  const StateChip({super.key, required this.state, this.live = false});

  final String state;
  final bool live;

  @override
  Widget build(BuildContext context) {
    final color = Fleet.forState(state);
    return Container(
      padding: const EdgeInsets.symmetric(horizontal: 10, vertical: 4),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.15),
        borderRadius: BorderRadius.circular(999),
        border: Border.all(color: color.withValues(alpha: 0.3)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          _Dot(color: color, pulse: live),
          const SizedBox(width: 6),
          Text(
            state.replaceAll('_', ' '),
            style: TextStyle(color: color, fontSize: 12, fontWeight: FontWeight.w500),
          ),
        ],
      ),
    );
  }
}

class _Dot extends StatefulWidget {
  const _Dot({required this.color, required this.pulse});
  final Color color;
  final bool pulse;

  @override
  State<_Dot> createState() => _DotState();
}

class _DotState extends State<_Dot> with SingleTickerProviderStateMixin {
  late final AnimationController _controller = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 2400),
  );

  @override
  void initState() {
    super.initState();
    if (widget.pulse) _controller.repeat(reverse: true);
  }

  @override
  void didUpdateWidget(covariant _Dot old) {
    super.didUpdateWidget(old);
    if (widget.pulse && !_controller.isAnimating) {
      _controller.repeat(reverse: true);
    } else if (!widget.pulse && _controller.isAnimating) {
      _controller.stop();
      _controller.value = 1;
    }
  }

  @override
  void dispose() {
    _controller.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    return FadeTransition(
      opacity: Tween(begin: 1.0, end: 0.35).animate(_controller),
      child: Container(
        width: 6,
        height: 6,
        decoration: BoxDecoration(color: widget.color, shape: BoxShape.circle),
      ),
    );
  }
}
