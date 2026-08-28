import 'package:flutter/material.dart';
import 'package:flutter_riverpod/flutter_riverpod.dart';

import '../../core/state.dart';

class VoiceProfile {
  const VoiceProfile({
    required this.id,
    required this.name,
    required this.gender,
    required this.tag,
    this.isDefault = false,
  });

  final String id;
  final String name;
  final String gender;
  final String tag;
  final bool isDefault;
}

const curatedVoices = [
  VoiceProfile(
    id: 'shadow',
    name: 'Shadow',
    gender: 'male',
    tag: '🕶️ Deep Cyberpunk Operative (Default)',
    isDefault: true,
  ),
  VoiceProfile(
    id: 'atlas',
    name: 'Atlas',
    gender: 'male',
    tag: '🏛️ Resonant, Authoritative Architect',
  ),
  VoiceProfile(
    id: 'vortex',
    name: 'Vortex',
    gender: 'male',
    tag: '⚡ Dynamic, Energetic High-Velocity',
  ),
  VoiceProfile(
    id: 'echo',
    name: 'Echo',
    gender: 'male',
    tag: '📊 Calm, Analytical Quant',
  ),
  VoiceProfile(
    id: 'aura',
    name: 'Aura',
    gender: 'female',
    tag: '💎 Crisp, Futuristic AI Co-Pilot',
  ),
  VoiceProfile(
    id: 'lyra',
    name: 'Lyra',
    gender: 'female',
    tag: '🌸 Warm, Natural Conversationalist',
  ),
];

class VoiceScreen extends ConsumerStatefulWidget {
  const VoiceScreen({super.key});

  @override
  ConsumerState<VoiceScreen> createState() => _VoiceScreenState();
}

class _VoiceScreenState extends ConsumerState<VoiceScreen> with SingleTickerProviderStateMixin {
  String _selectedVoice = 'shadow';
  bool _isListening = false;
  bool _isSpeaking = false;
  final List<Map<String, String>> _dialogue = [];
  final _textController = TextEditingController();

  late AnimationController _animController;
  late Animation<double> _pulseAnimation;

  @override
  void initState() {
    super.initState();
    _animController = AnimationController(
      vsync: this,
      duration: const Duration(milliseconds: 1200),
    )..repeat(reverse: true);
    _pulseAnimation = Tween<double>(begin: 1.0, end: 1.25).animate(
      CurvedAnimation(parent: _animController, curve: Curves.easeInOut),
    );
  }

  @override
  void dispose() {
    _animController.dispose();
    _textController.dispose();
    super.dispose();
  }

  void _toggleListening() {
    setState(() {
      _isListening = !_isListening;
    });

    if (!_isListening && _textController.text.trim().isNotEmpty) {
      _handleSend(_textController.text.trim());
    }
  }

  void _handleSend(String text) {
    if (text.isEmpty) return;
    final now = TimeOfDay.now().format(context);
    setState(() {
      _dialogue.add({'sender': 'user', 'text': text, 'time': now});
      _isSpeaking = true;
      _textController.clear();
    });

    // Simulate spoken response back
    Future.delayed(const Duration(seconds: 2), () {
      if (!mounted) return;
      setState(() {
        _isSpeaking = false;
        _dialogue.add({
          'sender': 'bot',
          'text': 'Executing directive: "$text" with voice model $_selectedVoice.',
          'time': TimeOfDay.now().format(context),
        });
      });
    });
  }

