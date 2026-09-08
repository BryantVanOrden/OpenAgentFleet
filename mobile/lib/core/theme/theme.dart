import 'package:flutter/material.dart';

/// Theming for the companion app.
///
/// Two axes, matching the web console exactly so an operator moving between the
/// two does not have to relearn what a colour means:
///
///   * brightness — light or dark
///   * accent — amber, red, blue, purple, green
///
/// ## On the static [Fleet] accessors
///
/// [FleetColors] is a real [ThemeExtension] and is the source of truth: it is
/// attached to [ThemeData] and resolves per-subtree like any other theme value.
/// The `Fleet.ink400`-style statics read from the palette that [buildTheme] last
/// installed, which works because this app renders exactly one theme at a time.
///
/// The trade-off is deliberate. Going through `Theme.of(context)` at all 98 call
/// sites would be more idiomatic, but every one of them sits inside a `const`
/// expression today; the statics keep the diff to the theme layer. If this app
/// ever renders two themes at once (a preview pane, say), the extension is
/// already there — switch the call sites and delete the statics.
enum FleetAccent { amber, red, blue, purple, green }

extension FleetAccentInfo on FleetAccent {
  String get label => switch (this) {
        FleetAccent.amber => 'Amber',
        FleetAccent.red => 'Red',
        FleetAccent.blue => 'Blue',
        FleetAccent.purple => 'Purple',
        FleetAccent.green => 'Green',
      };

  /// The colour shown in the picker for a given brightness.
  Color swatch(Brightness b) =>
      b == Brightness.dark ? _accentDark[this]! : _accentLight[this]!;
}

const _accentDark = <FleetAccent, Color>{
  FleetAccent.amber: Color(0xFFF5A524),
  FleetAccent.red: Color(0xFFF87171),
  FleetAccent.blue: Color(0xFF38BDF8),
  FleetAccent.purple: Color(0xFFA78BFA),
  FleetAccent.green: Color(0xFF34D399),
};

const _accentDarkHover = <FleetAccent, Color>{
  FleetAccent.amber: Color(0xFFFFBE4D),
  FleetAccent.red: Color(0xFFFCA5A5),
  FleetAccent.blue: Color(0xFF7DD3FC),
  FleetAccent.purple: Color(0xFFC4B5FD),
  FleetAccent.green: Color(0xFF6EE7B7),
};

// Light-mode accents are deep rather than pastel: they have to carry white text
// when used as a button fill, which a pastel cannot do accessibly.
const _accentLight = <FleetAccent, Color>{
  FleetAccent.amber: Color(0xFFB45309),
  FleetAccent.red: Color(0xFFBE123C),
  FleetAccent.blue: Color(0xFF1D4ED8),
  FleetAccent.purple: Color(0xFF6D28D9),
  FleetAccent.green: Color(0xFF047857),
};

const _accentLightHover = <FleetAccent, Color>{
  FleetAccent.amber: Color(0xFF92400E),
  FleetAccent.red: Color(0xFF9F1239),
  FleetAccent.blue: Color(0xFF1E3A8A),
  FleetAccent.purple: Color(0xFF5B21B6),
  FleetAccent.green: Color(0xFF065F46),
};

/// The full palette for one (brightness, accent) pair.
///
/// The `ink` scale is named for DEPTH, not darkness: `ink950` is always
/// "furthest back" and `ink100` is always "most prominent text". In light mode
/// `ink950` is near-white.
@immutable
class FleetColors extends ThemeExtension<FleetColors> {
  const FleetColors({
    required this.brightness,
    required this.accent,
    required this.ink950,
    required this.ink900,
    required this.ink850,
    required this.ink800,
    required this.ink700,
    required this.ink600,
    required this.ink500,
    required this.ink400,
    required this.ink300,
    required this.ink200,
    required this.ink100,
    required this.live,
    required this.liveHover,
    required this.good,
    required this.warn,
    required this.bad,
    required this.cool,
  });

  final Brightness brightness;
  final FleetAccent accent;

  final Color ink950, ink900, ink850, ink800, ink700, ink600, ink500;
  final Color ink400, ink300, ink200, ink100;
  final Color live, liveHover, good, warn, bad, cool;

  /// Text colour that reads on top of [live]. Works in both modes because
  /// `ink950` is near-black in dark and near-white in light.
  Color get onLive => ink950;

