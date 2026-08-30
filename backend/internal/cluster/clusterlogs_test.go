package cluster

import (
	"context"
	"strings"
	"testing"

	idpv1 "github.com/idp/platform/backend/internal/gen/idp/v1"
)

func TestStreamClusterLogsRequiresFleet(t *testing.T) {
	s := NewService(nil)
	err := s.StreamClusterLogs(context.Background(), &idpv1.StreamClusterLogsRequest{Id: "not-a-uuid"}, func(*idpv1.LogLine) error {
		return nil
	})
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "id is required") && !strings.Contains(err.Error(), "fleet is not configured") {
		t.Fatalf("err = %v", err)
	}
}
