import { useEffect, useRef, useState } from "react";
import * as api from "../lib/api";
import { ErrorNote, Modal, cx } from "./ui";

export interface VoiceOption {
  id: string;
  name: string;
  gender: "male" | "female";
  tag: string;
  is_default?: boolean;
}

export const CURATED_VOICES: VoiceOption[] = [
  {
    id: "shadow",
    name: "Shadow",
    gender: "male",
    tag: "🕶️ Deep Cyberpunk Tech Operative (Default)",
    is_default: true,
  },
  {
    id: "atlas",
    name: "Atlas",
    gender: "male",
    tag: "🏛️ Resonant, Authoritative Architect",
  },
  {
    id: "vortex",
    name: "Vortex",
    gender: "male",
    tag: "⚡ Dynamic, Energetic High-Velocity",
  },
  {
    id: "echo",
    name: "Echo",
    gender: "male",
    tag: "📊 Calm, Analytical Quant",
  },
  {
    id: "aura",
    name: "Aura",
    gender: "female",
    tag: "💎 Crisp, Futuristic AI Co-Pilot",
  },
  {
    id: "lyra",
    name: "Lyra",
    gender: "female",
    tag: "🌸 Warm, Natural Conversationalist",
  },
];

interface VoiceCoPilotProps {
  open: boolean;
  onClose: () => void;
  instanceName?: string;
  onSendSpokenCommand?: (transcript: string) => Promise<void>;
}

