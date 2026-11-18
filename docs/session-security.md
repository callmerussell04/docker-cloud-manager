# DCM session security

DCM access tokens are JWTs backed by durable server-side SSO sessions; refresh tokens are opaque single-use secrets.

Removing a cookie removes the browser's credential. Revocation changes what SSO accepts, including previously copied credentials. Neither operation alone proves the other happened.

## Session and token state

SSO owns durable PostgreSQL sessions. Gateway remains stateless and forwards through its existing service/client boundary. One successful local login or OIDC callback creates one session; separate browsers/devices create independent sessions. Tabs sharing the refresh cookie share a session. Registration alone does not create one.

The session ID is a random UUID bound to the existing DCM user ID. Access JWTs include that session ID (`sid`), user ID, access type and required expiry. Validation checks the exact signing algorithm and mandatory claims. Gateway asks SSO to verify tokens; SSO checks signature, expiry, session ownership/liveness and current active user on each verification/permission request. Missing or revoked sessions fail closed, without a positive session cache. Session ID is never an alternative owner ID or caller-supplied authorization scope.

Refresh tokens are opaque 32-byte cryptographically random secrets encoded with base64url, so successive refreshes issue distinct tokens even within the same second. SSO stores only SHA-256 digests, never raw refresh secrets. A unique digest index identifies the token and its session without trusting an unsigned session selector. High-entropy random secrets permit fast digest lookup; human passwords still use bcrypt. Refresh secrets remain in HttpOnly cookies and internal authenticated RPC bodies, never URLs, browser storage, logs or audit details.

Stored data:

