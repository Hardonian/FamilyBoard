package errdefs_test

import (
	"errors"
	"net/http"
	"testing"

	"github.com/Hardonian/FamilyBoard/internal/errdefs"
)

func TestErrorTaxonomy(t *testing.T) {
	root := errors.New("connection reset by peer")
	retryErr := errdefs.Retryable("upstream call timed out", root)

	if !errdefs.IsRetryable(retryErr) {
		t.Errorf("expected IsRetryable to be true")
	}
	if errdefs.HTTPStatusCode(retryErr) != http.StatusServiceUnavailable {
		t.Errorf("expected 503 for retryable, got %d", errdefs.HTTPStatusCode(retryErr))
	}
	if !errors.Is(retryErr, root) {
		t.Errorf("expected unwrapping to find root cause")
	}

	authErr := errdefs.Authentication("invalid or expired oauth token")
	if !errdefs.IsAuthentication(authErr) {
		t.Errorf("expected IsAuthentication to be true")
	}
	if errdefs.HTTPStatusCode(authErr) != http.StatusUnauthorized {
		t.Errorf("expected 401 for auth error, got %d", errdefs.HTTPStatusCode(authErr))
	}

	valErr := errdefs.Validation("start time must precede end time")
	if !errdefs.IsValidation(valErr) {
		t.Errorf("expected IsValidation to be true")
	}
	if errdefs.HTTPStatusCode(valErr) != http.StatusBadRequest {
		t.Errorf("expected 400 for validation, got %d", errdefs.HTTPStatusCode(valErr))
	}

	confErr := errdefs.Conflict("etag precondition failed")
	if !errdefs.IsConflict(confErr) {
		t.Errorf("expected IsConflict to be true")
	}
	if errdefs.HTTPStatusCode(confErr) != http.StatusConflict {
		t.Errorf("expected 409 for conflict, got %d", errdefs.HTTPStatusCode(confErr))
	}

	permErr := errdefs.Permanent("event 404 does not exist remotely")
	if !errdefs.IsPermanent(permErr) {
		t.Errorf("expected IsPermanent to be true")
	}
	if errdefs.HTTPStatusCode(permErr) != http.StatusUnprocessableEntity {
		t.Errorf("expected 422 for permanent error, got %d", errdefs.HTTPStatusCode(permErr))
	}

	unavailErr := errdefs.Unavailable("calendar backend offline")
	if !errdefs.IsUnavailable(unavailErr) {
		t.Errorf("expected IsUnavailable to be true")
	}
	if !errdefs.IsRetryable(unavailErr) {
		t.Errorf("unavailable should be treated as retryable")
	}
}
