package admin

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Wei-Shaw/sub2api/internal/service"
	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/require"
)

type dataResponse struct {
	Code int         `json:"code"`
	Data dataPayload `json:"data"`
}

type searchResponse struct {
	Code int         `json:"code"`
	Data searchBlock `json:"data"`
}

type accountIDsTestResponse struct {
	Code int                 `json:"code"`
	Data accountIDsTestBlock `json:"data"`
}

type accountIDsTestBlock struct {
	IDs   []int64 `json:"ids"`
	Total int     `json:"total"`
}

type dataPayload struct {
	Type     string        `json:"type"`
	Version  int           `json:"version"`
	Proxies  []dataProxy   `json:"proxies"`
	Accounts []dataAccount `json:"accounts"`
}

type dataProxy struct {
	ProxyKey string `json:"proxy_key"`
	Name     string `json:"name"`
	Protocol string `json:"protocol"`
	Host     string `json:"host"`
	Port     int    `json:"port"`
	Username string `json:"username"`
	Password string `json:"password"`
	Status   string `json:"status"`
}

type dataAccount struct {
	Name        string         `json:"name"`
	Platform    string         `json:"platform"`
	Type        string         `json:"type"`
	Credentials map[string]any `json:"credentials"`
	Extra       map[string]any `json:"extra"`
	ProxyKey    *string        `json:"proxy_key"`
	Concurrency int            `json:"concurrency"`
	Priority    int            `json:"priority"`
}

type searchBlock struct {
	AccountCandidates int               `json:"account_candidates"`
	AccountMatched    int               `json:"account_matched"`
	AccountFailed     int               `json:"account_failed"`
	Accounts          []searchAccount   `json:"accounts"`
	Duplicates        []duplicateBlock  `json:"duplicates"`
	Errors            []DataImportError `json:"errors"`
}

type duplicateBlock struct {
	Reason      string          `json:"reason"`
	IdentityKey string          `json:"identity_key"`
	Accounts    []searchAccount `json:"accounts"`
}

type searchAccount struct {
	ID       int64  `json:"id"`
	Name     string `json:"name"`
	Platform string `json:"platform"`
	Type     string `json:"type"`
}

func setupAccountDataRouter() (*gin.Engine, *stubAdminService) {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	adminSvc := newStubAdminService()

	h := NewAccountHandler(
		adminSvc,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
		nil,
	)

	router.GET("/api/v1/admin/accounts/data", h.ExportData)
	router.GET("/api/v1/admin/accounts/ids", h.ListIDs)
	router.GET("/api/v1/admin/accounts/duplicates", h.CheckDuplicates)
	router.POST("/api/v1/admin/accounts/data/search", h.SearchData)
	router.POST("/api/v1/admin/accounts/data", h.ImportData)
	return router, adminSvc
}

func TestListAccountIDsUsesCurrentFilters(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	adminSvc.accounts = []service.Account{{ID: 21}, {ID: 22}}

	w := httptest.NewRecorder()
	req, _ := http.NewRequest("GET", "/api/v1/admin/accounts/ids?platform=openai&type=oauth&status=active&group=ungrouped&privacy_mode=blocked&search=keyword", nil)
	router.ServeHTTP(w, req)

	require.Equal(t, http.StatusOK, w.Code)
	var resp accountIDsTestResponse
	require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
	require.Equal(t, []int64{21, 22}, resp.Data.IDs)
	require.Equal(t, 2, resp.Data.Total)
	require.Equal(t, 1, adminSvc.lastListAccountIDs.calls)
	require.Equal(t, "openai", adminSvc.lastListAccountIDs.platform)
	require.Equal(t, "oauth", adminSvc.lastListAccountIDs.accountType)
	require.Equal(t, "active", adminSvc.lastListAccountIDs.status)
	require.Equal(t, service.AccountListGroupUngrouped, adminSvc.lastListAccountIDs.groupID)
	require.Equal(t, "blocked", adminSvc.lastListAccountIDs.privacyMode)
	require.Equal(t, "keyword", adminSvc.lastListAccountIDs.search)
}

