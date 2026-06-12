package proxmox

import (
	"context"
	"strconv"
	"testing"

	"github.com/hashicorp/vault/sdk/logical"
	"github.com/stretchr/testify/require"
)

const (
	roleName   = "my_proxmox_role"
	testUser   = "Alice"
	testRealm  = "pam"
	testTTL    = int64(120)
	testMaxTTL = int64(3600)
)

func TestUserRole(t *testing.T) {
	b, s := getTestBackend(t)

	t.Run("List All Roles", func(t *testing.T) {
		for i := 1; i <= 10; i++ {
			_, err := testTokenRoleCreate(t, b, s,
				roleName+strconv.Itoa(i),
				map[string]interface{}{
					"user":    testUser,
					"realm":   testRealm,
					"ttl":     testTTL,
					"max_ttl": testMaxTTL,
				})
			require.NoError(t, err)
		}

		resp, err := testTokenRoleList(t, b, s)
		require.NoError(t, err)
		require.Len(t, resp.Data["keys"].([]string), 10)
	})

	t.Run("Create User Role - pass", func(t *testing.T) {
		resp, err := testTokenRoleCreate(t, b, s, roleName, map[string]interface{}{
			"user":    user,
			"realm":   realm,
			"ttl":     testTTL,
			"max_ttl": testMaxTTL,
		})

		require.Nil(t, err)
		require.Nil(t, resp.Error())
		require.Nil(t, resp)
	})

	t.Run("Read User Role", func(t *testing.T) {
		resp, err := testTokenRoleRead(t, b, s)

		require.Nil(t, err)
		require.Nil(t, resp.Error())
		require.NotNil(t, resp)
		require.Equal(t, user, resp.Data["user"])
		require.Equal(t, realm, resp.Data["realm"])
	})
	t.Run("Update User Role", func(t *testing.T) {
		resp, err := testTokenRoleUpdate(t, b, s, map[string]interface{}{
			"user":    "Claire",
			"ttl":     "1m",
			"max_ttl": "5h",
		})

		require.Nil(t, err)
		require.Nil(t, resp.Error())
		require.Nil(t, resp)
	})

	t.Run("Re-read User Role", func(t *testing.T) {
		resp, err := testTokenRoleRead(t, b, s)

		require.Nil(t, err)
		require.Nil(t, resp.Error())
		require.NotNil(t, resp)
		require.Equal(t, "Claire", resp.Data["user"])
		require.Equal(t, realm, resp.Data["realm"])
	})

	t.Run("Delete User Role", func(t *testing.T) {
		_, err := testTokenRoleDelete(t, b, s)

		require.NoError(t, err)
	})
}

func TestPrivilegeSeparationRole(t *testing.T) {
	b, s := getTestBackend(t)

	t.Run("privsep without acl is rejected", func(t *testing.T) {
		resp, err := testTokenRoleCreate(t, b, s, "privsep-no-acl", map[string]interface{}{
			"user":                 user,
			"realm":                realm,
			"privilege_separation": true,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		require.True(t, resp.IsError())
	})

	t.Run("privsep with acl is stored", func(t *testing.T) {
		resp, err := testTokenRoleCreate(t, b, s, "privsep-ok", map[string]interface{}{
			"user":                 user,
			"realm":                realm,
			"privilege_separation": true,
			"acl_path":             "/vms/100",
			"acl_roles":            "PVEVMAdmin,PVEVMUser",
		})

		require.NoError(t, err)
		require.Nil(t, resp)
	})

	t.Run("privsep role reads back", func(t *testing.T) {
		resp, err := b.HandleRequest(context.Background(), &logical.Request{
			Operation: logical.ReadOperation,
			Path:      "role/privsep-ok",
			Storage:   s,
		})

		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, true, resp.Data["privilege_separation"])
		require.Equal(t, "/vms/100", resp.Data["acl_path"])
		require.Equal(t, []string{"PVEVMAdmin", "PVEVMUser"}, resp.Data["acl_roles"])
		// acl_propagate defaults to true when omitted
		require.Equal(t, true, resp.Data["acl_propagate"])
	})

	t.Run("default role is not privilege separated", func(t *testing.T) {
		_, err := testTokenRoleCreate(t, b, s, "no-privsep", map[string]interface{}{
			"user":  user,
			"realm": realm,
		})
		require.NoError(t, err)

		resp, err := b.HandleRequest(context.Background(), &logical.Request{
			Operation: logical.ReadOperation,
			Path:      "role/no-privsep",
			Storage:   s,
		})
		require.NoError(t, err)
		require.NotNil(t, resp)
		require.Equal(t, false, resp.Data["privilege_separation"])
	})
}

// Utility function to create a role while, returning any response (including errors)
func testTokenRoleCreate(t *testing.T, b *proxmoxBackend, s logical.Storage, name string, d map[string]interface{}) (*logical.Response, error) {
	t.Helper()
	resp, err := b.HandleRequest(context.Background(), &logical.Request{
		Operation: logical.CreateOperation,
		Path:      "role/" + name,
		Data:      d,
		Storage:   s,
	})

	if err != nil {
		return nil, err
	}

	return resp, nil
}

// Utility function to update a role while, returning any response (including errors)
func testTokenRoleUpdate(t *testing.T, b *proxmoxBackend, s logical.Storage, d map[string]interface{}) (*logical.Response, error) {
	t.Helper()
	resp, err := b.HandleRequest(context.Background(), &logical.Request{
		Operation: logical.UpdateOperation,
		Path:      "role/" + roleName,
		Data:      d,
		Storage:   s,
	})

	if err != nil {
		return nil, err
	}

	if resp != nil && resp.IsError() {
		t.Fatal(resp.Error())
	}
	return resp, nil
}

// Utility function to read a role and return any errors
func testTokenRoleRead(t *testing.T, b *proxmoxBackend, s logical.Storage) (*logical.Response, error) {
	t.Helper()
	return b.HandleRequest(context.Background(), &logical.Request{
		Operation: logical.ReadOperation,
		Path:      "role/" + roleName,
		Storage:   s,
	})
}

// Utility function to list roles and return any errors
func testTokenRoleList(t *testing.T, b *proxmoxBackend, s logical.Storage) (*logical.Response, error) {
	t.Helper()
	return b.HandleRequest(context.Background(), &logical.Request{
		Operation: logical.ListOperation,
		Path:      "role/",
		Storage:   s,
	})
}

// Utility function to delete a role and return any errors
func testTokenRoleDelete(t *testing.T, b *proxmoxBackend, s logical.Storage) (*logical.Response, error) {
	t.Helper()
	return b.HandleRequest(context.Background(), &logical.Request{
		Operation: logical.DeleteOperation,
		Path:      "role/" + roleName,
		Storage:   s,
	})
}
