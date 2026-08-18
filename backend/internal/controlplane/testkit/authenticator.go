package testkit

import (
	"context"

	"rock/internal/controlplane/authn"
)

type Authenticator struct {
	Token     string
	Principal authn.Principal
	Err       error
}

func (a Authenticator) Authenticate(_ context.Context, token string) (authn.Principal, error) {
	if a.Err != nil || token != a.Token {
		if a.Err != nil {
			return authn.Principal{}, a.Err
		}
		return authn.Principal{}, authn.ErrInvalidToken
	}
	return a.Principal, nil
}
