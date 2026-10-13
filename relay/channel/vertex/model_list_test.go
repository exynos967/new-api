package vertex

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/QuantumNous/new-api/common"

	"github.com/stretchr/testify/require"
)

func modelListCredentialJSON(t *testing.T) string {
	t.Helper()
	data, err := common.Marshal(Credentials{
		ProjectID:   "model-list-project",
		PrivateKey:  "test-private-key",
		ClientEmail: "model-list@example.iam.gserviceaccount.com",
		TokenURI:    defaultGoogleTokenURI,
	})
	require.NoError(t, err)
	return string(data)
}

func withModelListTokenStub(t *testing.T) {
	t.Helper()
	original := acquireAccessTokenForModels
	acquireAccessTokenForModels = func(context.Context, Credentials, string) (string, error) {
		return "model-list-token", nil
	}
	t.Cleanup(func() { acquireAccessTokenForModels = original })
}

func TestFetchGoogleModelsPaginatesNormalizesAndSorts(t *testing.T) {
	withModelListTokenStub(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1beta1/publishers/google/models", r.URL.Path)
		require.Equal(t, "Bearer model-list-token", r.Header.Get("Authorization"))
		require.Empty(t, r.Header.Get("x-goog-user-project"))
		require.Equal(t, "false", r.URL.Query().Get("listAllVersions"))
		if r.URL.Query().Get("pageToken") == "" {
			_, _ = w.Write([]byte(`{
                "publisherModels":[
                    {"name":"publishers/google/models/gemini-z"},
                    {"name":"publishers/anthropic/models/claude-ignore"},
                    {"name":"publishers/google/models/gemini-2.5-flash-001"}
                ],
                "nextPageToken":"page-2"
            }`))
			return
		}
		require.Equal(t, "page-2", r.URL.Query().Get("pageToken"))
		_, _ = w.Write([]byte(`{
            "publisherModels":[
                {"name":"publishers/google/models/gemini-a","versionId":"7"},
                {"name":"publishers/google/models/gemini-z"},
                {"name":"publishers/google/models/nested/model"}
            ]
        }`))
	}))
	defer server.Close()

	models, err := FetchGoogleModels(
		context.Background(),
		server.URL,
		modelListCredentialJSON(t),
		`{"default":"global"}`,
		"",
	)
	require.NoError(t, err)
	require.Equal(t, []string{"gemini-2.5-flash-001", "gemini-a", "gemini-z"}, models)
}

func TestResolveModelListRegions(t *testing.T) {
	tests := []struct {
		name  string
		other string
		want  []string
	}{
		{name: "empty falls back to global", other: "", want: []string{"global"}},
		{name: "plain string stays single region", other: "us-central1", want: []string{"us-central1"}},
		{name: "json map returns distinct sorted regions", other: `{"default":"us-central1","gemini-x":"us-east1","gemini-y":"us-central1"}`, want: []string{"us-central1", "us-east1"}},
		{name: "empty map falls back to global", other: `{}`, want: []string{"global"}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := ResolveModelListRegions(test.other)
			require.NoError(t, err)
			require.Equal(t, test.want, got)
		})
	}
}

func TestResolveModelListRegionsRejectsInvalidEntries(t *testing.T) {
	for _, other := range []string{
		`not-a-region!`,
		`{"default":"us-central1","gemini-x": 1}`,
		`{"default":"UPPER-CASE"}`,
		`{broken`,
	} {
		_, err := ResolveModelListRegions(other)
		require.Error(t, err, other)
	}
}

func TestFetchGoogleModelsMergesModelsAcrossRegions(t *testing.T) {
	withModelListTokenStub(t)
	requestCount := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, "/v1beta1/publishers/google/models", r.URL.Path)
		requestCount++
		_, _ = w.Write([]byte(fmt.Sprintf(`{
            "publisherModels":[
                {"name":"publishers/google/models/gemini-shared"},
                {"name":"publishers/google/models/gemini-region-%d"}
            ]
        }`, requestCount)))
	}))
	defer server.Close()

	models, err := FetchGoogleModels(
		context.Background(),
		server.URL,
		modelListCredentialJSON(t),
		`{"default":"us-central1","gemini-x":"us-east1"}`,
		"",
	)
	require.NoError(t, err)
	require.Equal(t, 2, requestCount)
	require.Equal(t, []string{"gemini-region-1", "gemini-region-2", "gemini-shared"}, models)
}

func TestFetchGoogleModelsRepeatedPageTokenPerRegionIsIndependent(t *testing.T) {
	withModelListTokenStub(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
            "publisherModels":[{"name":"publishers/google/models/gemini-a"}],
            "nextPageToken":"same-token"
        }`))
	}))
	defer server.Close()

	_, err := FetchGoogleModels(context.Background(), server.URL, modelListCredentialJSON(t),
		`{"default":"us-central1","gemini-x":"us-east1"}`, "")
	require.ErrorContains(t, err, "region us-central1")
	require.ErrorContains(t, err, "repeated page token")
}

func TestFetchGoogleModelsRejectsRepeatedPageToken(t *testing.T) {
	withModelListTokenStub(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{
            "publisherModels":[{"name":"publishers/google/models/gemini-a"}],
            "nextPageToken":"same-token"
        }`))
	}))
	defer server.Close()

	_, err := FetchGoogleModels(context.Background(), server.URL, modelListCredentialJSON(t), "global", "")
	require.ErrorContains(t, err, "repeated page token")
}

func TestFetchGoogleModelsReturnsSanitizedUpstreamErrors(t *testing.T) {
	withModelListTokenStub(t)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":{"code":403,"message":"permission denied","status":"PERMISSION_DENIED"}}`))
	}))
	defer server.Close()

	_, err := FetchGoogleModels(context.Background(), server.URL, modelListCredentialJSON(t), "global", "")
	require.ErrorContains(t, err, "HTTP 403: permission denied")
}

func TestFetchGoogleModelsRejectsEmptyOrMalformedResponses(t *testing.T) {
	withModelListTokenStub(t)
	for _, test := range []struct {
		name string
		body string
		want string
	}{
		{name: "empty", body: `{"publisherModels":[]}`, want: "returned no Google publisher models"},
		{name: "malformed", body: `{`, want: "failed to decode Vertex model list response"},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(test.body))
			}))
			defer server.Close()
			_, err := FetchGoogleModels(context.Background(), server.URL, modelListCredentialJSON(t), "global", "")
			require.ErrorContains(t, err, test.want)
		})
	}
}
