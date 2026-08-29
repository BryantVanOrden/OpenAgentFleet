import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/models.dart';
import '../../core/state.dart';
import '../../core/theme/theme.dart';
import '../../core/widgets/inline_error.dart';

/// Open a new conversation.
///
/// Pick who is in it; the kind follows from that. Two agents with you left out
/// is a thread they can work in while you read along — which is the point of
/// being able to make one at all.
class NewConversationSheet extends ConsumerStatefulWidget {
  const NewConversationSheet({super.key});

  @override
  ConsumerState<NewConversationSheet> createState() =>
      _NewConversationSheetState();
}

class _NewConversationSheetState extends ConsumerState<NewConversationSheet> {
/// Errors show inline. This is a modal bottom sheet, and a snackbar raised
/// from inside one renders behind the sheet — invisible, which makes a failed
/// action look like a control that did nothing.
  String? _error;

  final _title = TextEditingController();
  final _selected = <String>{};
  bool _includeMe = true;
  bool _busy = false;

  @override
  void dispose() {
    _title.dispose();
    super.dispose();
  }

  /// What the chosen members add up to, in the same terms the server uses.
  String get _kindLabel {
    final agents = _selected.length;
    if (agents == 0) return 'Pick at least one agent';
    if (_includeMe && agents == 1) return 'Direct — you and one agent';
    if (!_includeMe && agents == 2) {
      return 'Pair — two agents talking, you watching';
    }
    if (!_includeMe && agents == 1) {
      return 'One agent, without you — it will talk to itself';
    }
    return 'Group — ${agents + (_includeMe ? 1 : 0)} members';
  }

  Future<void> _create() async {
    if (_selected.isEmpty) return;
    setState(() => _busy = true);
    final navigator = Navigator.of(context);
    try {
      final created = await ref.read(apiProvider).createConversation(
            title: _title.text.trim(),
            members: [
              if (_includeMe) Conversation.operatorId,
              ..._selected,
            ],
          );
      navigator.pop(created);
    } catch (err) {
      if (mounted) setState(() => _error = '$err');
      if (mounted) setState(() => _busy = false);
    }
  }

  @override
  Widget build(BuildContext context) {
    final instances = ref.watch(instancesProvider).valueOrNull ?? const [];

    return Padding(
      padding: EdgeInsets.only(
        left: 16,
        right: 16,
        top: 16,
        bottom: MediaQuery.of(context).viewInsets.bottom + 16,
      ),
      child: Column(
        mainAxisSize: MainAxisSize.min,
        crossAxisAlignment: CrossAxisAlignment.start,
        children: [
          const Text('New conversation',
              style: TextStyle(fontSize: 16, fontWeight: FontWeight.w700)),
          const SizedBox(height: 4),
          Text(_kindLabel,
              style: TextStyle(color: Fleet.ink400, fontSize: 11)),
          const SizedBox(height: 14),
          TextField(
            controller: _title,
            decoration: const InputDecoration(
              labelText: 'Title (optional)',
              hintText: 'What is this thread for?',
            ),
          ),
          const SizedBox(height: 12),
          SwitchListTile(
            contentPadding: EdgeInsets.zero,
            dense: true,
            value: _includeMe,
            onChanged: (v) => setState(() => _includeMe = v),
            title: const Text('Include me', style: TextStyle(fontSize: 13)),
            subtitle: Text(
              _includeMe
                  ? 'You are a participant'
                  : 'Agents only — you can still read every message',
              style: TextStyle(color: Fleet.ink400, fontSize: 11),
            ),
          ),
          const SizedBox(height: 4),
          Text('AGENTS',
              style: TextStyle(
                  color: Fleet.ink400,
                  fontSize: 10,
                  letterSpacing: 0.6,
                  fontWeight: FontWeight.w700)),
          const SizedBox(height: 6),
          if (instances.isEmpty)
            Padding(
              padding: const EdgeInsets.symmetric(vertical: 12),
              child: Text('No agents yet — provision one first.',
                  style: TextStyle(color: Fleet.ink400, fontSize: 12)),
            )
          else
            ConstrainedBox(
              constraints: const BoxConstraints(maxHeight: 260),
              child: SingleChildScrollView(
                child: Column(
                  children: [
                    for (final i in instances)
                      CheckboxListTile(
                        contentPadding: EdgeInsets.zero,
                        dense: true,
                        value: _selected.contains(i.id),
                        onChanged: (on) => setState(() {
                          if (on == true) {
                            _selected.add(i.id);
                          } else {
                            _selected.remove(i.id);
                          }
                        }),
                        title:
                            Text(i.name, style: const TextStyle(fontSize: 13)),
                        subtitle: Text(
                          i.voice.isEmpty ? i.state : '${i.state} · ${i.voice}',
                          style: TextStyle(color: Fleet.ink400, fontSize: 11),
                        ),
                      ),
                  ],
                ),
              ),
            ),
          InlineError(_error),
          const SizedBox(height: 14),
          SizedBox(
            width: double.infinity,
            child: FilledButton(
              onPressed: _busy || _selected.isEmpty ? null : _create,
              child: _busy
                  ? const SizedBox(
                      width: 16,
                      height: 16,
                      child: CircularProgressIndicator(strokeWidth: 2))
                  : const Text('Create'),
            ),
          ),
        ],
      ),
    );
  }
}
