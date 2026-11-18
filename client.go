package proxmox

import (
	"crypto/tls"
	"errors"
	"fmt"

	pxapi "github.com/Telmate/proxmox-api-go/proxmox"
)

type proxmoxClient struct {
	*pxapi.Client
}

func newClient(config *proxmoxConfig) (*proxmoxClient, error) {
	if config == nil {
		return nil, errors.New("client configuration was nil")
	}

	if config.ApiTokenID == "" {
		return nil, errors.New("client api token was not defined")
	}

	fullToken := fmt.Sprintf("%s@%s!%s", config.User, config.Realm, config.ApiTokenID)

	tlsConfig := &tls.Config{
		InsecureSkipVerify: true, //nolint:gosec // explicit opt-in via the insecure_skip_tls_verify config field; nil config is used otherwise
	}
	if !config.SkipCertValidation {
		tlsConfig = nil
	}

	c, err := pxapi.NewClient(config.ApiURL, nil, config.HTTPHeaders, tlsConfig, config.ProxyServer, int(config.TaskTimeout.Seconds()))
	if err != nil {
		return nil, err
	}

	c.SetAPIToken(fullToken, config.ApiTokenSecret)

	return &proxmoxClient{c}, nil
}
