import 'package:flutter/material.dart';

import '../theme/theme.dart';

/// Three dots rising in turn. Slow on purpose: a reply from a local model can
/// take a minute, and a fast strobe for a minute is a nag, not a status.
class ThinkingDots extends StatefulWidget {
  const ThinkingDots({super.key, this.color, this.size = 6});

  final Color? color;
  final double size;

  @override
  State<ThinkingDots> createState() => _ThinkingDotsState();
}

class _ThinkingDotsState extends State<ThinkingDots>
    with SingleTickerProviderStateMixin {
  late final AnimationController _c = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 1300),
  )..repeat();

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final color = widget.color ?? Fleet.live;
    final reduce = MediaQuery.maybeOf(context)?.disableAnimations ?? false;
    return ExcludeSemantics(
      child: AnimatedBuilder(
        animation: _c,
        builder: (context, _) {
          return Row(
            mainAxisSize: MainAxisSize.min,
            children: [
              for (var i = 0; i < 3; i++) ...[
                if (i > 0) SizedBox(width: widget.size * 0.7),
                _dot(color, reduce ? 0.6 : _phase(_c.value, i)),
              ],
            ],
          );
        },
      ),
    );
  }

  /// Each dot runs the same rise-and-fall, offset by an eighth of a cycle.
  static double _phase(double t, int i) {
    final local = (t - i * 0.125) % 1.0;
    if (local < 0 || local > 0.7) return 0.0;
    final x = local / 0.7;
    return x < 0.5 ? x * 2 : (1 - x) * 2;
  }

  Widget _dot(Color color, double a) {
    return Transform.translate(
      offset: Offset(0, -3 * a),
      child: Container(
        width: widget.size,
        height: widget.size,
        decoration: BoxDecoration(
          color: color.withValues(alpha: 0.3 + 0.7 * a),
          shape: BoxShape.circle,
        ),
      ),
    );
  }
}

/// An avatar that breathes: a soft ring that swells and fades around it.
class BreathingAvatar extends StatefulWidget {
  const BreathingAvatar({super.key, required this.child, this.color});

  final Widget child;
  final Color? color;

  @override
  State<BreathingAvatar> createState() => _BreathingAvatarState();
}

class _BreathingAvatarState extends State<BreathingAvatar>
    with SingleTickerProviderStateMixin {
  late final AnimationController _c = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 2000),
  )..repeat();

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    final color = widget.color ?? Fleet.live;
    final reduce = MediaQuery.maybeOf(context)?.disableAnimations ?? false;
    return AnimatedBuilder(
      animation: _c,
      builder: (context, child) {
        final t = reduce ? 0.0 : Curves.easeOut.transform(_c.value);
        return Container(
          decoration: BoxDecoration(
            shape: BoxShape.circle,
            boxShadow: [
              BoxShadow(
                color: color.withValues(alpha: 0.35 * (1 - t)),
                spreadRadius: 7 * t,
              ),
            ],
          ),
          child: child,
        );
      },
      child: widget.child,
    );
  }
}

/// An incoming-message bubble standing in for the reply that is being written.
/// It sits where the reply will appear, so the eye is already in the right
/// place when it lands. [hint] is what the thinker is doing right now.
class ThinkingBubble extends StatelessWidget {
  const ThinkingBubble({
    super.key,
    required this.who,
    this.avatar,
    this.hint,
    this.compact = false,
  });

  final String who;
  final Widget? avatar;
  final String? hint;
  final bool compact;

