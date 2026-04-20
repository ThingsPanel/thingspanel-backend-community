package service

import (
	"testing"

	"project/internal/model"
)

func TestShouldSkipMarketCheck(t *testing.T) {
	tests := []struct {
		name string
		req  *model.SuperAdminInitReq
		want bool
	}{
		{
			name: "nil req",
			req:  nil,
			want: false,
		},
		{
			name: "not returned from market",
			req: &model.SuperAdminInitReq{
				Email:            "user@example.com",
				MarketRegistered: false,
				MarketEmail:      "user@example.com",
			},
			want: false,
		},
		{
			name: "returned with matching email",
			req: &model.SuperAdminInitReq{
				Email:            "user@example.com",
				MarketRegistered: true,
				MarketEmail:      "user@example.com",
			},
			want: true,
		},
		{
			name: "returned with case-insensitive matching email",
			req: &model.SuperAdminInitReq{
				Email:            "User@Example.com",
				MarketRegistered: true,
				MarketEmail:      "user@example.com",
			},
			want: true,
		},
		{
			name: "returned with mismatch email",
			req: &model.SuperAdminInitReq{
				Email:            "user@example.com",
				MarketRegistered: true,
				MarketEmail:      "other@example.com",
			},
			want: false,
		},
		{
			name: "returned without market email",
			req: &model.SuperAdminInitReq{
				Email:            "user@example.com",
				MarketRegistered: true,
			},
			want: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := shouldSkipMarketCheck(tt.req)
			if got != tt.want {
				t.Fatalf("expected %v, got %v", tt.want, got)
			}
		})
	}
}
