package agent

import (
	"testing"

	"github.com/BryantVanOrden/OpenAgentFleet/backend/pkg/protocol"
)

// Catalogue work happens over the API and never touches the desktop. Judging
// it by whether the screen changed reported healthy agents as stuck.
func TestOffScreenActionsDoNotCountAsStalls(t *testing.T) {
	offScreen := []protocol.ActionKind{
		protocol.ActPublishWork, protocol.ActReadWork, protocol.ActRemember,
		protocol.ActRecall, protocol.ActMsgPeer, protocol.ActDelegateTask,
		protocol.ActDeepSearch, protocol.ActCallMCP, protocol.ActShareSecret,
		protocol.ActSnapshot, protocol.ActAssert, protocol.ActSpeak,
		"", // the first turn of a task: no previous action
	}
	for _, k := range offScreen {
		if movesScreen(k) {
			t.Errorf("%q counted as a screen-moving action", k)
		}
	}
}

func TestScreenActionsStillCountAsStalls(t *testing.T) {
	onScreen := []protocol.ActionKind{
		protocol.ActClick, protocol.ActDoubleClick, protocol.ActRightClick,
		protocol.ActType, protocol.ActKey, protocol.ActScroll,
		protocol.ActDrag, protocol.ActFocus,
	}
	for _, k := range onScreen {
		if !movesScreen(k) {
			t.Errorf("%q should count toward a stall", k)
		}
	}
}