  @override
  Widget build(BuildContext context) {
    final label = hint == null ? '$who is thinking' : '$who is thinking · $hint';
    return Semantics(
      liveRegion: true,
      label: label,
      child: MessageEnter(
        child: Padding(
          padding: const EdgeInsets.symmetric(vertical: 5),
          child: Row(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              BreathingAvatar(
                child: avatar ?? _Initial(who: who),
              ),
              const SizedBox(width: 8),
              Flexible(
                child: Container(
                  padding: EdgeInsets.symmetric(
                    horizontal: compact ? 10 : 14,
                    vertical: compact ? 7 : 10,
                  ),
                  decoration: BoxDecoration(
                    color: Fleet.ink850,
                    border: Border.all(color: Fleet.ink800),
                    borderRadius: const BorderRadius.only(
                      topLeft: Radius.circular(6),
                      topRight: Radius.circular(18),
                      bottomLeft: Radius.circular(18),
                      bottomRight: Radius.circular(18),
                    ),
                  ),
                  child: ExcludeSemantics(
                    child: Row(
                      mainAxisSize: MainAxisSize.min,
                      children: [
                        const ThinkingDots(),
                        const SizedBox(width: 8),
                        Flexible(
                          child: Text.rich(
                            overflow: TextOverflow.ellipsis,
                            TextSpan(
                              style: TextStyle(fontSize: 12, color: Fleet.ink400),
                              children: [
                                TextSpan(
                                  text: who,
                                  style: TextStyle(
                                    fontWeight: FontWeight.w600,
                                    color: Fleet.ink200,
                                  ),
                                ),
                                const TextSpan(text: ' is thinking'),
                                if (hint != null)
                                  TextSpan(
                                    text: ' · $hint',
                                    style: TextStyle(color: Fleet.ink500),
                                  ),
                              ],
                            ),
                          ),
                        ),
                      ],
                    ),
                  ),
                ),
              ),
            ],
          ),
        ),
      ),
    );
  }
}

/// One line of presence: a bot that is mid-run, with its step count.
class WorkingLine extends StatelessWidget {
  const WorkingLine({
    super.key,
    required this.name,
    required this.step,
    required this.maxSteps,
    this.goal = '',
  });

  final String name;
  final int step;
  final int maxSteps;
  final String goal;

  @override
  Widget build(BuildContext context) {
    return MessageEnter(
      child: Padding(
        padding: const EdgeInsets.fromLTRB(2, 4, 0, 4),
        child: Row(
          children: [
            BreathingAvatar(child: _Initial(who: name, size: 24)),
            const SizedBox(width: 8),
            Text(name,
                style: TextStyle(
                    fontSize: 12,
                    fontWeight: FontWeight.w600,
                    color: Fleet.ink200)),
            const SizedBox(width: 5),
            Text('is working', style: TextStyle(fontSize: 12, color: Fleet.ink400)),
            const SizedBox(width: 7),
            const ThinkingDots(size: 5),
            const SizedBox(width: 8),
            Expanded(
              child: Text(
                'step $step/$maxSteps${goal.isEmpty ? '' : ' · $goal'}',
                overflow: TextOverflow.ellipsis,
                style: TextStyle(fontSize: 11, color: Fleet.ink500),
              ),
            ),
          ],
        ),
      ),
    );
  }
}

class _Initial extends StatelessWidget {
  const _Initial({required this.who, this.size = 30});

  final String who;
  final double size;

  @override
  Widget build(BuildContext context) {
    return Container(
      width: size,
      height: size,
      alignment: Alignment.center,
      decoration: BoxDecoration(color: Fleet.ink800, shape: BoxShape.circle),
      child: Text(
        who.isEmpty ? '?' : who.substring(0, 1).toUpperCase(),
        style: TextStyle(
            fontSize: size * 0.36,
            fontWeight: FontWeight.w700,
            color: Fleet.ink200),
      ),
    );
  }
}

/// A message arriving: a short rise and fade, once, when the row first mounts.
/// Give it the message's id as [key] so scrolling back through history does
/// not replay it.
class MessageEnter extends StatefulWidget {
  const MessageEnter({super.key, required this.child});

  final Widget child;

  @override
  State<MessageEnter> createState() => _MessageEnterState();
}

class _MessageEnterState extends State<MessageEnter>
    with SingleTickerProviderStateMixin {
  late final AnimationController _c = AnimationController(
    vsync: this,
    duration: const Duration(milliseconds: 220),
  );
  late final Animation<double> _t =
      CurvedAnimation(parent: _c, curve: Curves.easeOutCubic);

  @override
  void initState() {
    super.initState();
    _c.forward();
  }

  @override
  void dispose() {
    _c.dispose();
    super.dispose();
  }

  @override
  Widget build(BuildContext context) {
    if (MediaQuery.maybeOf(context)?.disableAnimations ?? false) {
      return widget.child;
    }
    return AnimatedBuilder(
      animation: _t,
      builder: (context, child) => Opacity(
        opacity: _t.value,
        child: Transform.translate(
          offset: Offset(0, 8 * (1 - _t.value)),
          child: child,
        ),
      ),
      child: widget.child,
    );
  }
}
