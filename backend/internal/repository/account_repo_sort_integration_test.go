//go:build integration

package repository

import (
	"github.com/Wei-Shaw/sub2api/internal/pkg/pagination"
	"github.com/Wei-Shaw/sub2api/internal/service"
)

func (s *AccountRepoSuite) TestList_DefaultSortByNameAsc() {
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "z-account"})
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "a-account"})

	accounts, _, err := s.repo.List(s.ctx, pagination.PaginationParams{Page: 1, PageSize: 10})
	s.Require().NoError(err)
	s.Require().Len(accounts, 2)
	s.Require().Equal("a-account", accounts[0].Name)
	s.Require().Equal("z-account", accounts[1].Name)
}

func (s *AccountRepoSuite) TestListWithFilters_SortByPriorityDesc() {
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "low-priority", Priority: 10})
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "high-priority", Priority: 90})

	accounts, _, err := s.repo.ListWithFilters(s.ctx, pagination.PaginationParams{
		Page:      1,
		PageSize:  10,
		SortBy:    "priority",
		SortOrder: "desc",
	}, "", "", "", "", 0, "")
	s.Require().NoError(err)
	s.Require().Len(accounts, 2)
	s.Require().Equal("high-priority", accounts[0].Name)
	s.Require().Equal("low-priority", accounts[1].Name)
}

func (s *AccountRepoSuite) TestListWithFilters_SortByCreatedAtDesc() {
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "first-created"})
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "second-created"})

	accounts, _, err := s.repo.ListWithFilters(s.ctx, pagination.PaginationParams{
		Page:      1,
		PageSize:  10,
		SortBy:    "created_at",
		SortOrder: "desc",
	}, "", "", "", "", 0, "")
	s.Require().NoError(err)
	s.Require().Len(accounts, 2)
	s.Require().Equal("second-created", accounts[0].Name)
	s.Require().Equal("first-created", accounts[1].Name)
}

func (s *AccountRepoSuite) TestListWithFilters_SortByUsageWindowRefreshedAtDesc() {
	mustCreateAccount(s.T(), s.client, &service.Account{
		Name:  "old-usage-window",
		Extra: map[string]any{"codex_usage_updated_at": "2026-03-07T10:00:00Z"},
	})
	mustCreateAccount(s.T(), s.client, &service.Account{
		Name:  "new-usage-window",
		Extra: map[string]any{"codex_usage_updated_at": "2026-03-08T10:00:00Z"},
	})
	mustCreateAccount(s.T(), s.client, &service.Account{Name: "missing-usage-window"})

	accounts, _, err := s.repo.ListWithFilters(s.ctx, pagination.PaginationParams{
		Page:      1,
		PageSize:  10,
		SortBy:    "usage_window_refreshed_at",
		SortOrder: "desc",
	}, "", "", "", "", 0, "")
	s.Require().NoError(err)
	s.Require().Len(accounts, 3)
	s.Require().Equal("new-usage-window", accounts[0].Name)
	s.Require().Equal("old-usage-window", accounts[1].Name)
	s.Require().Equal("missing-usage-window", accounts[2].Name)
}
