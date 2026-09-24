import 'dart:math' as math;

import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/agent_kind_badge.dart';
import '../../core/widgets/thinking.dart';
import '../dashboard/add_agent_sheet.dart';
import 'agent_profile_sheet.dart';
import 'org_layout.dart';

const _cardWidth = 196.0;
const _cardHeight = 132.0;

/// Who reports to whom, drawn as a chart.
///
/// You are at the top; every agent hangs from its manager. Work escalates up
/// these lines and is delegated down them, so the shape is not decoration.
/// Tap a card to edit where it sits and what it is for; press and hold one
/// and drop it on another to move it under that agent.
class OrgChartScreen extends ConsumerStatefulWidget {
  const OrgChartScreen({super.key});

  @override
  ConsumerState<OrgChartScreen> createState() => _OrgChartScreenState();
}

class _OrgChartScreenState extends ConsumerState<OrgChartScreen> {
  final _transform = TransformationController();

  /// The chart size and viewport the current transform was fitted to. A new
  /// agent that changes the chart's width re-fits; a refresh that changes
  /// nothing leaves the operator's pan and zoom alone.
  Size? _fittedChart;
  Size? _fittedViewport;

  /// The card a dragged card is hovering over, for the drop highlight.
  String? _dropTarget;

  /// Where "You" sits horizontally in the current layout, to centre on.
  double? _rootCenterX;

  @override
  void dispose() {
    _transform.dispose();
    super.dispose();
  }

  /// Scale the chart to the screen's width (never above 1, never so small the
  /// cards are unreadable) and centre it.
  ///
  /// Called from inside the LayoutBuilder, which is the only place the
  /// viewport is known. The first fit happens before the InteractiveViewer
  /// exists, so it is set directly; a later one would notify a viewer that is
  /// mid-build, so it waits for the frame to finish.
  void _fit(Size chart, Size viewport, {bool force = false}) {
    if (!force && _fittedChart == chart && _fittedViewport == viewport) return;
    final first = _fittedChart == null;
    _fittedChart = chart;
    _fittedViewport = viewport;
    // Opening: never so small the cards cannot be read (a phone showing a
    // wide chart starts on you, centred, and pans). The fit button shows the
    // whole chart however small that makes it.
    final whole = math.min(1.0, viewport.width / chart.width);
    final scale = force ? whole.clamp(0.3, 1.0) : whole.clamp(0.75, 1.0);
    final rootCenter = _rootCenterX ?? chart.width / 2;
    final dx = chart.width * scale <= viewport.width
        ? (viewport.width - chart.width * scale) / 2
        : viewport.width / 2 - rootCenter * scale;
    final m = Matrix4.identity()
      ..translateByDouble(dx, 0, 0, 1)
      ..scaleByDouble(scale, scale, 1, 1);
    if (first || force) {
      _transform.value = m;
    } else {
      WidgetsBinding.instance.addPostFrameCallback((_) {
        if (mounted) _transform.value = m;
      });
    }
  }

  Future<void> _reparent(OrgNode agent, String managerId, String managerName) async {
    final messenger = ScaffoldMessenger.of(context);
    try {
      await ref
          .read(apiProvider)
          .setInstanceProfile(agent.id, reportsTo: managerId);
      ref.invalidate(orgProvider);
      ref.invalidate(instancesProvider);
      messenger.showSnackBar(SnackBar(
        content: Text('${agent.name} now reports to $managerName'),
      ));
    } catch (err) {
      messenger.showSnackBar(SnackBar(content: Text('$err')));
    }
  }

  Future<void> _addAgent() async {
    final created = await AddAgentSheet.show(context);
    if (created == true) {
      ref.invalidate(orgProvider);
      ref.invalidate(instancesProvider);
    }
  }