func TestExportDataIncludesSecrets(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	proxyID := int64(11)
	adminSvc.proxies = []service.Proxy{
		{
			ID:       proxyID,
			Name:     "proxy",
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
			Username: "user",
			Password: "pass",
			Status:   service.StatusActive,
		},
		{
			ID:       12,
			Name:     "orphan",
			Protocol: "https",
			Host:     "10.0.0.1",
			Port:     443,
			Username: "o",
			Password: "p",
			Status:   service.StatusActive,
		},
	}
	adminSvc.accounts = []service.Account{
		{
			ID:          21,
			Name:        "account",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Credentials: map[string]any{"token": "secret"},
			Extra:       map[string]any{"note": "x"},
			ProxyID:     &proxyID,
			Concurrency: 3,
			Priority:    50,
			Status:      service.StatusDisabled,
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/data", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Empty(t, resp.Data.Type)
	require.Equal(t, 0, resp.Data.Version)
	require.Len(t, resp.Data.Proxies, 1)
	require.Equal(t, "pass", resp.Data.Proxies[0].Password)
	require.Len(t, resp.Data.Accounts, 1)
	require.Equal(t, "secret", resp.Data.Accounts[0].Credentials["token"])
}

func TestExportDataWithoutProxies(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	proxyID := int64(11)
	adminSvc.proxies = []service.Proxy{
		{
			ID:       proxyID,
			Name:     "proxy",
			Protocol: "http",
			Host:     "127.0.0.1",
			Port:     8080,
			Username: "user",
			Password: "pass",
			Status:   service.StatusActive,
		},
	}
	adminSvc.accounts = []service.Account{
		{
			ID:          21,
			Name:        "account",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeOAuth,
			Credentials: map[string]any{"token": "secret"},
			ProxyID:     &proxyID,
			Concurrency: 3,
			Priority:    50,
			Status:      service.StatusDisabled,
		},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/data?include_proxies=false", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Proxies, 0)
	require.Len(t, resp.Data.Accounts, 1)
	require.Nil(t, resp.Data.Accounts[0].ProxyKey)
}

func TestExportDataPassesAccountFiltersAndSort(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	adminSvc.accounts = []service.Account{
		{ID: 1, Name: "acc-1", Status: service.StatusActive},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/accounts/data?platform=openai&type=oauth&status=active&group=12&privacy_mode=blocked&search=keyword&sort_by=priority&sort_order=desc",
		nil,
	)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Equal(t, 1, adminSvc.lastListAccounts.calls)
	require.Equal(t, "openai", adminSvc.lastListAccounts.platform)
	require.Equal(t, "oauth", adminSvc.lastListAccounts.accountType)
	require.Equal(t, "active", adminSvc.lastListAccounts.status)
	require.Equal(t, int64(12), adminSvc.lastListAccounts.groupID)
	require.Equal(t, "blocked", adminSvc.lastListAccounts.privacyMode)
	require.Equal(t, "keyword", adminSvc.lastListAccounts.search)
	require.Equal(t, "priority", adminSvc.lastListAccounts.sortBy)
	require.Equal(t, "desc", adminSvc.lastListAccounts.sortOrder)
}

func TestExportDataSelectedIDsOverrideFilters(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(
		http.MethodGet,
		"/api/v1/admin/accounts/data?ids=1,2&platform=openai&search=keyword&sort_by=priority&sort_order=desc",
		nil,
	)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp dataResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Len(t, resp.Data.Accounts, 2)
	require.Equal(t, 0, adminSvc.lastListAccounts.calls)
}

func TestImportDataReusesProxyAndSkipsDefaultGroup(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	adminSvc.accounts = nil

	adminSvc.proxies = []service.Proxy{
		{
			ID:       1,
			Name:     "proxy",
			Protocol: "socks5",
			Host:     "1.2.3.4",
			Port:     1080,
			Username: "u",
			Password: "p",
			Status:   service.StatusActive,
		},
	}

	dataPayload := map[string]any{
		"data": map[string]any{
			"type":    dataType,
			"version": dataVersion,
			"proxies": []map[string]any{
				{
					"proxy_key": "socks5|1.2.3.4|1080|u|p",
					"name":      "proxy",
					"protocol":  "socks5",
					"host":      "1.2.3.4",
					"port":      1080,
					"username":  "u",
					"password":  "p",
					"status":    "active",
				},
			},
			"accounts": []map[string]any{
				{
					"name":        "acc",
					"platform":    service.PlatformOpenAI,
					"type":        service.AccountTypeOAuth,
					"credentials": map[string]any{"token": "x"},
					"proxy_key":   "socks5|1.2.3.4|1080|u|p",
					"concurrency": 3,
					"priority":    50,
				},
			},
		},
		"skip_default_group_bind": true,
	}

	body, _ := json.Marshal(dataPayload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	require.Len(t, adminSvc.createdProxies, 0)
	require.Len(t, adminSvc.createdAccounts, 1)
	require.True(t, adminSvc.createdAccounts[0].SkipDefaultGroupBind)
}

func TestImportDataSkipsDuplicateAccountNames(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	adminSvc.accounts = []service.Account{
		{
			ID:          101,
			Name:        "existing",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Credentials: map[string]any{"api_key": "sk-existing"},
			Status:      service.StatusActive,
		},
	}

	dataPayload := map[string]any{
		"data": map[string]any{
			"type":    dataType,
			"version": dataVersion,
			"proxies": []map[string]any{},
			"accounts": []map[string]any{
				{
					"name":        "existing",
					"platform":    service.PlatformOpenAI,
					"type":        service.AccountTypeAPIKey,
					"credentials": map[string]any{"api_key": "sk-different-existing"},
					"concurrency": 3,
					"priority":    50,
				},
				{
					"name":        "new",
					"platform":    service.PlatformOpenAI,
					"type":        service.AccountTypeAPIKey,
					"credentials": map[string]any{"api_key": "sk-same"},
					"concurrency": 3,
					"priority":    50,
				},
				{
					"name":        "new",
					"platform":    service.PlatformOpenAI,
					"type":        service.AccountTypeAPIKey,
					"credentials": map[string]any{"api_key": "sk-different-new"},
					"concurrency": 3,
					"priority":    50,
				},
			},
		},
		"skip_default_group_bind": true,
	}

	body, _ := json.Marshal(dataPayload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Code int              `json:"code"`
		Data DataImportResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, 1, resp.Data.AccountCreated)
	require.Equal(t, 2, resp.Data.AccountFailed)
	require.Len(t, adminSvc.createdAccounts, 1)
	require.Equal(t, "new", adminSvc.createdAccounts[0].Name)
	require.Len(t, resp.Data.Errors, 2)
	require.Contains(t, resp.Data.Errors[0].Message, "already exists")
	require.Contains(t, resp.Data.Errors[1].Message, "import payload")
}

func TestImportDataOverwriteExistingAccount(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	adminSvc.accounts = []service.Account{
		{
			ID:          101,
			Name:        "existing",
			Platform:    service.PlatformOpenAI,
			Type:        service.AccountTypeAPIKey,
			Credentials: map[string]any{"api_key": "sk-existing"},
			Status:      service.StatusActive,
		},
	}

	dataPayload := map[string]any{
		"data": map[string]any{
			"type":    dataType,
			"version": dataVersion,
			"proxies": []map[string]any{},
			"accounts": []map[string]any{
				{
					"name":        "existing",
					"platform":    service.PlatformOpenAI,
					"type":        service.AccountTypeAPIKey,
					"credentials": map[string]any{"api_key": "sk-imported"},
					"extra":       map[string]any{"base_rpm": 20},
					"concurrency": 6,
					"priority":    70,
				},
			},
		},
		"skip_default_group_bind": true,
		"update_existing":         true,
	}

	body, _ := json.Marshal(dataPayload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp struct {
		Code int              `json:"code"`
		Data DataImportResult `json:"data"`
	}
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, 0, resp.Data.AccountCreated)
	require.Equal(t, 1, resp.Data.AccountUpdated)
	require.Equal(t, 0, resp.Data.AccountFailed)
	require.Len(t, adminSvc.createdAccounts, 0)
	require.Equal(t, []int64{101}, adminSvc.updatedAccountIDs)
	require.Len(t, adminSvc.updatedAccounts, 1)
	require.Equal(t, "existing", adminSvc.updatedAccounts[0].Name)
	require.Equal(t, service.AccountTypeAPIKey, adminSvc.updatedAccounts[0].Type)
	require.Equal(t, "sk-imported", adminSvc.updatedAccounts[0].Credentials["api_key"])
	require.Equal(t, 6, *adminSvc.updatedAccounts[0].Concurrency)
	require.Equal(t, 70, *adminSvc.updatedAccounts[0].Priority)
}

func TestSearchDataFindsExistingAccountsWithoutImporting(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	adminSvc.accounts = []service.Account{
		{ID: 101, Name: "acc", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Credentials: map[string]any{"email": "same@example.com"}, Status: service.StatusActive},
		{ID: 102, Name: "other", Platform: service.PlatformOpenAI, Type: service.AccountTypeOAuth, Status: service.StatusActive},
	}

	dataPayload := map[string]any{
		"data": map[string]any{
			"type":    dataType,
			"version": dataVersion,
			"proxies": []map[string]any{
				{
					"proxy_key": "socks5|1.2.3.4|1080|u|p",
					"name":      "proxy",
					"protocol":  "socks5",
					"host":      "1.2.3.4",
					"port":      1080,
					"username":  "u",
					"password":  "p",
					"status":    "active",
				},
			},
			"accounts": []map[string]any{
				{
					"name":        "acc",
					"platform":    service.PlatformOpenAI,
					"type":        service.AccountTypeOAuth,
					"credentials": map[string]any{"email": "same@example.com", "token": "x"},
					"proxy_key":   "socks5|1.2.3.4|1080|u|p",
					"concurrency": 3,
					"priority":    50,
				},
			},
		},
	}

	body, _ := json.Marshal(dataPayload)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/admin/accounts/data/search", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp searchResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, 1, resp.Data.AccountCandidates)
	require.Equal(t, 1, resp.Data.AccountMatched)
	require.Len(t, resp.Data.Accounts, 1)
	require.Equal(t, int64(101), resp.Data.Accounts[0].ID)
	require.Equal(t, "acc", resp.Data.Accounts[0].Name)
	require.Len(t, adminSvc.createdAccounts, 0)
	require.Len(t, adminSvc.createdProxies, 0)
}

func TestCheckDuplicatesFindsDuplicateAccountNames(t *testing.T) {
	router, adminSvc := setupAccountDataRouter()
	adminSvc.accounts = []service.Account{
		{ID: 101, Name: "same", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-a"}, Status: service.StatusActive},
		{ID: 102, Name: " SAME ", Platform: service.PlatformGemini, Type: service.AccountTypeOAuth, Credentials: map[string]any{"token": "b"}, Status: service.StatusActive},
		{ID: 103, Name: "other", Platform: service.PlatformOpenAI, Type: service.AccountTypeAPIKey, Credentials: map[string]any{"api_key": "sk-a"}, Status: service.StatusActive},
	}

	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/admin/accounts/duplicates", nil)
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusOK, rec.Code)

	var resp searchResponse
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &resp))
	require.Equal(t, 0, resp.Code)
	require.Equal(t, 3, resp.Data.AccountCandidates)
	require.Equal(t, 2, resp.Data.AccountMatched)
	require.Len(t, resp.Data.Duplicates, 1)
	require.Equal(t, "name", resp.Data.Duplicates[0].Reason)
	require.Len(t, resp.Data.Duplicates[0].Accounts, 2)
}
