package proxmox

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	pxapi "github.com/Telmate/proxmox-api-go/proxmox"
	"github.com/google/uuid"
	"github.com/hashicorp/vault/sdk/framework"
	"github.com/hashicorp/vault/sdk/logical"
)

const (
	proxmoxTokenType = "proxmox_api_token"
)

type proxmoxToken struct {
	TokenID string `json:"token_id"`
	Secret  string `json:"secret"`
}

func (b *proxmoxBackend) proxmoxToken() *framework.Secret {
	return &framework.Secret{
		Type: proxmoxTokenType,
		Fields: map[string]*framework.FieldSchema{
			"token_id": {
				Type:        framework.TypeString,
				Description: "The user-specific token identifier (excl user and realm)",
			},
			"secret": {
				Type:        framework.TypeString,
				Description: "The secret API token value used for authentication",
			},
		},
		Revoke: b.tokenRevoke,
		Renew:  b.tokenRenew,
	}
}

func (b *proxmoxBackend) tokenRevoke(ctx context.Context, req *logical.Request, d *framework.FieldData) (*logical.Response, error) {
	client, err := b.getClient(ctx, req.Storage)
	if err != nil {
		return nil, fmt.Errorf("error getting client: %w", err)
	}

	tokenID := ""
	tokenIDRaw, ok := req.Secret.InternalData["token_id"]
	if ok {
		tokenID, ok = tokenIDRaw.(string)
		if !ok {
			return nil, fmt.Errorf("invalid value for token in secret internal data")
		}
	}

	roleRaw, ok := req.Secret.InternalData["role"]
	if !ok {
		return nil, fmt.Errorf("secret is missing role internal data")
	}

	role := roleRaw.(string)
	roleEntry, err := b.getRole(ctx, req.Storage, role)
	if err != nil {
		return nil, fmt.Errorf("error retrieving role: %w", err)
	}
	if roleEntry == nil {
		return nil, errors.New("error retrieving role: role is nil")
	}

	if err := deleteToken(client, roleEntry.User, roleEntry.Realm, tokenID); err != nil {
		return nil, fmt.Errorf("error revoking user token: %w", err)
	}

	return nil, nil
}

func (b *proxmoxBackend) tokenRenew(ctx context.Context, req *logical.Request, d *framework.FieldData) (*logical.Response, error) {
	roleRaw, ok := req.Secret.InternalData["role"]
	if !ok {
		return nil, fmt.Errorf("secret is missing role internal data")
	}

	role := roleRaw.(string)
	roleEntry, err := b.getRole(ctx, req.Storage, role)
	if err != nil {
		return nil, fmt.Errorf("error retrieving role: %w", err)
	}
	if roleEntry == nil {
		return nil, errors.New("error retrieving role: role is nil")
	}

	// Extending the Vault lease alone is not enough: the underlying Proxmox token
	// carries its own absolute expiry set at creation time, so without pushing the
	// new expiry to Proxmox the token would stop working mid-lease. Keep them in sync.
	if roleEntry.TTL > 0 {
		tokenID := ""
		if tokenIDRaw, ok := req.Secret.InternalData["token_id"]; ok {
			tokenID, ok = tokenIDRaw.(string)
			if !ok {
				return nil, fmt.Errorf("invalid value for token in secret internal data")
			}
		}

		client, err := b.getClient(ctx, req.Storage)
		if err != nil {
			return nil, fmt.Errorf("error getting client: %w", err)
		}

		expire := time.Now().Add(roleEntry.TTL).Unix()
		if err := updateTokenExpire(client, roleEntry.User, roleEntry.Realm, tokenID, expire, roleEntry.PrivilegeSeparation); err != nil {
			return nil, fmt.Errorf("error updating token expiry on renew: %w", err)
		}
	}

	resp := &logical.Response{Secret: req.Secret}

	if roleEntry.TTL > 0 {
		resp.Secret.TTL = roleEntry.TTL
	}

	if roleEntry.MaxTTL > 0 {
		resp.Secret.MaxTTL = roleEntry.MaxTTL
	}

	return resp, nil
}

func createToken(c *proxmoxClient, user string, realm string, expire int64, privsep bool) (*proxmoxToken, error) {
	if len(user) == 0 {
		return nil, errors.New("error creating token: no user provided")
	}

	if len(realm) == 0 {
		return nil, errors.New("error creating token: no realm provided")
	}

	rawTokenId := uuid.New().String()
	// Proxmox API wants token IDs to start with a latter (regexp (?^:[A-Za-z][A-Za-z0-9\.\-_]+)) so lets remap the entire thing
	tokenId := strings.NewReplacer("0", "g", "1", "h", "2", "i", "3", "j", "4", "k", "5", "l", "6", "m", "7", "n", "8", "o", "9", "p").Replace(rawTokenId)

	u, err := pxapi.NewConfigUserFromApi(pxapi.UserID{Name: user, Realm: realm}, c.Client)
	if err != nil {
		return nil, fmt.Errorf("error when setting up API user: %w", err)
	}

	secret, err := u.CreateApiToken(c.Client, pxapi.ApiToken{TokenId: tokenId, Comment: "Managed by Vault", Expire: expire, Privsep: privsep})
	if err != nil {
		return nil, fmt.Errorf("error from API when creating token: %w", err)
	}

	return &proxmoxToken{
		TokenID: tokenId,
		Secret:  secret,
	}, nil
}

// grantTokenACL grants the given Proxmox roles to a privilege-separated API token
// at the given object path via PUT /access/acl. The token is identified by its full
// id (user@realm!tokenid). When the token is later deleted Proxmox removes the
// matching ACL entries automatically, so revocation needs no explicit cleanup.
func grantTokenACL(c *proxmoxClient, tokenIDFull string, path string, roles []string, propagate bool) error {
	if path == "" {
		return errors.New("error granting token ACL: no path provided")
	}
	if len(roles) == 0 {
		return errors.New("error granting token ACL: no roles provided")
	}

	propagateValue := 0
	if propagate {
		propagateValue = 1
	}

	params := map[string]interface{}{
		"path":      path,
		"roles":     strings.Join(roles, ","),
		"tokens":    tokenIDFull,
		"propagate": propagateValue,
	}

	return c.Put(params, "/access/acl")
}

// updateTokenExpire pushes a new absolute expiry to an existing Proxmox API token.
// privsep must reflect the token's current privilege-separation setting, because the
// underlying Proxmox update always sends privsep and would otherwise reset it.
func updateTokenExpire(c *proxmoxClient, user string, realm string, tokenID string, expire int64, privsep bool) error {
	u, err := pxapi.NewConfigUserFromApi(pxapi.UserID{Name: user, Realm: realm}, c.Client)
	if err != nil {
		return err
	}

	return u.UpdateApiToken(c.Client, pxapi.ApiToken{TokenId: tokenID, Comment: "Managed by Vault", Expire: expire, Privsep: privsep})
}

func deleteToken(c *proxmoxClient, user string, realm string, tokenID string) error {
	u, err := pxapi.NewConfigUserFromApi(pxapi.UserID{Name: user, Realm: realm}, c.Client)
	if err != nil {
		return err
	}

	err = u.DeleteApiToken(c.Client, pxapi.ApiToken{TokenId: tokenID})
	if err != nil {
		return err
	}

	return nil
}
