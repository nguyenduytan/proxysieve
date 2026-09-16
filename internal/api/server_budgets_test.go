package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	internalbudget "github.com/nguyenduytan/proxysieve/internal/budget"
	publicbudget "github.com/nguyenduytan/proxysieve/pkg/budget"
)

func TestBudgetStatuses(t *testing.T) {
	manager, err := internalbudget.New([]publicbudget.Config{{ID: "system", Name: "System", Limit: 100, Hard: true, Action: publicbudget.ActionReject}}, 100)
	if err != nil {
		t.Fatal(err)
	}
	server := &Server{budgets: manager}
	request := httptest.NewRequest(http.MethodGet, "/api/v1/budgets", nil)
	response := httptest.NewRecorder()
	server.budgetStatuses(response, request)
	var body struct {
		Items []internalbudget.Status `json:"items"`
	}
	if response.Code != http.StatusOK || json.Unmarshal(response.Body.Bytes(), &body) != nil || len(body.Items) != 1 || body.Items[0].ID != "system" || body.Items[0].Remaining != 100 {
		t.Fatal(response.Code, response.Body.String())
	}

	request = httptest.NewRequest(http.MethodPost, "/api/v1/budgets", nil)
	response = httptest.NewRecorder()
	server.budgetStatuses(response, request)
	if response.Code != http.StatusMethodNotAllowed {
		t.Fatal(response.Code, response.Body.String())
	}
}

func TestBudgetStatusesAreEmptyWithoutConfiguration(t *testing.T) {
	response := httptest.NewRecorder()
	(&Server{}).budgetStatuses(response, httptest.NewRequest(http.MethodGet, "/api/v1/budgets", nil))
	if response.Code != http.StatusOK || response.Body.String() != "{\"items\":[]}\n" {
		t.Fatal(response.Code, response.Body.String())
	}
}
