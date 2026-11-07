package collector

import (
	"signalatlas/api/internal/domain"
	"testing"
)

func TestPoolKeySeparatesAccountsAndSecrets(t *testing.T) {
	base := domain.Profile{ID: "gateway", Host: "192.168.1.1", Username: "root"}
	if poolKey(base, "secret-a") != poolKey(base, "secret-a") {
		t.Fatal("the same account and secret must map to the same pooled session")
	}
	if poolKey(base, "secret-a") == poolKey(base, "secret-b") {
		t.Fatal("a changed password must not reuse the old session")
	}
	other := base
	other.ID = "ap-main"
	if poolKey(base, "secret-a") == poolKey(other, "secret-a") {
		t.Fatal("different profiles must not share a session")
	}
	other = base
	other.Host = "192.168.1.2"
	if poolKey(base, "secret-a") == poolKey(other, "secret-a") {
		t.Fatal("different hosts must not share a session")
	}
}
