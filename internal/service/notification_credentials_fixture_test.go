package service

import "context"

type fixtureSourceCredentials struct{ source, projection string }

func (f fixtureSourceCredentials) Credential(_ context.Context, tenant, purpose string) (string, error) {
	if purpose == "projection" {
		return f.projection, nil
	}
	return f.source, nil
}
func (f fixtureSourceCredentials) Invalidate(string, string, string) {}