| Record | Required state |
| --- | --- |
| Session | ID, user ID FK, created time, absolute expiry, idle expiry, revoked time and safe reason enum (`logout`, `replay`, `user_security_change`, `provider_logout`); for a session created by an OIDC callback also the provider, the provider session ID (the ID token's `sid`, indexed with the provider) and the ID token |
| Refresh token | Unique digest, session FK, issued time, consumed time; at most one unconsumed token per session |

The stored ID token is Keycloak's, not a DCM credential. It is kept only as the `id_token_hint` of the provider sign-out URL, cleared in the same statement that revokes the session (a constraint forbids a revoked session with an ID token), never logged or audited, and leaves SSO only inside that URL.

Consumed digests are retained until the session's absolute expiry so any issued predecessor can be recognized. Revocation and consumption are durable and survive SSO restart. Expired sessions and token rows are cleaned up in bounded batches; expiry enforcement does not depend on cleanup. Refresh is rate-limited and token input is size-bound. Raw replacement tokens are never stored for retry. Session creation, rotation and revocation use consumer-owned service interfaces and explicit composition-root dependencies; transaction mechanics stay in repository adapters.

## Rotation, replay and concurrency

For refresh, SSO identifies the digest, then locks the user and session in a consistent order shared by all session mutations. It rechecks token state, ownership, active user, revocation and both expiries under the lock. It generates and signs the replacement pair before commit, then atomically marks the old digest consumed, inserts the new digest and advances idle expiry. Credentials are returned only after confirmed commit. A generation/signing/write failure rolls back and leaves the old token usable if commit has not occurred. No provider network calls run under the transaction.

Presenting a known consumed token revokes that session, including its current refresh token and all its access JWTs. Revocation commits before a generic `401` is returned. Unknown/malformed secrets return the same safe error but cannot revoke a guessed session. A revoked or expired token returns `401` without creating state. Replay affects only that session, not other devices.

There is no grace period and no idempotent replay of a successful refresh response. For two requests with the same token, one rotation can commit; the loser detects consumption, commits session revocation and returns `401`. The winner can receive `200`, but its new tokens then fail after revocation. The server cannot distinguish theft from a legitimate duplicate. This deliberately favors stopping replay over preserving a raced session.

The browser serializes all refresh entry points (startup, OIDC callback and interceptor), including effect re-entry and same-origin tabs. It uses a shared in-tab promise and an origin-wide Web Lock around the HTTP request so the cookie is read when the lock holder sends it. Logout takes the same lock, waits for pending refresh and suppresses late access-token updates. Successful logout is broadcast to other tabs, which clear their auth/query state. On browsers without cross-tab locking, refresh proceeds with in-tab serialization and simultaneous tabs may force sign-in; a racy localStorage lock is never used as a security guarantee. Separate origins sharing cookies have the same duplicate risk.

If commit succeeds but the response or Set-Cookie is lost, the old token is consumed; retry triggers revocation and requires sign-in. Clients/proxies must not automatically retry refresh. A confirmed rollback permits a later user-initiated retry; an ambiguous transport/commit result is treated as uncertain. On `503`/`504` or network failure, the browser suspends authenticated actions and shows a retry/sign-in choice rather than claiming revocation or deleting the cookie, warning that retry after an uncertain result may require sign-in. No token is returned for an uncertain commit.

## Logout and lifetime

| Event | Required outcome |
| --- | --- |
| `POST /api/v1/auth/logout` with current or consumed refresh secret | Revoke that secret's session, then expire the matching cookie and return `200`; repeat calls succeed. No valid access JWT is required, so logout works after access expiry. |
| Logout with absent, unknown, expired or already revoked secret | Idempotent `200` and cookie removal; never revoke an unrelated session. A retained known expired session may be marked revoked. |
| Logout storage/SSO failure or uncertain commit | Safe `503`/`504`, no successful-logout claim, preserve cookie for retry. Browser suspends actions and offers retry; confirmed success clears memory, queries and cookie. |
| Logout racing refresh | Same user/session locks serialize them. Logout can resolve a consumed predecessor. Either logout prevents refresh, or refresh commits first and logout revokes the successor too. A late Set-Cookie may restore an unusable cookie but cannot restore authorization. |
| Back-channel logout from the provider | `POST /api/v1/auth/oidc/keycloak/backchannel-logout` with a verified logout token revokes, under each user's lock and with reason `provider_logout`, the sessions bound to its `sid`, or without one every session of the user bound to that provider subject that was signed in through the provider. A valid token that matches nothing answers `200`; revocation is idempotent and logout-token IDs are not tracked. |
| Password change/reset or deactivation | Revoke all of that user's sessions atomically with the user mutation. Serialize with login/refresh using the user lock. Reactivation never revives them. Failure rolls back both changes. |
| Role/profile update, or a quota change in Core | Keep sessions; use current user data for authorization. Deactivation through any update path still revokes all sessions. |
| Another device logs in | Create a separate session; existing sessions remain active. |

Ordinary logout is single-session DCM logout. There is no logout-all endpoint or session-management UI. All-session revocation is an internal security operation used by password/status changes.

Provider sign-out: signing out of a live session created through OIDC also ends the Keycloak session. Gateway passes SSO the validated request origin plus `/login` as the post-logout redirect URI; SSO revokes the session and, when the provider is available and advertises an end-session endpoint, answers that endpoint with `id_token_hint`, `client_id` and `post_logout_redirect_uri`. `POST /api/v1/auth/logout` returns it as JSON `provider_logout_url` (an empty object otherwise), and the SPA navigates to it after the DCM sign-out succeeded. Because the revocation clears the ID token, the URL is answered once; a local session, an already revoked one, or a sign-out while the provider is unreachable gets none. Keycloak then calls the back-channel route for the session DCM already revoked, which succeeds without effect. Ending the Keycloak session also signs the user out of other applications on that realm.

Back-channel logout: SSO verifies the logout token's signature, issuer, audience and expiry as for an ID token, and also requires the back-channel logout event, a `sub` or `sid`, and no nonce. The route needs no cookie or browser origin, bounds its body, has its own rate-limit bucket, and answers `400` for an invalid token, `503` while the provider's keys are unavailable and `404` with OIDC off, always with `Cache-Control: no-store`. Keycloak sends it on sign-out and session termination there, but not reliably when a user is disabled or loses a group; the 12-hour OIDC lifetime bounds those cases.

Access JWT expiry is 15 minutes, capped by session absolute expiry. Local sessions have a 30-day absolute limit from initial authentication; sessions created by an OIDC callback have a 12-hour one (`service.OIDCSessionTTL`), so a change made at the provider reaches DCM within it. Both have a 7-day refresh idle limit. Successful refresh sets idle expiry to `min(now + 7 days, absolute expiry)`; API activity alone does not extend it. At `now >= expiry`, SSO rejects. Revoked or idle-expired sessions reject access verification even if JWT expiry is later. Cookie lifetime comes from SSO's remaining refresh validity, rounded down and capped by both limits, rather than an independent Gateway sliding setting. These durations are product choices defined by SSO bootstrap configuration in code, not values mandated by a standard. They are not runtime settings.

Revocation applies to authorization checks starting after the revocation commit. An already authorized request may finish. Established terminal/log streams and already issued one-use stream tickets retain their existing lifetimes; revocation does not actively terminate streams. SSO/database unavailability denies new authorization with safe availability errors rather than accepting cached identity or misreporting invalid credentials.

## API and browser contract

- SSO `Auth.Logout` takes refresh-secret input and the post-logout redirect URI, and returns the provider sign-out URL, empty except for a live session created through OIDC. SSO `Auth.BackchannelLogout` takes the provider and the logout token under system scope and returns the number of revoked sessions. Gateway carries it through its service/client interfaces with existing internal authentication, bounded RPC contexts and safe mappings. A request owner ID is never accepted as logout authority.
- Login, refresh and OIDC callback RPC results include an absolute refresh-expiry timestamp in Unix seconds so Gateway can round down remaining validity and set cookie Max-Age consistently. HTTP login/refresh JSON is limited to `access_token`; credential responses carry `Cache-Control: no-store`.
- On refresh `401`, Gateway expires the cookie and the browser clears local auth. On refresh `503`/`504`, the cookie is preserved and the browser shows temporary failure. Login/callback persistence failures never set a refresh cookie or return usable credentials; OIDC codes/states may already be consumed, requiring a fresh OIDC start. A failed OIDC callback redirects to the SPA callback page with `login=error&reason=<code>` and sets no refresh cookie; the SPA calls refresh only after `login=success`.
- Cookie deletion preserves configured Path/Domain, HttpOnly, SameSite and HTTPS Secure deployment policy. Cookie-authenticated refresh/logout validate Origin against configured browser origins, falling back to an exact allowed Referer origin when Origin is absent; missing/untrusted browser origin evidence yields safe `403`, without mutation or cookie deletion. OIDC callback relies on its state/nonce/browser binding instead of this POST-origin rule: a durable, single-use login attempt (30 minutes) bound to a per-attempt HttpOnly cookie.
- Legacy refresh JWTs and access JWTs lacking session claims are rejected; there are no compatibility shims, and sign-in is required after rollout.
- Credentials/digests stay out of DTOs, logs and audits. Best-effort safe event reasons may describe login, rotation, replay revocation and logout; audit failure cannot undo a committed security change.

Rotation/replay and inactivity expiry are informed by [RFC 9700 §4.14.2](https://www.rfc-editor.org/rfc/rfc9700.html#section-4.14.2). DCM's opaque format, strict duplicate handling, durations and logout scope are local design choices; this document does not claim DCM is a general OAuth authorization server.
