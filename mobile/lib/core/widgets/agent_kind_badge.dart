import 'package:flutter/material.dart';

import '../models.dart';
import '../theme/theme.dart';

/// The icon people will recognise each kind of agent by.
IconData agentKindIcon(String kind) => switch (AgentKind.normalise(kind)) {
      AgentKind.desktop => Icons.desktop_windows_outlined,
      AgentKind.claudeCode => Icons.code_rounded,
      AgentKind.codex => Icons.terminal_rounded,
      AgentKind.hermes => Icons.bolt_rounded,
      AgentKind.openClaw => Icons.hub_outlined,
      AgentKind.webhook => Icons.webhook_outlined,
      _ => Icons.smart_toy_outlined,
    };

/// A colour per kind, from the theme, so a mixed fleet is legible at a glance
/// in either brightness.
Color agentKindColor(String kind) => switch (AgentKind.normalise(kind)) {
      AgentKind.desktop => Fleet.ink300,
      AgentKind.claudeCode => Fleet.live,
      AgentKind.codex => Fleet.cool,
      AgentKind.hermes => Fleet.good,
      AgentKind.openClaw => Fleet.warn,
      _ => Fleet.ink200,
    };

/// Small pill naming how an agent runs: Desktop, Claude Code, Codex, Hermes,
/// OpenClaw or Webhook.
class AgentKindBadge extends StatelessWidget {
  const AgentKindBadge(this.kind, {super.key, this.dense = false});

  final String kind;

  /// Icon and a smaller label, for tight spots like a chart card.
  final bool dense;

  @override
  Widget build(BuildContext context) {
    final color = agentKindColor(kind);
    return Container(
      padding: EdgeInsets.symmetric(horizontal: dense ? 6 : 8, vertical: 2),
      decoration: BoxDecoration(
        color: color.withValues(alpha: 0.12),
        borderRadius: BorderRadius.circular(999),
        border: Border.all(color: color.withValues(alpha: 0.35)),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(agentKindIcon(kind), size: dense ? 11 : 12, color: color),
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
    );
  }
}