  @override
  Widget build(BuildContext context) {
    final org = ref.watch(orgProvider);
    final instances =
        ref.watch(instancesProvider).valueOrNull ?? const <Instance>[];

    return Scaffold(
      appBar: AppBar(
        title: const Text('Org chart'),
        actions: [
          IconButton(
            tooltip: 'Fit to screen',
            icon: const Icon(Icons.fit_screen_outlined),
            onPressed: () {
              final chart = _fittedChart;
              final viewport = _fittedViewport;
              if (chart != null && viewport != null) {
                _fit(chart, viewport, force: true);
              }
            },
          ),
          IconButton(
            tooltip: 'Refresh',
            icon: const Icon(Icons.refresh),
            onPressed: () => ref.invalidate(orgProvider),
          ),
        ],
      ),
      floatingActionButton: FloatingActionButton.extended(
        onPressed: _addAgent,
        icon: const Icon(Icons.person_add_alt_outlined),
        label: const Text('Add agent'),
      ),
      body: org.when(
        loading: () => const Center(child: CircularProgressIndicator()),
        error: (err, _) => _Message(
          icon: Icons.cloud_off_outlined,
          title: 'Could not load the org chart',
          body: '$err',
          action: OutlinedButton(
            onPressed: () => ref.invalidate(orgProvider),
            child: const Text('Retry'),
          ),
        ),
        data: (chart) {
          if (chart.nodes.isEmpty) {
            return _Message(
              icon: Icons.account_tree_outlined,
              title: 'Nobody reports to you yet',
              body: 'Add an agent and it appears here, under you. Agents you '
                  'add later can report to it.',
              action: FilledButton.icon(
                onPressed: _addAgent,
                icon: const Icon(Icons.person_add_alt_outlined),
                label: const Text('Add agent'),
              ),
            );
          }
          return _chart(chart, instances);
        },
      ),
    );
  }

  Widget _chart(OrgChart chart, List<Instance> instances) {
    final inputs = [
      for (final n in chart.nodes)
        OrgLayoutInput(id: n.id, parentId: n.reportsTo, sortKey: n.name),
    ];
    final layout = layoutOrgTree(
      inputs,
      cardWidth: _cardWidth,
      cardHeight: _cardHeight,
    );
    final byId = {for (final n in chart.nodes) n.id: n};
    final instanceById = {for (final i in instances) i.id: i};

    return LayoutBuilder(builder: (context, constraints) {
      final viewport = Size(constraints.maxWidth, constraints.maxHeight);
      final size = Size(layout.width, layout.height);
      _rootCenterX = layout.boxes[orgRootId]?.centerX;
      _fit(size, viewport);

      return InteractiveViewer(
        transformationController: _transform,
        constrained: false,
        minScale: 0.3,
        maxScale: 2.5,
        boundaryMargin: const EdgeInsets.all(240),
        child: SizedBox(
          width: layout.width,
          height: layout.height + 80,
          child: Stack(
            clipBehavior: Clip.none,
            children: [
              Positioned.fill(
                child: CustomPaint(
                  painter: OrgConnectorPainter(
                    connectors: layout.connectors,
                    color: Fleet.ink500,
                  ),
                ),
              ),
              for (final box in layout.boxes.values)
                Positioned(
                  left: box.x,
                  top: box.y,
                  width: box.width,
                  height: box.height,
                  child: box.id == orgRootId
                      ? _dropZone(
                          id: orgRootId,
                          name: 'you',
                          inputs: inputs,
                          byId: byId,
                          child: _YouCard(highlight: _dropTarget == orgRootId),
                        )
                      : _draggableCard(
                          byId[box.id]!,
                          instanceById[box.id],
                          inputs,
                          byId,
                        ),
                ),
            ],
          ),
        ),
      );
    });
  }

