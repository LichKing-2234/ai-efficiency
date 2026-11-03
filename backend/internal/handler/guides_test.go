package handler

import "testing"

func TestNewGuidesHonoursConfiguredLink(t *testing.T) {
	guides := NewGuides("图文教程", "https://confluence.example/page")
	if guides.CCSwitch == nil {
		t.Fatal("expected ccswitch guide")
	}
	if guides.CCSwitch.Label != "图文教程" || guides.CCSwitch.URL != "https://confluence.example/page" {
		t.Fatalf("unexpected guide: %+v", guides.CCSwitch)
	}
}

func TestNewGuidesDropsEmptyURL(t *testing.T) {
	guides := NewGuides("图文教程", "  ")
	if guides.CCSwitch != nil {
		t.Fatalf("expected no guide, got %+v", guides.CCSwitch)
	}
}

func TestNewGuidesAllowsEmptyLabel(t *testing.T) {
	guides := NewGuides("", "https://confluence.example/page")
	if guides.CCSwitch == nil || guides.CCSwitch.Label != "" {
		t.Fatalf("expected guide with empty label, got %+v", guides.CCSwitch)
	}
}
