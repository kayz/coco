package ai

import (
	"testing"
	"time"
)

func boolPtr(v bool) *bool { return &v }

func testRegistry(models ...*ModelConfig) *Registry {
	r := &Registry{
		providers:  map[string]*ProviderConfig{},
		models:     map[string]*ModelConfig{},
		modelOrder: []string{},
	}
	for _, m := range models {
		r.models[m.Name] = m
		r.modelOrder = append(r.modelOrder, m.Name)
	}
	return r
}

func TestPickModelForRoleCronPrefersCheapFast(t *testing.T) {
	reg := testRegistry(
		&ModelConfig{Name: "main", Intellect: "excellent", Speed: "fast", Cost: "medium"},
		&ModelConfig{Name: "cheap", Intellect: "good", Speed: "fast", Cost: "low"},
		&ModelConfig{Name: "expensive", Intellect: "full", Speed: "fast", Cost: "high"},
	)
	r := NewModelRouter(reg, time.Minute)
	got := r.PickModelForRole(RoleCron)
	if got == nil || got.Name != "cheap" {
		t.Fatalf("expected cron model cheap, got %#v", got)
	}
}

func TestPrimaryRotationThreshold(t *testing.T) {
	reg := testRegistry(
		&ModelConfig{Name: "main", Intellect: "excellent", Speed: "fast", Cost: "medium"},
		&ModelConfig{Name: "backup", Intellect: "good", Speed: "fast", Cost: "low"},
	)
	r := NewModelRouter(reg, time.Minute)
	main := r.GetCurrentModel()
	if main == nil || main.Name != "main" {
		t.Fatalf("unexpected primary model: %#v", main)
	}

	r.RecordFailure(main)
	r.RecordFailure(main)
	if r.ShouldRotatePrimary(main) {
		t.Fatalf("should not rotate primary before threshold")
	}

	r.RecordFailure(main)
	if !r.ShouldRotatePrimary(main) {
		t.Fatalf("should rotate primary at threshold")
	}
	if !r.IsInCooldown(main.Name) {
		t.Fatalf("primary should enter cooldown after threshold failures")
	}
}

func TestFailoverForRolePrimaryPrefersSameClass(t *testing.T) {
	reg := testRegistry(
		&ModelConfig{Name: "main", Intellect: "excellent", Speed: "fast", Cost: "medium", Skills: []string{"multimodal"}},
		&ModelConfig{Name: "text-only-strong", Intellect: "full", Speed: "fast", Cost: "high"},
		&ModelConfig{Name: "multi-backup", Intellect: "good", Speed: "fast", Cost: "medium", Skills: []string{"multimodal"}},
	)
	r := NewModelRouter(reg, time.Minute)
	main, _ := reg.GetModel("main")
	next, err := r.FailoverForRole(RolePrimary, main)
	if err != nil {
		t.Fatalf("failover should succeed: %v", err)
	}
	if next.Name != "multi-backup" {
		t.Fatalf("expected same-class multimodal backup, got %s", next.Name)
	}
}

func TestPickModelSkipsDisabledAndTimedOffShelf(t *testing.T) {
	nowPlus := time.Now().Add(2 * time.Hour).Format(time.RFC3339)
	reg := testRegistry(
		&ModelConfig{Name: "disabled", Intellect: "excellent", Speed: "fast", Cost: "low", Enabled: boolPtr(false), Roles: []string{RoleCron}},
		&ModelConfig{Name: "timed-off", Intellect: "excellent", Speed: "fast", Cost: "low", DisabledUntil: nowPlus, Roles: []string{RoleCron}},
		&ModelConfig{Name: "ok", Intellect: "good", Speed: "fast", Cost: "medium", Roles: []string{RoleCron}},
	)
	r := NewModelRouter(reg, time.Minute)
	got := r.PickModelForRole(RoleCron)
	if got == nil || got.Name != "ok" {
		t.Fatalf("expected only available model 'ok', got %#v", got)
	}
}