export default function VoiceCoPilot({
  open,
  onClose,
  instanceName,
  onSendSpokenCommand,
}: VoiceCoPilotProps) {
  const [selectedVoice, setSelectedVoice] = useState<string>("shadow");
  const [isListening, setIsListening] = useState<boolean>(false);
  const [isSpeaking, setIsSpeaking] = useState<boolean>(false);
  const [transcript, setTranscript] = useState<string>("");
  const [conversation, setConversation] = useState<
    Array<{ sender: "user" | "bot"; text: string; time: string }>
  >([]);
  const [error, setError] = useState<string | null>(null);
  // Whether the sidecar answered. Null while unknown, so the banner does not
  // flash "unavailable" during the first request.
  const [ttsAvailable, setTtsAvailable] = useState<boolean | null>(null);

  const recognitionRef = useRef<any>(null);
  const audioRef = useRef<HTMLAudioElement | null>(null);

  // Declared before the effects that clean up with it: it is a const, so the
  // effects below would be referencing it before initialisation otherwise.
  const stopPlayback = () => {
    if (audioRef.current) {
      audioRef.current.pause();
      audioRef.current = null;
    }
    if ("speechSynthesis" in window) window.speechSynthesis.cancel();
  };

  // Ask the sidecar what it can do, once the modal is actually opened. Asking
  // on mount would probe it for every operator who never opens this panel.
  useEffect(() => {
    if (!open) return;
    let cancelled = false;
    void api.voice
      .list()
      .then((cat) => {
        if (cancelled) return;
        setTtsAvailable(cat.available);
        if (cat.available && cat.default) setSelectedVoice(cat.default);
      })
      .catch(() => {
        if (!cancelled) setTtsAvailable(false);
      });
    return () => {
      cancelled = true;
    };
  }, [open]);

  // Closing the panel has to silence it. Without this an utterance keeps
  // playing over whatever the operator does next, with no visible control.
  useEffect(() => {
    if (!open) stopPlayback();
  }, [open]);

  useEffect(() => stopPlayback, []);

  useEffect(() => {
    // Check browser SpeechRecognition support
    const SpeechRecognition =
      (window as any).SpeechRecognition || (window as any).webkitSpeechRecognition;
    if (SpeechRecognition) {
      const recognition = new SpeechRecognition();
      recognition.continuous = false;
      recognition.interimResults = true;
      recognition.lang = "en-US";

      recognition.onresult = (event: any) => {
        const current = event.resultIndex;
        const text = event.results[current][0].transcript;
        setTranscript(text);
      };

      recognition.onerror = (event: any) => {
        console.error("Speech recognition error:", event.error);
        setIsListening(false);
      };

      recognition.onend = () => {
        setIsListening(false);
      };

      recognitionRef.current = recognition;
    }
  }, []);

  const toggleListen = () => {
    if (!recognitionRef.current) {
      setError("Speech recognition is not supported in this browser. Please use Chrome/Edge.");
      return;
    }

    if (isListening) {
      recognitionRef.current.stop();
      setIsListening(false);
      if (transcript.trim()) {
        void handleSend(transcript.trim());
      }
    } else {
      setError(null);
      setTranscript("");
      try {
        recognitionRef.current.start();
        setIsListening(true);
      } catch (err) {
        console.error(err);
      }
    }
  };

  const handleSend = async (text: string) => {
    if (!text) return;
    const now = new Date().toLocaleTimeString();
    setConversation((prev) => [...prev, { sender: "user", text, time: now }]);
    setTranscript("");

    try {
      if (onSendSpokenCommand) {
        await onSendSpokenCommand(text);
      }
      await speakAloud(`Executing directive: ${text}`, selectedVoice);
    } catch (err) {
      setError(err instanceof Error ? err.message : String(err));
    }
  };

  /**
   * Speak text in the fleet's own voice.
   *
   * This called `window.speechSynthesis` and nothing else, so a modal titled
   * "Pocket TTS Real-Time Voice Co-Pilot" was actually the operating system's
   * built-in robot voice, the six speakers in the picker above were labels on a
   * pitch multiplier, and the sidecar holding the real weights was never
   * contacted. Now it asks the sidecar, and only falls back to the browser when
   * there is no sidecar to ask -- with the fallback said out loud in the
   * transcript, so nobody mistakes the OS voice for the product again.
   */
  const speakAloud = async (text: string, voiceId: string) => {
    setIsSpeaking(true);
    const now = new Date().toLocaleTimeString();
    setConversation((prev) => [...prev, { sender: "bot", text, time: now }]);

    // Anything still playing is stale the moment there is something newer to
    // say, and two overlapping utterances are unintelligible.
    stopPlayback();

    try {
      const url = await api.voice.speak(text, voiceId);
      const audio = new Audio(url);
      audioRef.current = audio;
      // Revoked on both paths: a blob per utterance that is never released
      // holds the whole WAV in memory for the lifetime of the page.
      const done = () => {
        URL.revokeObjectURL(url);
        if (audioRef.current === audio) audioRef.current = null;
        setIsSpeaking(false);
      };
      audio.onended = done;
      audio.onerror = () => {
        done();
        setError("The synthesised audio could not be played by this browser.");
      };
      await audio.play();
    } catch (err) {
      setIsSpeaking(false);
      const detail = err instanceof Error ? err.message : String(err);
      if (browserFallback(text, voiceId)) {
        setError(`Speech service unavailable (${detail}) - using this browser's built-in voice.`);
      } else {
        setError(`Speech unavailable: ${detail}`);
      }
    }
  };

  /** Last resort when the sidecar is not deployed. Returns whether it ran. */
  const browserFallback = (text: string, voiceId: string): boolean => {
    if (!("speechSynthesis" in window)) return false;
    setIsSpeaking(true);
    window.speechSynthesis.cancel();
    const utterance = new SpeechSynthesisUtterance(text);
    const profile = CURATED_VOICES.find((v) => v.id === voiceId);
    if (profile) {
      utterance.pitch = profile.gender === "female" ? 1.2 : 0.85;
      utterance.rate = 1.05;
    }
    utterance.onend = () => setIsSpeaking(false);
    utterance.onerror = () => setIsSpeaking(false);
    window.speechSynthesis.speak(utterance);
    return true;
  };


  if (!open) return null;

  return (
    <Modal open={open} onClose={onClose} title="🎙️ Pocket TTS Real-Time Voice Co-Pilot" wide>
      <div className="space-y-4">
        {/* Header & Voice Selector */}
        <div className="flex flex-wrap items-center justify-between gap-4 border-b border-ink-800 pb-3">
          <div>
            <h4 className="text-sm font-semibold text-ink-100">
              Live Voice Dialogue {instanceName ? `· ${instanceName}` : ""}
            </h4>
            <p className="text-xs text-ink-400">
              {ttsAvailable === false
                ? "No text-to-speech sidecar is deployed — falling back to this browser's built-in voice."
                : "Spoken duplex communication powered by Kyutai Labs Pocket TTS."}
            </p>
          </div>

          {/* Voice Selector */}
          <div className="flex items-center gap-2">
            <span className="text-xs font-mono text-ink-400">Voice:</span>
            <select
              value={selectedVoice}
              onChange={(e) => setSelectedVoice(e.target.value)}
              className={cx(
                "rounded-lg bg-ink-850 px-2.5 py-1 text-xs font-mono text-ink-200 border border-ink-700",
              )}
            >
              <optgroup label="Male Voices (4)">
                {CURATED_VOICES.filter((v) => v.gender === "male").map((v) => (
                  <option key={v.id} value={v.id}>
                    {v.name} ({v.tag})
                  </option>
                ))}
              </optgroup>
              <optgroup label="Female Voices (2)">
                {CURATED_VOICES.filter((v) => v.gender === "female").map((v) => (
                  <option key={v.id} value={v.id}>
                    {v.name} ({v.tag})
                  </option>
                ))}
              </optgroup>
            </select>
          </div>
        </div>

        <ErrorNote error={error} onDismiss={() => setError(null)} />

        {/* Real-time Voice Waveform Visualizer */}
        <div className="flex flex-col items-center justify-center rounded-2xl bg-ink-950 p-6 border border-ink-800 text-center relative overflow-hidden">
          <div className="absolute inset-0 bg-gradient-to-b from-live-500/5 to-transparent pointer-events-none" />

          {/* Animated Waveform / Pulse Rings */}
          <div className="relative flex items-center justify-center my-4">
            <div
              className={cx(
                "absolute h-28 w-28 rounded-full transition-all duration-700",
                isListening
                  ? "animate-ping bg-live-500/20"
                  : isSpeaking
                  ? "animate-pulse bg-sky-500/25"
                  : "bg-transparent",
              )}
            />
            <button
              onClick={toggleListen}
              className={cx(
                "relative z-10 flex h-20 w-20 items-center justify-center rounded-full text-3xl shadow-xl transition-all",
                isListening
                  ? "bg-live-500 text-ink-950 scale-110 ring-4 ring-live-400/50"
                  : isSpeaking
                  ? "bg-sky-500 text-ink-950 ring-4 ring-sky-400/50"
                  : "bg-ink-800 text-ink-200 hover:bg-ink-700 hover:scale-105",
              )}
            >
              {isListening ? "🎙️" : isSpeaking ? "🔊" : "🎤"}
            </button>
          </div>

          <div className="text-xs font-mono mt-2">
            {isListening ? (
              <span className="text-live-400 animate-pulse font-semibold">Listening to speech… Click to finish</span>
            ) : isSpeaking ? (
              <span className="text-sky-400 animate-pulse font-semibold">Agent speaking with voice: {selectedVoice}</span>
            ) : (
              <span className="text-ink-400">Click microphone or hold to speak</span>
            )}
          </div>

          {transcript && (
            <p className="mt-3 rounded-lg bg-ink-900/80 px-3 py-1.5 text-xs text-ink-200 font-mono border border-ink-800 max-w-md">
              "{transcript}"
            </p>
          )}
        </div>

        {/* Live Spoken Transcript Feed */}
        <div className="max-h-48 overflow-y-auto space-y-2 rounded-xl bg-ink-900 p-3 border border-ink-800 text-xs">
          <div className="font-mono text-[10px] text-ink-500 uppercase tracking-wider mb-1">
            Dialogue Transcript
          </div>
          {conversation.length === 0 ? (
            <p className="text-ink-500 text-[11px] italic">No spoken dialogue yet. Tap the microphone to talk.</p>
          ) : (
            conversation.map((msg, i) => (
              <div
                key={i}
                className={cx(
                  "flex flex-col gap-0.5 rounded-lg p-2 max-w-[85%]",
                  msg.sender === "user"
                    ? "ml-auto bg-live-500/10 text-live-200 border border-live-500/20"
                    : "mr-auto bg-ink-800 text-ink-200 border border-ink-700",
                )}
              >
                <div className="flex items-center justify-between gap-2 text-[10px] font-mono text-ink-400">
                  <span>{msg.sender === "user" ? "You" : `Bot (${selectedVoice})`}</span>
                  <span>{msg.time}</span>
                </div>
                <p className="text-xs">{msg.text}</p>
              </div>
            ))
          )}
        </div>
      </div>
    </Modal>
  );
}