  Widget _draggableCard(
    OrgNode node,
    Instance? instance,
    List<OrgLayoutInput> inputs,
    Map<String, OrgNode> byId,
  ) {
    final card = OrgAgentCard(
      node: node,
      warnPct: instance?.budgetWarnPct ?? 0,
      highlight: _dropTarget == node.id,
      onTap: () => AgentProfileSheet.show(context, node.id),
    );
    return _dropZone(
      id: node.id,
      name: node.name,
      inputs: inputs,
      byId: byId,
      child: LongPressDraggable<String>(
        data: node.id,
        feedback: Material(
          color: Colors.transparent,
          child: SizedBox(
            width: _cardWidth,
            height: _cardHeight,
            child: Opacity(
              opacity: 0.9,
              child: OrgAgentCard(node: node, warnPct: 0, lifted: true),
            ),
          ),
        ),
        childWhenDragging: Opacity(opacity: 0.35, child: card),
        child: card,
      ),
    );
  }

  /// Anything a card can be dropped on: another agent, or you.
  Widget _dropZone({
    required String id,
    required String name,
    required List<OrgLayoutInput> inputs,
    required Map<String, OrgNode> byId,
    required Widget child,
  }) {
    return DragTarget<String>(
      onWillAcceptWithDetails: (d) {
        final agent = byId[d.data];
        final ok = agent != null &&
            d.data != id &&
            agent.reportsTo != id &&
            !wouldCreateCycle(inputs, d.data, id);
        if (ok) setState(() => _dropTarget = id);
        return ok;
      },
      onLeave: (_) => setState(() => _dropTarget = null),
      onAcceptWithDetails: (d) {
        setState(() => _dropTarget = null);
        final agent = byId[d.data];
        if (agent != null) _reparent(agent, id, name);
      },
      builder: (context, _, __) => child,
    );
  }
}

/// The elbow connectors between managers and reports.
class OrgConnectorPainter extends CustomPainter {
  OrgConnectorPainter({required this.connectors, required this.color});

  final List<OrgConnector> connectors;
  final Color color;

  @override
  void paint(Canvas canvas, Size size) {
    final paint = Paint()
      ..color = color
      ..style = PaintingStyle.stroke
      ..strokeWidth = 1.4
      ..strokeCap = StrokeCap.round;

    for (final c in connectors) {
      final p = c.points;
      final path = Path()..moveTo(p[0].x, p[0].y);
      final dx = p[2].x - p[1].x;
      if (dx.abs() < 0.5) {
        path.lineTo(p[3].x, p[3].y);
      } else {
        final dir = dx.sign;
        final r = math.min(
          8.0,
          math.min(dx.abs() / 2, math.min(p[1].y - p[0].y, p[3].y - p[2].y)),
        );
        path
          ..lineTo(p[1].x, p[1].y - r)
          ..quadraticBezierTo(p[1].x, p[1].y, p[1].x + dir * r, p[1].y)
          ..lineTo(p[2].x - dir * r, p[2].y)
          ..quadraticBezierTo(p[2].x, p[2].y, p[2].x, p[2].y + r)
          ..lineTo(p[3].x, p[3].y);
      }
      canvas.drawPath(path, paint);
    }
  }

  @override
  bool shouldRepaint(covariant OrgConnectorPainter old) =>
      old.connectors != connectors || old.color != color;
}

class _YouCard extends StatelessWidget {
  const _YouCard({this.highlight = false});
  final bool highlight;

  @override
  Widget build(BuildContext context) {
    return Container(
      alignment: Alignment.center,
      decoration: BoxDecoration(
        color: Fleet.live.withValues(alpha: highlight ? 0.28 : 0.14),
        borderRadius: BorderRadius.circular(999),
        border: Border.all(
          color: Fleet.live.withValues(alpha: highlight ? 1 : 0.5),
          width: highlight ? 2 : 1,
        ),
      ),
      child: Row(
        mainAxisSize: MainAxisSize.min,
        children: [
          Icon(Icons.person_rounded, size: 18, color: Fleet.live),
          const SizedBox(width: 6),
          Text('You',
              style: TextStyle(
                  color: Fleet.ink100,
                  fontSize: 15,
                  fontWeight: FontWeight.w700)),
        ],
      ),
    );
  }
}

