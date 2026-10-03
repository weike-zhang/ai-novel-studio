package worlds

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

const (
	testWorldID = "11111111-1111-4111-8111-111111111111"
	testUserID  = "22222222-2222-4222-8222-222222222222"
)

type fakeRepository struct {
	listInput    PublicWorldQuery
	viewerID     string
	journeyInput CreateJourneyInput
	listPage     PublicWorldPage
	world        World
	journey      Journey
	err          error
}

func (f *fakeRepository) ListPublic(_ context.Context, input PublicWorldQuery) (PublicWorldPage, error) {
	f.listInput = input
	return f.listPage, f.err
}

func (f *fakeRepository) Get(_ context.Context, _ string, viewerID string) (World, error) {
	f.viewerID = viewerID
	return f.world, f.err
}

func (f *fakeRepository) CreateJourney(_ context.Context, input CreateJourneyInput) (Journey, error) {
	f.journeyInput = input
	return f.journey, f.err
}

func TestListWorldsParsesQueryWithoutAuthentication(t *testing.T) {
	repository := &fakeRepository{listPage: PublicWorldPage{Worlds: []WorldCard{}}}
	cursor, err := encodeCursor(pageCursor{Sort: SortPopular, Popularity: 4, UpdatedAt: time.Now().UTC(), ID: testWorldID})
	if err != nil {
		t.Fatal(err)
	}
	request := httptest.NewRequest(http.MethodGet, "/v1/worlds?keyword=mist&type=fantasy&tags=sea&tags=slow,birds&sort=popular&cursor="+url.QueryEscape(cursor)+"&limit=12", nil)
	response := httptest.NewRecorder()

	NewHandler(repository).ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	input := repository.listInput
	if input.Keyword != "mist" || input.Type != "fantasy" || input.Sort != SortPopular || input.Cursor != cursor || input.Limit != 12 {
		t.Fatalf("query = %#v", input)
	}
	if got := strings.Join(input.Tags, ","); got != "sea,slow,birds" {
		t.Fatalf("tags = %q", got)
	}
}

func TestGetWorldPassesOptionalViewer(t *testing.T) {
	repository := &fakeRepository{world: World{WorldCard: WorldCard{ID: testWorldID}}}
	request := httptest.NewRequest(http.MethodGet, "/worlds/"+testWorldID, nil)
	request = request.WithContext(WithUserID(request.Context(), testUserID))
	response := httptest.NewRecorder()

	NewHandler(repository).ServeHTTP(response, request)

	if response.Code != http.StatusOK || repository.viewerID != testUserID {
		t.Fatalf("status = %d, viewer = %q", response.Code, repository.viewerID)
	}
}

func TestGetWorldMapsHiddenWorldToNotFound(t *testing.T) {
	repository := &fakeRepository{err: ErrNotFound}
	request := httptest.NewRequest(http.MethodGet, "/v1/worlds/"+testWorldID, nil)
	response := httptest.NewRecorder()

	NewHandler(repository).ServeHTTP(response, request)

	assertError(t, response, http.StatusNotFound, "not_found")
}

func TestCreateSimulationRequiresAuthentication(t *testing.T) {
	repository := &fakeRepository{}
	request := httptest.NewRequest(http.MethodPost, "/simulations", strings.NewReader(`{"worldId":"`+testWorldID+`"}`))
	response := httptest.NewRecorder()

	NewHandler(repository).ServeHTTP(response, request)

	assertError(t, response, http.StatusUnauthorized, "authentication_required")
	if repository.journeyInput != (CreateJourneyInput{}) {
		t.Fatal("store was called without authentication")
	}
}

func TestCreateSimulationUsesContractBody(t *testing.T) {
	repository := &fakeRepository{journey: Journey{ID: "33333333-3333-4333-8333-333333333333", WorldID: testWorldID}}
	request := httptest.NewRequest(http.MethodPost, "/v1/simulations", strings.NewReader(`{"worldId":"`+testWorldID+`"}`))
	request = request.WithContext(WithUserID(request.Context(), testUserID))
	response := httptest.NewRecorder()

	NewHandler(repository).ServeHTTP(response, request)

	if response.Code != http.StatusCreated {
		t.Fatalf("status = %d, body = %s", response.Code, response.Body.String())
	}
	want := (CreateJourneyInput{UserID: testUserID, WorldID: testWorldID})
	if repository.journeyInput != want {
		t.Fatalf("input = %#v, want %#v", repository.journeyInput, want)
	}
}

func TestCreateSimulationRejectsNestedLegacyRoute(t *testing.T) {
	repository := &fakeRepository{}
	request := httptest.NewRequest(http.MethodPost, "/worlds/"+testWorldID+"/simulations", strings.NewReader(`{}`))
	response := httptest.NewRecorder()

	NewHandler(repository).ServeHTTP(response, request)

	assertError(t, response, http.StatusNotFound, "not_found")
}

func TestCreateSimulationRejectsUnknownFieldsAndInvalidUUID(t *testing.T) {
	tests := []string{
		`{"worldId":"not-a-uuid"}`,
		`{"worldId":"` + testWorldID + `","ownerId":"` + testUserID + `"}`,
		`{"worldId":"` + testWorldID + `"} {}`,
	}
	for _, body := range tests {
		t.Run(body, func(t *testing.T) {
			repository := &fakeRepository{}
			request := httptest.NewRequest(http.MethodPost, "/simulations", strings.NewReader(body))
			request = request.WithContext(WithUserID(request.Context(), testUserID))
			response := httptest.NewRecorder()

			NewHandler(repository).ServeHTTP(response, request)

			assertError(t, response, http.StatusBadRequest, "invalid_request")
		})
	}
}

func TestListWorldsRejectsInvalidQuery(t *testing.T) {
	repository := &fakeRepository{}
	request := httptest.NewRequest(http.MethodGet, "/worlds?sort=random", nil)
	response := httptest.NewRecorder()

	NewHandler(repository).ServeHTTP(response, request)

	assertError(t, response, http.StatusBadRequest, "invalid_request")
}

func TestListWorldsDoesNotExposeStoreFailure(t *testing.T) {
	repository := &fakeRepository{err: errors.New("database address and secret details")}
	request := httptest.NewRequest(http.MethodGet, "/worlds", nil)
	response := httptest.NewRecorder()

	NewHandler(repository).ServeHTTP(response, request)

	assertError(t, response, http.StatusInternalServerError, "internal_error")
	if strings.Contains(response.Body.String(), "database") {
		t.Fatalf("internal error leaked: %s", response.Body.String())
	}
}

func TestWrongMethodReturnsJSONAndAllowHeader(t *testing.T) {
	request := httptest.NewRequest(http.MethodPost, "/worlds", nil)
	response := httptest.NewRecorder()

	NewHandler(&fakeRepository{}).ServeHTTP(response, request)

	assertError(t, response, http.StatusMethodNotAllowed, "method_not_allowed")
	if response.Header().Get("Allow") != http.MethodGet {
		t.Fatalf("Allow = %q", response.Header().Get("Allow"))
	}
}

func assertError(t *testing.T, response *httptest.ResponseRecorder, status int, code string) {
	t.Helper()
	if response.Code != status {
		t.Fatalf("status = %d, want %d, body = %s", response.Code, status, response.Body.String())
	}
	if contentType := response.Header().Get("Content-Type"); contentType != "application/json; charset=utf-8" {
		t.Fatalf("Content-Type = %q", contentType)
	}
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode error: %v", err)
	}
	if body.Code != code {
		t.Fatalf("code = %q, want %q", body.Code, code)
	}
}
