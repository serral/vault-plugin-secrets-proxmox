# Vault Plugin: Proxmox

This is a standalone backend plugin for use with [Hashicorp Vault](https://www.github.com/hashicorp/vault).
This plugin provides handling of Proxmox VE API tokens by Vault.

> This project is an independent fork of [mollstam/vault-plugin-secrets-proxmox](https://github.com/mollstam/vault-plugin-secrets-proxmox). It diverged after upstream commit `a97df68`. Credit for the original plugin goes to Tobias Mollstam. This fork is maintained separately and does not send changes upstream. It is released under the [Unlicense](LICENSE) (public domain), like the original.

By default minted tokens inherit all privileges of the user account they belong to. Tokens can optionally be created with Proxmox privilege separation (`privsep`), in which case they only receive the roles you grant them via an ACL on the role definition. See [Privilege separation](#privilege-separation) below.

## Getting Started

This is a [Vault plugin](https://www.vaultproject.io/docs/internals/plugins.html)
and is meant to work with Vault. This guide assumes you have already installed Vault
and have a basic understanding of how Vault works.

Otherwise, first read this guide on how to [get started with Vault](https://www.vaultproject.io/intro/getting-started/install.html).

To learn specifically about how plugins work, see documentation on [Vault plugins](https://www.vaultproject.io/docs/internals/plugins.html).

## Installation

### Option 1: download a release (Linux x86-64 only)

Each [release](https://github.com/serral/vault-plugin-secrets-proxmox/releases) has two files: the plugin binary and a `SHA256SUMS` file. The binary is a plain executable, not an archive. Releases are built for Linux on x86-64 only. For any other operating system or CPU, use option 2.

1. Download the binary and its checksum file, then verify them:
```sh
VERSION=v1.2.0
BASE=https://github.com/serral/vault-plugin-secrets-proxmox/releases/download/${VERSION}
curl -fsSLO ${BASE}/vault-plugin-secrets-proxmox_Linux_x86_64
curl -fsSLO ${BASE}/vault-plugin-secrets-proxmox_${VERSION}_SHA256SUMS
sha256sum -c vault-plugin-secrets-proxmox_${VERSION}_SHA256SUMS
chmod +x vault-plugin-secrets-proxmox_Linux_x86_64
```
2. Move the binary into your Vault `plugin_directory` and register it with its SHA256 hash. See the [Vault plugin docs](https://developer.hashicorp.com/vault/docs/plugins/plugin-architecture#plugin-registration) for more information.
```sh
mv vault-plugin-secrets-proxmox_Linux_x86_64 /path/to/vault/plugin_directory/
vault plugin register \
  -sha256="$(cut -d' ' -f1 vault-plugin-secrets-proxmox_${VERSION}_SHA256SUMS)" \
  -command=vault-plugin-secrets-proxmox_Linux_x86_64 \
  secret vault-plugin-secrets-proxmox
```

### Option 2: build from source

Building requires Go 1.25 or newer (see `go.mod`).

1. Clone this repository and change directory into the root.
2. For good measure, run some tests: `go test -v`.
3. Change directory into `cmd/vault-plugin-secrets-proxmox`
4. Build the plugin `go build` and then get the SHA256 hash of the binary.
5. Install the plugin and register it with the hash, see the [Vault plugin docs](https://developer.hashicorp.com/vault/docs/plugins/plugin-architecture#plugin-registration) for more information.

## Setup (Proxmox)

1. Access your Proxmox VE cluster and under **Permissions** / **API Tokens** create a new API token that you will setup the Vault secrets engine with. This token needs the privileges to create and delete API tokens for the relevant users and, if you intend to use [privilege separation](#privilege-separation), to modify ACLs. Rather than handing it full administrator rights, scope it to what it needs (for example `User.Modify` to manage the users' tokens and `Permissions.Modify` to grant ACLs, on the relevant `/access/...` paths). Privilege separation on this configuring token itself is recommended.
2. If you want the role(s), and subsequent minted API tokens, to impersonate a user other than you (probably a good idea) create those accounts under **Permissions** / **Users** (eg `ci` or `packer` or I don't know..). Without privilege separation the minted tokens inherit **all** permissions of the user their role is created for; with privilege separation they only receive the roles granted via the role's ACL settings.

## Setup (Vault)

1. With the plugin installed from the steps above, mount it at some endpoint of your choosing
```sh
vault secrets enable -path=proxmox vault-plugin-secrets-proxmox
```

2. Configure the plugin
```sh
vault write proxmox/config user=<User that configured API token is for, e.g. root> realm=<Realm of the user that configured API token is for, e.g. pam> token_id=<API Token ID e.g. mytesttoken (excluding '<user>@<realm>!' which are set separately)> token_secret=<The secret uuid corresponding to a TokenID> proxmox_url=<API Endpoint URL, e.g. https://host.fqdn:8006/api2/json>
```
Optional config fields include `insecure_skip_tls_verify`, `http_headers`, `proxy_server` and `timeout`.

3. Create a role for the Proxmox user you are going to create tokens for
```sh
vault write proxmox/role/alice user="alice" realm="pve"
```

4. To test that it works, retrieve a new Proxmox API token from Vault
```sh
vault read proxmox/creds/alice
```

5. You should now have gotten an API token for Proxmox, now lets revoke it (using the output `lease_id`)
```sh
vault lease revoke proxmox/creds/alice/<lease id>
```

## Privilege separation

By default a role creates Proxmox tokens that inherit every privilege of their user. To instead mint tokens that only hold a specific set of roles, enable privilege separation on the Vault role and tell it which Proxmox roles to grant and where:

```sh
vault write proxmox/role/alice \
  user="alice" \
  realm="pve" \
  privilege_separation=true \
  acl_path="/vms/100" \
  acl_roles="PVEVMAdmin" \
  acl_propagate=true
```

When `privilege_separation=true` the plugin creates the token with `privsep=1` and then grants `acl_roles` at `acl_path` (via `PUT /access/acl`). Because a privilege-separated token has no privileges until an ACL is attached, `acl_path` and `acl_roles` are required whenever `privilege_separation` is true. If the ACL grant fails the plugin deletes the half-provisioned token so you never receive a token with no access.

Role ACL fields:

| Field | Required | Default | Description |
|-------|----------|---------|-------------|
| `privilege_separation` | no | `false` | Create tokens with `privsep=1` and ACL-scoped privileges instead of inheriting the user's privileges. |
| `acl_path` | when privsep | - | Proxmox ACL object path the token is granted access to (e.g. `/vms/100`, `/`). |
| `acl_roles` | when privsep | - | Comma-separated Proxmox roles granted at `acl_path` (e.g. `PVEVMAdmin,PVEVMUser`). |
| `acl_propagate` | no | `true` | Whether the granted ACL propagates to child paths. |

When a token is revoked, Proxmox automatically removes the ACL entries that referenced it, so no extra cleanup is needed.

## Lease renewal

Renewing a credential's lease in Vault also pushes the new expiry to the underlying Proxmox token, keeping the two in sync so the token does not expire on the Proxmox side while Vault still considers the lease valid.

## Contribute

Pull requests welcome, and be nice.
