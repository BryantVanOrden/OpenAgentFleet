import 'package:flutter/material.dart';
import 'package:flutter_svg/flutter_svg.dart';

import '../models.dart';
import '../theme/theme.dart';

/// The generic icon for a kind. The products with a mark of their own show
/// it through [AgentKindIcon]; this is what the others (a desktop, a
/// webhook) wear, and the fallback anywhere an [IconData] is required.
IconData agentKindIcon(String kind) => switch (AgentKind.normalise(kind)) {
      AgentKind.desktop => Icons.desktop_windows_outlined,
      AgentKind.claudeCode => Icons.code_rounded,
      AgentKind.codex => Icons.terminal_rounded,
      AgentKind.hermes => Icons.bolt_rounded,
      AgentKind.openClaw => Icons.hub_outlined,
      AgentKind.webhook => Icons.webhook_outlined,
      _ => Icons.smart_toy_outlined,
    };

/// The product's own mark, bundled under assets/agents/. Null for kinds
/// that are not a product: a desktop, a webhook.
String? agentKindLogo(String kind) => switch (AgentKind.normalise(kind)) {
      AgentKind.claudeCode => 'assets/agents/claude.svg',
      AgentKind.codex => 'assets/agents/codex.svg',
      AgentKind.hermes => 'assets/agents/hermes.svg',
      AgentKind.openClaw => 'assets/agents/openclaw.svg',
      _ => null,
    };

bool get _dark => Fleet.ink950.computeLuminance() < 0.2;

/// A colour per kind, taken from each product's own mark where it has one
/// and toned for contrast on either brightness, so a mixed fleet is legible
/// at a glance and the label matches the logo beside it.
Color agentKindColor(String kind) => switch (AgentKind.normalise(kind)) {
      AgentKind.desktop => Fleet.ink300,
      // Claude's terracotta, #D97757.
      AgentKind.claudeCode =>
        _dark ? const Color(0xFFE38B6D) : const Color(0xFFB4532F),
      // The Codex mark's violet-to-blue.
      AgentKind.codex =>
        _dark ? const Color(0xFF9FA8FF) : const Color(0xFF4148D8),
      // Hermes's mark is one colour, the text's.
      AgentKind.hermes => Fleet.ink200,
      // The OpenClaw lobster.
      AgentKind.openClaw =>
        _dark ? const Color(0xFFFF7070) : const Color(0xFFB42318),
      _ => Fleet.ink200,
    };

/// A kind's mark: the product's logo for Claude Code, Codex, Hermes and
/// OpenClaw, the generic icon for a desktop or a webhook. Labelled with the
/// product's name for screen readers.
class AgentKindIcon extends StatelessWidget {
  const AgentKindIcon(this.kind,
      {super.key, this.size = 18, this.color, this.semantic = true});

  final String kind;
  final double size;

  /// For the generic icons, and for Hermes's one-colour mark. The other
  /// logos keep their own colours.
  final Color? color;

  /// Off where the product's name is already read out beside it, so a
  /// badge is not announced twice.
  final bool semantic;

  @override
  Widget build(BuildContext context) {
    final logo = agentKindLogo(kind);
    final label = AgentKind.label(kind);
    if (logo == null) {
      return Icon(agentKindIcon(kind),
          size: size,
          color: color ?? agentKindColor(kind),
          semanticLabel: semantic ? label : null);
    }
    final kindN = AgentKind.normalise(kind);
    Widget mark = SvgPicture.asset(
      logo,
      width: size,
      height: size,
      fit: BoxFit.contain,
      colorFilter: kindN == AgentKind.hermes
          ? ColorFilter.mode(color ?? Fleet.ink100, BlendMode.srcIn)
          : null,
      semanticsLabel: semantic ? label : null,
      excludeFromSemantics: !semantic,
      placeholderBuilder: (_) => Icon(agentKindIcon(kind),
          size: size, color: color ?? agentKindColor(kind)),
    );
    // The Codex mark is a violet-to-deep-blue gradient whose dark end sinks
    // into a dark surface; a light disc behind it keeps its outline.
    if (kindN == AgentKind.codex && _dark) {
      mark = Container(
        width: size,
        height: size,
        padding: EdgeInsets.all(size * 0.08),
        decoration: const BoxDecoration(
          color: Color(0xFFEDEFFF),
          shape: BoxShape.circle,
        ),
        child: mark,
      );
    }
    return mark;
  }
}

/// Small pill naming how an agent runs: Desktop, Claude Code, Codex, Hermes,
/// OpenClaw or Webhook, with the product's mark.
class AgentKindBadge extends StatelessWidget {
  const AgentKindBadge(this.kind, {super.key, this.dense = false});

  final String kind;

  /// Icon and a smaller label, for tight spots like a chart card.
  final bool dense;

  @override
  Widget build(BuildContext context) {
    final color = agentKindColor(kind);
    return Semantics(
      label: AgentKind.label(kind),
      excludeSemantics: true,
      child: Container(
        padding: EdgeInsets.fromLTRB(dense ? 5 : 6, 2, dense ? 7 : 9, 2),
        decoration: BoxDecoration(
          color: color.withValues(alpha: 0.12),
          borderRadius: BorderRadius.circular(999),
          border: Border.all(color: color.withValues(alpha: 0.35)),
        ),
        child: Row(
          mainAxisSize: MainAxisSize.min,
          children: [
            AgentKindIcon(kind,
                size: dense ? 12 : 14, color: color, semantic: false),
            const SizedBox(width: 4),
            Text(
              AgentKind.label(kind),
              style: TextStyle(
                color: color,
                fontSize: dense ? 10 : 11,
                fontWeight: FontWeight.w600,
              ),
            ),
          ],
        ),
      ),
    );
  }
}
