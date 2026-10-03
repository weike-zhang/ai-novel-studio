package worlds

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type fakeAuthoringRepository struct {
	world  AuthoringWorld
	err    error
	create CreateDraftInput
	update UpdateDraftInput
}

func (f *fakeAuthoringRepository) CreateDraft(_ context.Context, input CreateDraftInput) (AuthoringWorld, error) {
	f.create = input
	return f.world, f.err
}
func (f *fakeAuthoringRepository) UpdateDraft(_ context.Context, input UpdateDraftInput) (AuthoringWorld, error) {
	f.update = input
	return f.world, f.err
}
func (f *fakeAuthoringRepository) ConfirmAIFields(context.Context, ConfirmAIFieldsInput) (AuthoringWorld, error) {
	return f.world, f.err
}
func (f *fakeAuthoringRepository) Publish(context.Context, ChangePublicationInput) (AuthoringWorld, error) {
	return f.world, f.err
}
func (f *fakeAuthoringRepository) Unpublish(context.Context, ChangePublicationInput) (AuthoringWorld, error) {
	return f.world, f.err
}

func TestAuthoringHTTPRequiresIdentity(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/v1/worlds", strings.NewReader(`{"title":"雾港"}`))
	response := httptest.NewRecorder()
	NewAuthoringHandler(&fakeAuthoringRepository{}).ServeHTTP(response, request)
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAuthoringHTTPCreateAndVersionConflict(t *testing.T) {
	repository := &fakeAuthoringRepository{world: AuthoringWorld{ID: "world-1", Version: 1}}
	handler := NewAuthoringHandler(repository)
	request := httptest.NewRequest(http.MethodPost, "/v1/worlds", strings.NewReader(`{"title":"雾港","summary":"简介","setting":{},"source":"manual"}`))
	request = request.WithContext(WithUserID(request.Context(), "0198b8a0-0001-7000-8000-000000000001"))
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusCreated || repository.create.Title != "雾港" {
		t.Fatalf("status=%d input=%#v", response.Code, repository.create)
	}

	repository.err = ErrVersionConflict
	request = httptest.NewRequest(http.MethodPatch, "/v1/worlds/world-1", strings.NewReader(`{"expectedVersion":1,"title":"雾港","summary":"简介","setting":{}}`))
	request = request.WithContext(WithUserID(request.Context(), "0198b8a0-0001-7000-8000-000000000001"))
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusConflict || !strings.Contains(response.Body.String(), "version_conflict") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAuthoringHTTPReturnsStructuredValidation(t *testing.T) {
	repository := &fakeAuthoringRepository{err: &ValidationError{Issues: []ValidationIssue{{Field: "title", Code: "required", Message: "标题不能为空"}}}}
	request := httptest.NewRequest(http.MethodPost, "/v1/worlds", strings.NewReader(`{"title":""}`))
	request = request.WithContext(WithUserID(request.Context(), "0198b8a0-0001-7000-8000-000000000001"))
	response := httptest.NewRecorder()
	NewAuthoringHandler(repository).ServeHTTP(response, request)
	if response.Code != http.StatusUnprocessableEntity || !strings.Contains(response.Body.String(), "issues") {
		t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
	}
}

func TestAuthoringHTTPPublicationModerationOutcomes(t *testing.T) {
	identity := "0198b8a0-0001-7000-8000-000000000001"
	for name, fixture := range map[string]struct {
		world AuthoringWorld
		err   error
		code  int
		body  string
	}{
		"pending":     {world: AuthoringWorld{Status: "pending_review"}, code: http.StatusAccepted, body: "pending_review"},
		"revise":      {err: ErrModerationRevise, code: http.StatusUnprocessableEntity, body: "publication_requires_revision"},
		"blocked":     {err: ErrModerationBlocked, code: http.StatusForbidden, body: "publication_blocked"},
		"unavailable": {err: ErrModerationUnavailable, code: http.StatusServiceUnavailable, body: "moderation_unavailable"},
	} {
		t.Run(name, func(t *testing.T) {
			repository := &fakeAuthoringRepository{world: fixture.world, err: fixture.err}
			request := httptest.NewRequest(http.MethodPost, "/v1/worlds/world-1/publish", strings.NewReader(`{"expectedVersion":1}`))
			request = request.WithContext(WithUserID(request.Context(), identity))
			response := httptest.NewRecorder()
			NewAuthoringHandler(repository).ServeHTTP(response, request)
			if response.Code != fixture.code || !strings.Contains(response.Body.String(), fixture.body) {
				t.Fatalf("status=%d body=%s", response.Code, response.Body.String())
			}
		})
	}
}
