package notification

import (
	"context"
	"net/url"
	"strconv"

	"encore.app/l3-core/notification/contract"
)

type notificationGroupPage struct {
	Items    []contract.GroupView `json:"items"`
	Page     int                  `json:"page"`
	PageSize int                  `json:"pageSize"`
	Total    int                  `json:"total"`
}

func parseNotificationGroupPage(query url.Values) (int, int, error) {
	for key, values := range query {
		if (key != "page" && key != "pageSize") || len(values) != 1 {
			return 0, 0, ErrManagementInvalid
		}
	}
	page, pageSize := 1, 20
	if raw := query.Get("page"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 {
			return 0, 0, ErrManagementInvalid
		}
		page = value
	}
	if raw := query.Get("pageSize"); raw != "" {
		value, err := strconv.Atoi(raw)
		if err != nil || value < 1 || value > 100 {
			return 0, 0, ErrManagementInvalid
		}
		pageSize = value
	}
	if page > int(^uint(0)>>1)/pageSize {
		return 0, 0, ErrManagementInvalid
	}
	return page, pageSize, nil
}

func (s *Store) GetGroupRevision(ctx context.Context, tenantID, groupID string, revision int64) (contract.GroupView, error) {
	if revision < 1 {
		return contract.GroupView{}, storeErr(StoreErrorInvalid)
	}
	return s.getGroupVersion(ctx, tenantID, groupID, revision)
}

func loadNotificationGroupPage(ctx context.Context, store groupQueryStore, tenant string, page, pageSize int) (notificationGroupPage, error) {
	result := notificationGroupPage{Items: []contract.GroupView{}, Page: page, PageSize: pageSize}
	if store == nil || tenant == "" || page < 1 || pageSize < 1 || pageSize > 100 {
		return result, storeErr(StoreErrorInvalid)
	}
	groups, err := store.ListGroups(ctx, tenant)
	if err != nil {
		return result, err
	}
	result.Total = len(groups)
	start := (page - 1) * pageSize
	if start >= len(groups) {
		return result, nil
	}
	end := start + pageSize
	if end > len(groups) {
		end = len(groups)
	}
	result.Items = make([]contract.GroupView, 0, end-start)
	for _, group := range groups[start:end] {
		group, err = attachGroupProjections(ctx, store, tenant, group)
		if err != nil {
			return notificationGroupPage{Items: []contract.GroupView{}, Page: page, PageSize: pageSize}, err
		}
		result.Items = append(result.Items, group)
	}
	return result, nil
}

type groupQueryStore interface {
	ListGroups(context.Context, string) ([]contract.GroupView, error)
	ListGroupProjections(context.Context, string, string) ([]contract.SourceGroupProjection, error)
}

func attachGroupProjections(ctx context.Context, store groupQueryStore, tenant string, group contract.GroupView) (contract.GroupView, error) {
	projections, err := store.ListGroupProjections(ctx, tenant, group.ID)
	if err != nil {
		return contract.GroupView{}, err
	}
	group.MigrationState = "native"
	group.SourceProjections = projections
	if group.SourceProjections == nil {
		group.SourceProjections = []contract.SourceGroupProjection{}
	}
	return group, nil
}