/// One agent's card in the chart.
class OrgAgentCard extends StatelessWidget {
  const OrgAgentCard({
    super.key,
    required this.node,
    required this.warnPct,
    this.highlight = false,
    this.lifted = false,
    this.onTap,
  });

  final OrgNode node;

  /// The agent's warning percentage; 0 is the server default of 80.
  final int warnPct;

  /// Something is being dragged over this card and may be dropped here.
  final bool highlight;

  /// Drawn as the drag feedback.
  final bool lifted;
  final VoidCallback? onTap;

  @override
  Widget build(BuildContext context) {
    final status = orgStatus(
      online: node.online,
      busy: node.busy,
      hold: node.hold,
      ticketRef: node.ticketRef,
    );
    final statusColor = switch (status.activity) {
      OrgActivity.working => Fleet.good,
      OrgActivity.idle => Fleet.ink300,
      OrgActivity.held => Fleet.warn,
      OrgActivity.offline => Fleet.ink400,
    };
    final kindColor = agentKindColor(node.kind);

    Widget avatar = Container(
      width: 34,
      height: 34,
      alignment: Alignment.center,
      decoration: BoxDecoration(
        color: kindColor.withValues(alpha: 0.16),
        shape: BoxShape.circle,
        border: Border.all(color: kindColor.withValues(alpha: 0.45)),
      ),
      child: Text(
        initialsOf(node.name),
        style: TextStyle(
            color: Fleet.ink100, fontSize: 12.5, fontWeight: FontWeight.w700),
      ),
    );
    if (status.activity == OrgActivity.working && !lifted) {
      avatar = BreathingAvatar(color: Fleet.good, child: avatar);
    }

    return Semantics(
      button: onTap != null,
      label: '${node.name}, ${node.title.isEmpty ? '' : '${node.title}, '}'
          '${AgentKind.label(node.kind)}, ${status.label}',
      child: Material(
        color: Fleet.ink900,
        elevation: lifted ? 8 : 0,
        shadowColor: Colors.black54,
        shape: RoundedRectangleBorder(
          borderRadius: BorderRadius.circular(14),
          side: BorderSide(
            color: highlight ? Fleet.live : Fleet.ink700,
            width: highlight ? 2 : 1,
          ),
        ),
        child: InkWell(
          borderRadius: BorderRadius.circular(14),
          onTap: onTap,
          child: Padding(
            padding: const EdgeInsets.fromLTRB(10, 10, 10, 9),
            child: Column(
              crossAxisAlignment: CrossAxisAlignment.start,
              children: [
                Row(
                  children: [
                    avatar,
                    const SizedBox(width: 8),
                    Expanded(
                      child: Column(
                        crossAxisAlignment: CrossAxisAlignment.start,
                        children: [
                          Text(node.name,
                              maxLines: 1,
                              overflow: TextOverflow.ellipsis,
                              style: TextStyle(
                                  color: Fleet.ink100,
                                  fontSize: 13.5,
                                  fontWeight: FontWeight.w700)),
                          Text(
                            node.title.isEmpty ? 'No title yet' : node.title,
                            maxLines: 1,
                            overflow: TextOverflow.ellipsis,
                            style: TextStyle(
                              color: node.title.isEmpty
                                  ? Fleet.ink500
                                  : Fleet.ink300,
                              fontSize: 11,
                              fontStyle: node.title.isEmpty
                                  ? FontStyle.italic
                                  : FontStyle.normal,
                            ),
                          ),
                        ],
                      ),
                    ),
                  ],
                ),
                const SizedBox(height: 7),
                Row(
                  children: [
                    AgentKindBadge(node.kind, dense: true),
                    const Spacer(),
                    if (node.trust == 'low') ...[
                      Tooltip(
                        message: 'Low trust',
                        child: Icon(Icons.shield_outlined,
                            size: 13, color: Fleet.warn),
                      ),
                      const SizedBox(width: 4),
                    ],
                    Icon(Icons.confirmation_number_outlined,
                        size: 12, color: Fleet.ink400),
                    const SizedBox(width: 3),
                    Text('${node.openTickets} open',
                        style: TextStyle(color: Fleet.ink300, fontSize: 10.5)),
                  ],
                ),
                const SizedBox(height: 7),
                Row(
                  children: [
                    Container(
                      width: 7,
                      height: 7,
                      decoration: BoxDecoration(
                          color: statusColor, shape: BoxShape.circle),
                    ),
                    const SizedBox(width: 6),
                    Expanded(
                      child: Text(
                        status.label,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(
                            color: statusColor,
                            fontSize: 11.5,
                            fontWeight: FontWeight.w600),
                      ),
                    ),
                  ],
                ),
                // Room for the ticket's title only when there is no budget
                // bar to show: the card is a fixed size for the layout.
                if (node.busy && node.ticketTitle.isNotEmpty && !node.hasBudget)
                  Padding(
                    padding: const EdgeInsets.only(left: 13, top: 2),
                    child: Text(node.ticketTitle,
                        maxLines: 1,
                        overflow: TextOverflow.ellipsis,
                        style: TextStyle(color: Fleet.ink400, fontSize: 10.5)),
                  ),
                const Spacer(),
                if (node.hasBudget)
                  _BudgetBar(
                    spend: node.spendMonthUsd,
                    budget: node.budgetMonthUsd,
                    warnPct: warnPct,
                  ),
              ],
            ),
          ),
        ),
      ),
    );
  }
}

