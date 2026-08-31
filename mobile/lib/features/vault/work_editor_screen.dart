import 'package:flutter/material.dart';
import 'package:flutter/services.dart';

import '../../core/models.dart';
import '../../core/network/api_client.dart';
import '../../core/theme/theme.dart';
import 'code_highlight.dart';

/// Edit a catalog item's content.
///
/// Saving republishes under the same name and folder, which is what the
/// catalog already treats as an edit: the version goes up and the item stays
/// one item. It is deliberately the same path an agent's `publish_work` takes,
/// so a person fixing a line and an agent fixing a line leave the same trace.
class WorkEditorScreen extends StatefulWidget {
  const WorkEditorScreen({
    super.key,
    required this.api,
    required this.item,
  });

  final ApiClient api;
  final WorkItem item;

  @override
  State<WorkEditorScreen> createState() => _WorkEditorScreenState();
}

class _WorkEditorScreenState extends State<WorkEditorScreen> {
  late final CodeEditingController _controller;
  late CodeLanguage _language;
  late String _original;
  bool _saving = false;
  String? _error;

  @override
  void initState() {
    super.initState();
    _original = widget.item.content;
    _language = languageFor(
      widget.item.name,
      mime: widget.item.mime,
      content: widget.item.content,
    );
    _controller = CodeEditingController(
      text: widget.item.content,
      language: _language,
    );
    _controller.addListener(_onChanged);
  }

  @override
  void dispose() {
    _controller.removeListener(_onChanged);
    _controller.dispose();
    super.dispose();
  }

  void _onChanged() {
    // Only to keep the save button's enabled state honest.
    if (mounted) setState(() {});
  }

  bool get _dirty => _controller.text != _original;

  Future<void> _save() async {
    setState(() {
      _saving = true;
      _error = null;
    });
    try {
      final saved = await widget.api.putWorkItem(
        name: widget.item.name,
        kind: widget.item.kind,
        content: _controller.text,
        description: widget.item.description,
        parentId: widget.item.parentId,
      );
      if (!mounted) return;
      setState(() {
        _original = _controller.text;
        _saving = false;
      });
      ScaffoldMessenger.of(context).showSnackBar(
        SnackBar(content: Text('Saved ${saved.name} · v${saved.version}')),
      );
      Navigator.of(context).pop(true);
    } catch (e) {
      if (!mounted) return;
      // The catalog refuses some edits on purpose — an app edited into
      // something a browser cannot render, for one — and the reason it gives
      // is worth showing rather than replacing with "save failed".
      setState(() {
        _saving = false;
        _error = '$e';
      });
    }
  }

  Future<bool> _confirmDiscard() async {
    if (!_dirty) return true;
    final leave = await showDialog<bool>(
      context: context,
      builder: (_) => AlertDialog(
        title: const Text('Discard changes?'),
        content: Text('${widget.item.name} has unsaved edits.'),
        actions: [
          TextButton(
            onPressed: () => Navigator.of(context).pop(false),
            child: const Text('Keep editing'),
          ),
          TextButton(
            onPressed: () => Navigator.of(context).pop(true),
            child: const Text('Discard'),
          ),
        ],
      ),
    );
    return leave ?? false;
  }

  @override
  Widget build(BuildContext context) {
    return PopScope(
      canPop: false,
      onPopInvokedWithResult: (didPop, _) async {
        if (didPop) return;
        final navigator = Navigator.of(context);
        if (await _confirmDiscard()) navigator.pop(false);
      },
      child: Scaffold(
        appBar: AppBar(
          title: Column(
            crossAxisAlignment: CrossAxisAlignment.start,
            children: [
              Text(widget.item.name,
                  style: const TextStyle(fontSize: 15),
                  overflow: TextOverflow.ellipsis),
              Text(
                '${languageLabel(_language)} · v${widget.item.version}'
                '${_dirty ? ' · edited' : ''}',
                style: TextStyle(color: Fleet.ink400, fontSize: 11),
              ),
            ],
          ),
          actions: [
            PopupMenuButton<CodeLanguage>(
              tooltip: 'Syntax',
              icon: const Icon(Icons.palette_outlined, size: 20),
              onSelected: (l) => setState(() {
                _language = l;
                _controller.language = l;
              }),
              itemBuilder: (_) => [
                for (final l in CodeLanguage.values)
                  PopupMenuItem(
                    value: l,
                    child: Row(
                      children: [
                        if (l == _language)
                          const Icon(Icons.check, size: 16)
                        else
                          const SizedBox(width: 16),
                        const SizedBox(width: 8),
                        Text(languageLabel(l)),
                      ],
                    ),
                  ),
              ],
            ),
            IconButton(
              tooltip: 'Copy all',
              icon: const Icon(Icons.copy_all_outlined, size: 20),
              onPressed: () {
                Clipboard.setData(ClipboardData(text: _controller.text));
                ScaffoldMessenger.of(context).showSnackBar(
                  const SnackBar(content: Text('Copied')),
                );
              },
            ),
            Padding(
              padding: const EdgeInsets.only(right: 8),
              child: TextButton.icon(
                onPressed: (_dirty && !_saving) ? _save : null,
                icon: _saving
                    ? const SizedBox(
                        width: 14,
                        height: 14,
                        child: CircularProgressIndicator(strokeWidth: 2))
                    : const Icon(Icons.save_outlined, size: 18),
                label: const Text('Save'),
              ),
            ),
          ],
        ),
        body: Column(
          children: [
            if (_error != null)
              Container(
                width: double.infinity,
                color: Fleet.bad.withValues(alpha: 0.15),
                padding: const EdgeInsets.fromLTRB(16, 10, 16, 10),
                child: Row(
                  crossAxisAlignment: CrossAxisAlignment.start,
                  children: [
                    Icon(Icons.error_outline, size: 16, color: Fleet.bad),
                    const SizedBox(width: 8),
                    Expanded(
                      child: SelectableText(
                        _error!,
                        style: TextStyle(color: Fleet.bad, fontSize: 12),
                      ),
                    ),
                  ],
                ),
              ),
            Expanded(
              child: Container(
                color: Fleet.ink900,
                // Code is read by line, so it scrolls sideways rather than
                // wrapping: a wrapped line in a file of HTML is worse than a
                // scrollbar.
                child: SingleChildScrollView(
                  scrollDirection: Axis.horizontal,
                  child: ConstrainedBox(
                    constraints: BoxConstraints(
                      minWidth: MediaQuery.of(context).size.width,
                    ),
                    child: IntrinsicWidth(
                      child: TextField(
                        controller: _controller,
                        maxLines: null,
                        expands: false,
                        autocorrect: false,
                        enableSuggestions: false,
                        keyboardType: TextInputType.multiline,
                        style: const TextStyle(
                          fontFamily: 'monospace',
                          fontSize: 13,
                          height: 1.45,
                        ),
                        decoration: const InputDecoration(
                          border: InputBorder.none,
                          contentPadding: EdgeInsets.fromLTRB(16, 14, 16, 120),
                        ),
                      ),
                    ),
                  ),
                ),
              ),
            ),
          ],
        ),
      ),
    );
  }
}