  @override
  Widget build(BuildContext context) {
    final theme = Theme.of(context);

    return Scaffold(
      appBar: AppBar(
        title: const Text('🎙️ Pocket TTS Voice Co-Pilot'),
      ),
      body: Column(
        children: [
          // Voice Selector Card
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 16, vertical: 10),
            color: theme.colorScheme.surfaceContainerHighest.withOpacity(0.4),
            child: Row(
              mainAxisAlignment: MainAxisAlignment.spaceBetween,
              children: [
                const Text('Voice Profile:', style: TextStyle(fontSize: 13, fontWeight: FontWeight.w600)),
                DropdownButton<String>(
                  value: _selectedVoice,
                  underline: const SizedBox(),
                  onChanged: (val) => setState(() => _selectedVoice = val ?? 'shadow'),
                  items: curatedVoices.map((v) {
                    return DropdownMenuItem(
                      value: v.id,
                      child: Text(
                        '${v.name} (${v.gender})',
                        style: const TextStyle(fontSize: 13),
                      ),
                    );
                  }).toList(),
                ),
              ],
            ),
          ),

          // Central Animated Voice Orb
          Expanded(
            flex: 3,
            child: Center(
              child: Column(
                mainAxisAlignment: MainAxisAlignment.center,
                children: [
                  ScaleTransition(
                    scale: (_isListening || _isSpeaking) ? _pulseAnimation : const AlwaysStoppedAnimation(1.0),
                    child: GestureDetector(
                      onTap: _toggleListening,
                      child: Container(
                        width: 120,
                        height: 120,
                        decoration: BoxDecoration(
                          shape: BoxShape.circle,
                          color: _isListening
                              ? Colors.green
                              : _isSpeaking
                                  ? Colors.blue
                                  : theme.colorScheme.primaryContainer,
                          boxShadow: [
                            BoxShadow(
                              color: (_isListening ? Colors.green : Colors.blue).withOpacity(0.3),
                              blurRadius: 24,
                              spreadRadius: 6,
                            ),
                          ],
                        ),
                        child: Icon(
                          _isListening
                              ? Icons.mic
                              : _isSpeaking
                                  ? Icons.volume_up
                                  : Icons.mic_none,
                          size: 48,
                          color: (_isListening || _isSpeaking) ? Colors.white : theme.colorScheme.onPrimaryContainer,
                        ),
                      ),
                    ),
                  ),
                  const SizedBox(height: 16),
                  Text(
                    _isListening
                        ? 'Listening… Tap to finish'
                        : _isSpeaking
                            ? 'Agent speaking ($_selectedVoice)…'
                            : 'Tap orb to start voice dialogue',
                    style: TextStyle(
                      fontSize: 13,
                      fontWeight: FontWeight.w600,
                      color: _isListening ? Colors.green : Colors.grey,
                    ),
                  ),
                ],
              ),
            ),
          ),

          const Divider(height: 1),

          // Spoken Transcript History
          Expanded(
            flex: 4,
            child: ListView.builder(
              padding: const EdgeInsets.all(12),
              itemCount: _dialogue.length,
              itemBuilder: (context, i) {
                final d = _dialogue[i];
                final isUser = d['sender'] == 'user';
                return Align(
                  alignment: isUser ? Alignment.centerRight : Alignment.centerLeft,
                  child: Container(
                    margin: const EdgeInsets.only(bottom: 8),
                    padding: const EdgeInsets.all(10),
                    constraints: BoxConstraints(
                      maxWidth: MediaQuery.of(context).size.width * 0.8,
                    ),
                    decoration: BoxDecoration(
                      color: isUser
                          ? theme.colorScheme.primary.withOpacity(0.15)
                          : theme.colorScheme.surfaceContainerHighest,
                      borderRadius: BorderRadius.circular(12),
                    ),
                    child: Column(
                      crossAxisAlignment: CrossAxisAlignment.start,
                      children: [
                        Row(
                          mainAxisSize: MainAxisSize.min,
                          children: [
                            Text(
                              isUser ? 'You' : 'Agent ($_selectedVoice)',
                              style: TextStyle(
                                fontSize: 10,
                                fontWeight: FontWeight.bold,
                                color: theme.colorScheme.primary,
                              ),
                            ),
                            const SizedBox(width: 8),
                            Text(d['time'] ?? '', style: const TextStyle(fontSize: 9, color: Colors.grey)),
                          ],
                        ),
                        const SizedBox(height: 4),
                        Text(d['text'] ?? '', style: const TextStyle(fontSize: 12)),
                      ],
                    ),
                  ),
                );
              },
            ),
          ),

          // Quick Text Input
          Container(
            padding: const EdgeInsets.symmetric(horizontal: 8, vertical: 6),
            decoration: BoxDecoration(
              color: theme.colorScheme.surface,
              border: Border(top: BorderSide(color: theme.dividerColor)),
            ),
            child: Row(
              children: [
                Expanded(
                  child: TextField(
                    controller: _textController,
                    decoration: const InputDecoration(
                      hintText: 'Type or speak a directive…',
                      border: InputBorder.none,
                      isDense: true,
                    ),
                    onSubmitted: (val) => _handleSend(val.trim()),
                  ),
                ),
                IconButton(
                  icon: const Icon(Icons.send),
                  onPressed: () => _handleSend(_textController.text.trim()),
                ),
              ],
            ),
          ),
        ],
      ),
    );
  }
}
