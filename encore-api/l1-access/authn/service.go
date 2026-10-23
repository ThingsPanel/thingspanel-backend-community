package authn

import "encore.app/internal/authcore"

//encore:service
type Service struct{}

func initService() (*Service, error) {
	if authcore.Enabled() {
		if _, err := authcore.Load(); err != nil {
			return nil, err
		}
	}
	return &Service{}, nil
}
