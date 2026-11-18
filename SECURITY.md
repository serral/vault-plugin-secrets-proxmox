# Security policy

This plugin handles Proxmox API credentials, so security reports matter.

## Reporting a vulnerability

Do not open a public issue for a security problem.

Use GitHub private vulnerability reporting for this repository: open the **Security** tab, then **Report a vulnerability**. Include the plugin version or commit, the Vault and Proxmox VE versions, and steps to reproduce.

## Supported versions

Only the latest commit on `main` is supported. This project has no tagged releases yet.

## Scope

In scope: how the plugin stores the configuration token, mints, renews and revokes Proxmox tokens, and applies ACLs for privilege-separated tokens.

Out of scope: vulnerabilities in Vault or Proxmox VE themselves. Report those to their own projects.
