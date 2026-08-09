package dbadmin

import "testing"

func TestClampLimit(t *testing.T) {
	tests := []struct {
		in   int32
		want int32
	}{
		{0, defaultLimit},
		{-1, defaultLimit},
		{10, 10},
		{maxQueryLimit, maxQueryLimit},
		{maxQueryLimit + 1, maxQueryLimit},
	}
	for _, tt := range tests {
		if got := clampLimit(tt.in); got != tt.want {
			t.Errorf("clampLimit(%d) = %d, want %d", tt.in, got, tt.want)
		}
	}
}

func TestQuoteSQLIdent(t *testing.T) {
	got, err := quoteSQLIdent("users")
	if err != nil {
		t.Fatal(err)
	}
	if got != `"users"` {
		t.Fatalf("got %q", got)
	}
	if _, err := quoteSQLIdent("users;drop"); err == nil {
		t.Fatal("expected error for invalid identifier")
	}
}
