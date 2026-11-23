package fusionhttp

import (
	"context"
	"net/http/httptest"
	"strings"
	"testing"

	"ikhnos/internal/fusionapi"
)

type extrasWithOrg struct{ extrasWith }

func (extrasWithOrg) OrgName(context.Context) (fusionapi.OrgName, error) {
	return fusionapi.OrgName{ID: "default", Name: "Edge lab"}, nil
}

func TestTheStatusNamesTheOrganisation(t *testing.T) {
	g := newGuard(fakeAccounts{}, fakeTokens{})
	g.Extras = func() fusionapi.Extras { return extrasWithOrg{} }
	w := httptest.NewRecorder()
	g.Status(w, httptest.NewRequest("GET", "/status", nil), &fusionapi.Client{}, Caller{Kind: "user", ID: "u", Scope: fusionapi.AllSignals()})
	if !strings.Contains(w.Body.String(), `"organisation":{"id":"default","name":"Edge lab"}`) {
		t.Fatalf("%d %s", w.Code, w.Body.String())
	}
}
