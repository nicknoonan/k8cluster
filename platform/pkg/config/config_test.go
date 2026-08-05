package config

import "testing"

func TestParseManagedDeployments(t *testing.T) {
	items, err := ParseManagedDeployments("default/api,media/transcoder")
	if err != nil {
		t.Fatalf("ParseManagedDeployments returned error: %v", err)
	}
	if len(items) != 2 {
		t.Fatalf("len(items) = %d, want 2", len(items))
	}
	if items[0].Namespace != "default" || items[0].Name != "api" {
		t.Fatalf("unexpected first item: %#v", items[0])
	}
}

func TestParseManagedDeploymentsRejectsInvalidValue(t *testing.T) {
	if _, err := ParseManagedDeployments("broken"); err == nil {
		t.Fatal("expected error")
	}
}
