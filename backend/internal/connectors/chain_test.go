package connectors

import (
	"reflect"
	"testing"
)

// A per-request pin leads, then the bot's own order. The pin must not appear
// twice, or a failing provider would be retried before the fallback it exists
// to fall back to.
func TestPreferredChain(t *testing.T) {
	cases := []struct {
		name      string
		preferred string
		bot       []string
		want      []string
	}{
		{
			name: "no pin keeps the bot's order",
			bot:  []string{"a", "b"},
			want: []string{"a", "b"},
		},
		{
			name:      "a pin leads",
			preferred: "c",
			bot:       []string{"a", "b"},
			want:      []string{"c", "a", "b"},
		},
		{
			name:      "a pin already in the chain is not duplicated",
			preferred: "b",
			bot:       []string{"a", "b", "c"},
			want:      []string{"b", "a", "c"},
		},
		{
			name:      "a pin with no bot chain stands alone",
			preferred: "a",
			want:      []string{"a"},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := PreferredChain(tc.preferred, tc.bot)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("PreferredChain(%q, %v) = %v, want %v",
					tc.preferred, tc.bot, got, tc.want)
			}
		})
	}
}

// An empty chain must stay empty rather than becoming a one-element chain of
// "", which would ask the registry to prefer a provider that does not exist.
func TestPreferredChainStaysEmpty(t *testing.T) {
	if got := PreferredChain("", nil); len(got) != 0 {
		t.Errorf("expected an empty chain, got %v", got)
	}
}
