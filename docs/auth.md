# Authentication

GlacierVault supports two authentication methods, selected via the `AUTH_TYPE`
environment variable in docker compose.

## Password (default)

```yaml
environment:
  - AUTH_TYPE=password  # default; omit for password auth
  - INITIAL_PASSWORD=changeme
```

- Single admin account, bcrypt-hashed password stored in the database.
- 8-hour JWT session in an HttpOnly cookie.
- Change the password in Settings after first boot if you used a generated one.
- If `INITIAL_PASSWORD` is omitted, the server generates a random password and
  prints it in the container log on first boot.

## OIDC / SSO

For users with an existing identity provider (Authentik, Keycloak, Authelia,
Dex, etc.).

```yaml
environment:
  - AUTH_TYPE=oidc
  - OIDC_ISSUER=https://auth.example.com/application/o/glaciervault/
  - OIDC_CLIENT_ID=your-client-id
  - OIDC_CLIENT_SECRET=your-client-secret
  - OIDC_REDIRECT_URL=https://vault.example.com/api/auth/oidc/callback
```

### Setup

1. In your identity provider, create a new OIDC application / client:
   - **Redirect URI:** `https://<your-host>/api/auth/oidc/callback`
   - **Scopes:** `openid profile email`
2. Copy the issuer URL, client ID, and client secret into docker compose.
3. Set `AUTH_TYPE=oidc` and restart the container.
4. The login page will show a "Sign in with SSO" button instead of the
   password form.

### How it works

- `GET /api/auth/oidc/login` generates a CSRF state token and redirects to
  your identity provider.
- `GET /api/auth/oidc/callback` verifies the state, exchanges the code for
  tokens, validates the ID token, and creates an 8-hour JWT session.
- The session uses the same HttpOnly cookie as password auth.
- The user's email from the ID token becomes the session subject.

### Notes

- When `AUTH_TYPE=oidc`, password login is disabled. The password hash in the
  database is ignored.
- To switch back to password auth, set `AUTH_TYPE=password` and restart.
  Your original password (or `INITIAL_PASSWORD`) still works.
- `OIDC_REDIRECT_URL` must exactly match the redirect URI registered with
  your provider, including scheme and port.

## Planned improvements

- TOTP two-factor authentication
- Login rate limiting and lockout
- Session invalidation on password change ("log out all devices")
- Scoped API tokens for automation
- `Secure` cookie flag enforcement over HTTPS

## Two-Factor Authentication (TOTP)

Available in Settings → Security when using password auth.

1. Click "Enable 2FA" — a QR code appears. Scan it with your authenticator
   app (Google Authenticator, Authy, 1Password, etc.).
2. Enter the 6-digit code to confirm. You'll receive 10 single-use backup
   codes — save them somewhere safe.
3. On next login, after your password, you'll be prompted for the current
   TOTP code (or a backup code).

To disable, enter your password in Settings → Security. Disabling clears
the secret and all backup codes.

## API Tokens

For scripts and automation. Manage in Settings → Security → API Tokens.
Tokens are long-lived, revocable, and shown only once at creation.

- Click "Create token", give it a name and optional expiry (e.g. `720h`).
- Copy the `gv_...` token immediately — it won't be shown again.
- Use as `Authorization: Bearer gv_...` header.
- Tokens bypass TOTP (they're for automation, not browsers).
- Revoke anytime; revoked tokens stop working immediately.

## Session Management

Found in Settings → Security → Sessions.

- Changing your password (Settings → General → Account) automatically logs
  out all other devices.
- "Log out all devices" revokes all sessions immediately without changing
  the password.
- Failed logins are rate-limited: 5 failures from an IP triggers a
  15-minute lockout.