/// Month spend against the ceiling.
class _BudgetBar extends StatelessWidget {
  const _BudgetBar({
    required this.spend,
    required this.budget,
    required this.warnPct,
  });

  final double spend;
  final double budget;
  final int warnPct;

  @override
  Widget build(BuildContext context) {
    final fraction = budget > 0 ? (spend / budget).clamp(0.0, 1.0) : 0.0;
    final warnAt = (warnPct > 0 ? warnPct : 80) / 100;
    final color = fraction >= 1
        ? Fleet.bad
        : fraction >= warnAt
            ? Fleet.warn
            : Fleet.good;
    return Column(
      crossAxisAlignment: CrossAxisAlignment.stretch,
      children: [
        Text(
          '\$${spend.toStringAsFixed(2)} of \$${budget.toStringAsFixed(budget >= 100 ? 0 : 2)} this month',
          maxLines: 1,
          overflow: TextOverflow.ellipsis,
          style: TextStyle(
            color: Fleet.ink400,
            fontSize: 10,
            fontFeatures: const [FontFeature.tabularFigures()],
          ),
        ),
        const SizedBox(height: 3),
        ClipRRect(
          borderRadius: BorderRadius.circular(999),
          child: LinearProgressIndicator(
            value: fraction,
            minHeight: 4,
            backgroundColor: Fleet.ink800,
            valueColor: AlwaysStoppedAnimation(color),
          ),
        ),
      ],
    );
  }
}

class _Message extends StatelessWidget {
  const _Message({
    required this.icon,
    required this.title,
    required this.body,
    this.action,
  });

  final IconData icon;
  final String title;
  final String body;
  final Widget? action;

  @override
  Widget build(BuildContext context) {
    return Center(
      child: Padding(
        padding: const EdgeInsets.all(32),
        child: Column(
          mainAxisSize: MainAxisSize.min,
          children: [
            Icon(icon, size: 46, color: Fleet.ink500),
            const SizedBox(height: 14),
            Text(title,
                textAlign: TextAlign.center,
                style: const TextStyle(fontSize: 16)),
            const SizedBox(height: 6),
            Text(body,
                textAlign: TextAlign.center,
                style: TextStyle(color: Fleet.ink400, fontSize: 13)),
            if (action != null) ...[
              const SizedBox(height: 18),
              action!,
            ],
          ],
        ),
      ),
    );
  }
}
