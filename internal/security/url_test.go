package security

import "testing"

func TestValidateFetchURLBlocksLocalTargets(t *testing.T) {
	tests := []string{
		"http://127.0.0.1",
		"http://localhost:8080",
		"http://10.0.0.5",
		"http://192.168.1.10",
		"http://[::1]",
		"file:///etc/passwd",
	}

	for _, rawURL := range tests {
		if err := ValidateFetchURL(rawURL); err == nil {
			t.Fatalf("expected SSRF validation to block %s", rawURL)
		}
	}
}

func TestValidateFetchURLAllowsPublicIPLiteral(t *testing.T) {
	if err := ValidateFetchURL("https://93.184.216.34"); err != nil {
		t.Fatalf("expected public IP literal to pass, got %v", err)
	}
}
