package agent

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/BryantVanOrden/AgentFleet/backend/internal/voice"
	"github.com/BryantVanOrden/AgentFleet/backend/pkg/protocol"
)

// The `speak` action, made audible.
//
// It used to fall through to agentd like any other action. agentd synthesised
// with a tone generator — a sine wave modulated by the text's letter
// frequencies, not speech — base64'd the result into a response field, and
// returned it. Nothing read that field. So an agent that decided to say
// something out loud produced a buzz that was thrown away, in a sandbox with no
// audio device and no path back to the operator, and was told it had spoken.
//
// Three things had to change for the action to mean anything:
//
//  1. Synthesis happens here, against the same sidecar the console and the
//     phone use, so there is one voice and one set of weights for the fleet
//     rather than a different tone generator per desktop.
//  2. The audio is stored as an artifact, so it has a URL and outlives the turn.
//  3. The URL goes out on the event bus, which is what the console and the
//     phone are already listening to. That is the missing path: the sandbox
//     could never reach the operator, but the orchestrator always could.

// speak synthesises an utterance, stores it, and tells whoever is watching.
//
// The returned string is the agent's own observation of what happened. It is
// deliberately explicit about failure: an agent whose speech did not come out
// should know, because the alternative is one that keeps narrating to a room
// with no speakers.
func (r *Runner) speak(ctx context.Context, task *protocol.Task, inst *protocol.Instance,
	a protocol.Action) string {

	text := firstNonEmpty(a.Text, a.Summary, a.Thought)
	if strings.TrimSpace(text) == "" {
		return "failed: speak needs something to say"
	}

	// a.Target carries the voice, matching how the sandbox's handler read it.
	// Empty means the fleet default.
	audio, err := voice.Speak(ctx, text, a.Target, 0)
	if err != nil {
		if errors.Is(err, voice.ErrUnavailable) {
			// Not a task failure. Speech is an optional sidecar, and an agent
			// that stops working because nobody deployed a text-to-speech
			// service would be worse than one that carries on silently. It is
			// told plainly so it stops trying.
			return "spoken text was not synthesised: no text-to-speech service is " +
				"deployed, so speech is unavailable in this fleet. Report progress " +
				"in writing instead."
		}
		return "speech failed: " + clip(err.Error(), 200)
	}

	// Stored under the task, alongside that task's step screenshots, so the
	// whole trajectory — what the agent saw, what it did, what it said — is in
	// one place and is cleaned up together.
	key := fmt.Sprintf("tasks/%s/speech-%04d.wav", task.ID, task.Step)
	if r.art == nil {
		return "spoken, but not saved: no artifact store is configured"
	}
	if err := r.art.Put(ctx, key, audio.ContentType, audio.Body); err != nil {
		// The audio existed and is now lost. Say so rather than reporting a
		// clean success, which is the failure mode this whole action had.
		return "speech was synthesised but could not be stored: " + clip(err.Error(), 200)
	}

	// The event a client acts on. The console plays it and shows the transcript
	// (see the agent.speech case in admin/src/App.tsx); the phone app speaks its
	// own chat replies through the same sidecar but does not yet subscribe to
	// this event. Emitting it regardless is right — the audio is stored and the
	// event is the record that it exists — but the outcome below says "the
	// console" rather than "the console and the app", because the second half
	// would be a claim about a listener that is not there.
	r.bus.Emit("agent.speech", inst.ID, task.ID, map[string]any{
		"text":         clip(text, 500),
		"voice":        audio.Voice,
		"artifact_key": key,
		"url":          "/api/artifacts/" + key,
		"content_type": audio.ContentType,
		"bytes":        len(audio.Body),
	})

	return fmt.Sprintf("spoken aloud in voice %q (%d bytes of audio, played in the operator's console): %s",
		audio.Voice, len(audio.Body), clip(text, 160))
}
