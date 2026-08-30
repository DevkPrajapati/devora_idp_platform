package project

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgtype"
)

func TestReleaseNamespacesSkipsWhenNoRepo(t *testing.T) {
	s := &Service{}
	if err := s.releaseNamespaces(context.Background(), pgtype.UUID{}); err != nil {
		t.Fatal(err)
	}
}
