package ai

import "testing"

func TestProviderKeys(t *testing.T) {
	p := &ProviderConfig{APIKey: "single-key"}
	keys := p.Keys()
	if len(keys) != 1 || keys[0] != "single-key" {
		t.Fatalf("unexpected keys: %#v", keys)
	}

	p = &ProviderConfig{APIKeys: []string{"k1", " ", "k2"}}
	keys = p.Keys()
	if len(keys) != 2 || keys[0] != "k1" || keys[1] != "k2" {
		t.Fatalf("unexpected key pool: %#v", keys)
	}
}
