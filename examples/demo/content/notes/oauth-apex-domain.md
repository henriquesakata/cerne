The Google OAuth callback must be served from the apex domain (`taskflow.example`), never from `www`.

The `state` cookie is issued for the apex host. When the callback lands on `www`, the browser does not send it, validation fails, and the user sees a 400 after having approved the consent screen. The failure looks like an attack in the logs (state mismatch) and is not one.

Why it stays this way: the redirect URI is registered with Google as the apex URL, and widening the cookie to `.taskflow.example` would expose it to every subdomain, including user-content hosts.

If a load balancer or CDN rule ever redirects apex to `www`, sign-in with Google breaks for everyone.