  static FleetColors of(BuildContext context) =>
      Theme.of(context).extension<FleetColors>() ?? _active;

  /// Colour for an instance or task state, used by badges everywhere.
  Color forState(String state) => switch (state) {
        'running' || 'succeeded' => good,
        'provisioning' || 'queued' => live,
        'awaiting_human' => warn,
        // A marathon's step-window closed and the agent carried on in a new
        // task: not a failure, not still running here. Same tone as paused.
        'paused' || 'continued' => cool,
        'error' || 'failed' => bad,
        _ => ink400,
      };

  static FleetColors build(Brightness b, FleetAccent accent) {
    final dark = b == Brightness.dark;
    return FleetColors(
      brightness: b,
      accent: accent,
      ink950: dark ? const Color(0xFF08090C) : const Color(0xFFF4F6F9),
      ink900: dark ? const Color(0xFF0D0F14) : const Color(0xFFFFFFFF),
      ink850: dark ? const Color(0xFF12151C) : const Color(0xFFF7F9FB),
      ink800: dark ? const Color(0xFF171B24) : const Color(0xFFEEF1F6),
      ink700: dark ? const Color(0xFF1F242F) : const Color(0xFFE3E8EF),
      ink600: dark ? const Color(0xFF2B3240) : const Color(0xFFD5DCE5),
      ink500: dark ? const Color(0xFF3D4757) : const Color(0xFFB3BECD),
      ink400: dark ? const Color(0xFF5B687C) : const Color(0xFF97A2B2),
      ink300: dark ? const Color(0xFF8A97AB) : const Color(0xFF667487),
      ink200: dark ? const Color(0xFFC3CCD9) : const Color(0xFF3C4757),
      ink100: dark ? const Color(0xFFE8EDF4) : const Color(0xFF10151D),
      live: dark ? _accentDark[accent]! : _accentLight[accent]!,
      liveHover: dark ? _accentDarkHover[accent]! : _accentLightHover[accent]!,
      good: dark ? const Color(0xFF22C55E) : const Color(0xFF15803D),
      warn: dark ? const Color(0xFFF97316) : const Color(0xFFC2410C),
      bad: dark ? const Color(0xFFEF4444) : const Color(0xFFB91C1C),
      cool: dark ? const Color(0xFF38BDF8) : const Color(0xFF0369A1),
    );
  }

  @override
  FleetColors copyWith({Brightness? brightness, FleetAccent? accent}) =>
      FleetColors.build(brightness ?? this.brightness, accent ?? this.accent);

  @override
  FleetColors lerp(ThemeExtension<FleetColors>? other, double t) {
    // Snapping rather than interpolating: the palettes are discrete, and a
    // half-lerped accent is a colour that exists in no theme.
    if (other is! FleetColors) return this;
    return t < 0.5 ? this : other;
  }
}

/// Palette the static accessors read.
FleetColors _active = FleetColors.build(Brightness.dark, FleetAccent.amber);

/// Point the static accessors at the theme that is actually being rendered.
///
/// This exists because [MaterialApp] evaluates `theme` and `darkTheme` both, so
/// whichever [buildTheme] call ran last would otherwise win — leaving the
/// statics on the dark palette while the app renders light. [FleetThemeSync]
/// calls this from inside the tree, where the resolved theme is known.
void syncActivePalette(FleetColors colors) => _active = colors;

/// Drop-in for `MaterialApp.builder`. Reads the resolved theme and installs it
/// before any descendant builds.
class FleetThemeSync extends StatelessWidget {
  const FleetThemeSync({super.key, required this.child});

  final Widget child;

  @override
  Widget build(BuildContext context) {
    final resolved = Theme.of(context).extension<FleetColors>();
    if (resolved != null) syncActivePalette(resolved);
    return child;
  }
}

/// Static colour accessors. See the class doc on [FleetAccent] for why these
/// exist alongside the [FleetColors] extension.
class Fleet {
  const Fleet._();

  static Color get ink950 => _active.ink950;
  static Color get ink900 => _active.ink900;
  static Color get ink850 => _active.ink850;
  static Color get ink800 => _active.ink800;
  static Color get ink700 => _active.ink700;
  static Color get ink600 => _active.ink600;
  static Color get ink500 => _active.ink500;
  static Color get ink400 => _active.ink400;
  static Color get ink300 => _active.ink300;
  static Color get ink200 => _active.ink200;
  static Color get ink100 => _active.ink100;

  static Color get live => _active.live;
  static Color get liveHover => _active.liveHover;
  static Color get good => _active.good;
  static Color get warn => _active.warn;
  static Color get bad => _active.bad;
  static Color get cool => _active.cool;

  static Color forState(String state) => _active.forState(state);
}

ThemeData buildTheme([
  Brightness brightness = Brightness.dark,
  FleetAccent accent = FleetAccent.amber,
]) {
  final c = FleetColors.build(brightness, accent);
  _active = c;

  final scheme = ColorScheme(
    brightness: brightness,
    primary: c.live,
    onPrimary: c.onLive,
    secondary: c.cool,
    onSecondary: c.onLive,
    surface: c.ink900,
    onSurface: c.ink100,
    error: c.bad,
    onError: c.onLive,
  );

  return ThemeData(
    useMaterial3: true,
    brightness: brightness,
    colorScheme: scheme,
    scaffoldBackgroundColor: c.ink950,
    extensions: [c],
    appBarTheme: AppBarTheme(
      backgroundColor: c.ink950,
      foregroundColor: c.ink100,
      surfaceTintColor: Colors.transparent,
      elevation: 0,
      centerTitle: false,
      titleTextStyle: TextStyle(
        color: c.ink100,
        fontSize: 18,
        fontWeight: FontWeight.w600,
      ),
    ),
    cardTheme: CardThemeData(
      color: c.ink900,
      surfaceTintColor: Colors.transparent,
      elevation: 0,
      margin: EdgeInsets.zero,
      shape: RoundedRectangleBorder(
        borderRadius: BorderRadius.circular(14),
        side: BorderSide(color: c.ink700),
      ),
    ),
    inputDecorationTheme: InputDecorationTheme(
      filled: true,
      fillColor: c.ink900,
      contentPadding: const EdgeInsets.symmetric(horizontal: 14, vertical: 12),
      border: OutlineInputBorder(
        borderRadius: BorderRadius.circular(12),
        borderSide: BorderSide(color: c.ink600),
      ),
      enabledBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(12),
        borderSide: BorderSide(color: c.ink600),
      ),
      focusedBorder: OutlineInputBorder(
        borderRadius: BorderRadius.circular(12),
        borderSide: BorderSide(color: c.live, width: 2),
      ),
      labelStyle: TextStyle(color: c.ink300),
      hintStyle: TextStyle(color: c.ink400),
    ),
    filledButtonTheme: FilledButtonThemeData(
      style: FilledButton.styleFrom(
        backgroundColor: c.live,
        foregroundColor: c.onLive,
        minimumSize: const Size(0, 48),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
        textStyle: const TextStyle(fontWeight: FontWeight.w600),
      ),
    ),
    outlinedButtonTheme: OutlinedButtonThemeData(
      style: OutlinedButton.styleFrom(
        foregroundColor: c.ink100,
        side: BorderSide(color: c.ink600),
        minimumSize: const Size(0, 48),
        shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
      ),
    ),
    textButtonTheme: TextButtonThemeData(
      style: TextButton.styleFrom(foregroundColor: c.live),
    ),
    navigationBarTheme: NavigationBarThemeData(
      backgroundColor: c.ink900,
      indicatorColor: c.live.withValues(alpha: 0.15),
      surfaceTintColor: Colors.transparent,
      labelTextStyle: WidgetStateProperty.all(
        TextStyle(fontSize: 11, color: c.ink300),
      ),
    ),
    popupMenuTheme: PopupMenuThemeData(
      color: c.ink850,
      surfaceTintColor: Colors.transparent,
      shape: RoundedRectangleBorder(borderRadius: BorderRadius.circular(12)),
    ),
    dialogTheme: DialogThemeData(
      backgroundColor: c.ink850,
      surfaceTintColor: Colors.transparent,
    ),
    dividerTheme: DividerThemeData(color: c.ink800, space: 1, thickness: 1),
    progressIndicatorTheme: ProgressIndicatorThemeData(color: c.live),
    snackBarTheme: SnackBarThemeData(
      backgroundColor: c.ink800,
      contentTextStyle: TextStyle(color: c.ink100),
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
    final c = FleetColors.of(context);
    final color = c.forState(state);
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
            style: TextStyle(
                color: color, fontSize: 12, fontWeight: FontWeight.w500),
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
